//go:build windows

package main

// Independent UI windows only. All window/WebView work runs on the tray STA;
// COM reference counting can run on other threads. These handlers intentionally
// do not use comObj's process-lifetime, trivial AddRef/Release implementation.
import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Slots and IIDs checked against Microsoft's Microsoft.Web.WebView2
// 1.0.2903.40 build/native/include/WebView2.h (base interfaces).
const (
	progressAddTitle     = 46
	progressGetTitle     = 48
	progressAddClose     = 59
	progressPutNewWindow = 4
	progressUserGesture  = 8
	progressGetDeferral  = 9
	progressComplete     = 3
	wmNCDestroy          = 0x0082
	wmProgressDispose    = 0x8000 + 31
)

const (
	progressCreated = iota + 100
	progressTitleChanged
	progressCloseRequested
	progressProcessFailed
	progressNewWindow
)

var (
	progressWindows     = make(map[string]*progressWindow)
	progressByHWND      = make(map[uintptr]*progressWindow)
	progressUIStopping  bool
	progressNextSerial  uintptr
	progressClass       *uint16
	progressClassIcons  [2]uintptr
	progressProcCB      = syscall.NewCallback(progressProc)
	progressHandlerVT   [4]uintptr
	progressHandlers    sync.Map // COM address -> *progressHandler: strong GC roots until Release == 0
	progressTitleIID    = mustGUID("f5f2b923-953e-4042-9f95-f3a118e1afd4")
	progressCloseIID    = mustGUID("5c19e9e0-092f-486b-affa-ca8231913039")
	pProgressSetTitle   = user32.NewProc("SetWindowTextW")
	pProgressDestroyIco = user32.NewProc("DestroyIcon")
	pProgressUnregister = user32.NewProc("UnregisterClassW")
)

// Win32 RECT uses LONG (32 bits), including in a 64-bit process.
type progressRect struct{ left, top, right, bottom int32 }

type progressHandler struct {
	vtbl   uintptr // COM ABI: first word is the vtable address
	refs   atomic.Int32
	iid    guid
	kind   int
	window *progressWindow
}

type progressEvent struct {
	remove int
	token  int64 // EventRegistrationToken is passed by value to remove_*
}

type progressWindow struct {
	id                 string
	serial             uintptr // guards queued messages when Windows reuses an HWND
	hwnd, env, ctl, wv uintptr
	request, deferral  uintptr
	creating, closed   bool
	closing            bool
	events             []progressEvent
}

// Allocate the four callback trampolines once, never per popup/registration.
func progressCOMInit() {
	progressHandlerVT[0] = syscall.NewCallback(func(this, riid, out uintptr) uintptr {
		if out == 0 || riid == 0 {
			return 0x80004003 // E_POINTER
		}
		h := (*progressHandler)(ptrFrom(this))
		*(*uintptr)(ptrFrom(out)) = 0
		want := *(*guid)(ptrFrom(riid))
		if want != iidUnknown && want != h.iid {
			return hrENoInterface
		}
		h.refs.Add(1)
		*(*uintptr)(ptrFrom(out)) = this
		return 0
	})
	progressHandlerVT[1] = syscall.NewCallback(func(this uintptr) uintptr {
		return uintptr((*progressHandler)(ptrFrom(this)).refs.Add(1))
	})
	progressHandlerVT[2] = syscall.NewCallback(func(this uintptr) uintptr {
		h := (*progressHandler)(ptrFrom(this))
		n := h.refs.Add(-1)
		if n == 0 {
			progressHandlers.Delete(this)
		}
		return uintptr(n)
	})
	progressHandlerVT[3] = syscall.NewCallback(func(this, sender, args uintptr) uintptr {
		h := (*progressHandler)(ptrFrom(this))
		w := h.window
		// The local typed pointer keeps the handler alive even if Close removes
		// the registration and releases its last native reference during Invoke.
		defer runtime.KeepAlive(h)
		if h.kind == progressCreated {
			w.controllerCreated(sender, args) // sender is HRESULT; args is controller
			return 0
		}
		if w.closed || w.closing || progressUIStopping {
			if h.kind == progressNewWindow {
				comCall(args, nwPutHandled, 1)
			}
			return 0
		}
		switch h.kind {
		case progressTitleChanged:
			w.updateTitle()
		case progressCloseRequested, progressProcessFailed:
			// Release controllers outside WebView's callback stack.
			w.requestClose()
		case progressNewWindow:
			onNewWindow(args)
		}
		return 0
	})
}

func newProgressHandler(w *progressWindow, iid guid, kind int) uintptr {
	h := &progressHandler{vtbl: uintptr(unsafe.Pointer(&progressHandlerVT[0])), iid: iid, kind: kind, window: w}
	h.refs.Store(1) // caller's reference; native registration takes its own
	p := uintptr(unsafe.Pointer(h))
	progressHandlers.Store(p, h)
	return p
}

func registerProgressClass() bool {
	if progressClass != nil {
		return true
	}
	inst, _, _ := pGetModuleHandle.Call(0)
	cls, _ := syscall.UTF16PtrFromString(AppName + "Progress-" + appRootHash)
	cur, _, _ := pLoadCursor.Call(0, 32512)
	icons := [2]uintptr{programIcon(inst, 32, 13, 141, 128), programIcon(inst, 16, 13, 141, 128)}
	wc := wndClassEx{Instance: inst, WndProc: progressProcCB, ClassName: cls, Cursor: cur, Background: 6, Icon: icons[0], IconSm: icons[1]}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		logf("progress window: could not register the window class")
		for _, icon := range icons {
			if icon != 0 {
				pProgressDestroyIco.Call(icon)
			}
		}
		return false
	}
	progressClass, progressClassIcons = cls, icons
	return true
}

func openProgressWindow(id string, args uintptr) {
	if progressUIStopping {
		return
	}
	var userInitiated int32
	if failed(comCall(args, progressUserGesture, uintptr(unsafe.Pointer(&userInitiated)))) || userInitiated == 0 {
		return
	}
	if w := progressWindows[id]; w != nil && !w.closed && !w.closing {
		w.focus()
		return // a previously navigated WebView cannot be assigned as NewWindow
	}
	if mw.env == 0 || !registerProgressClass() {
		return
	}
	progressNextSerial++
	w := &progressWindow{id: id, serial: progressNextSerial, env: mw.env}
	comCall(w.env, 1) // independently retained across main-window/browser teardown
	if hr := comCall(args, progressGetDeferral, uintptr(unsafe.Pointer(&w.deferral))); failed(hr) || w.deferral == 0 {
		logf("progress window: could not defer the request (0x%08x)", uint32(hr))
		w.close()
		return
	}
	w.request = args
	comCall(args, 1)
	inst, _, _ := pGetModuleHandle.Call(0)
	title, _ := syscall.UTF16PtrFromString(shownName())
	s := dpiScale(mw.hwnd)
	hwnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(progressClass)), uintptr(unsafe.Pointer(title)), wsOverlappedWin,
		cwUseDefault, cwUseDefault, uintptr(660*s), uintptr(650*s), 0, 0, inst, 0)
	if hwnd == 0 {
		logf("progress window: could not create the window")
		w.close()
		return
	}
	w.hwnd = hwnd
	progressWindows[id], progressByHWND[hwnd] = w, w
	if systemUsesDarkMode() {
		on := int32(1)
		pDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&on)), 4)
	}
	w.creating = true
	handler := newProgressHandler(w, iidCtlCompleted, progressCreated)
	hr := comCall(w.env, envCreateController, hwnd, handler)
	comCall(handler, 2) // runtime owns the asynchronous completion reference
	if failed(hr) {
		logf("progress window: controller creation failed (0x%08x)", uint32(hr))
		w.creating = false
		w.close()
		return
	}
	w.focus()
}

func (w *progressWindow) finishRequest() {
	args, deferral := w.request, w.deferral
	w.request, w.deferral = 0, 0
	if deferral != 0 {
		comCall(deferral, progressComplete)
		comCall(deferral, 2)
	}
	if args != 0 {
		comCall(args, 2)
	}
}

func (w *progressWindow) controllerCreated(hr, ctl uintptr) {
	w.creating = false
	if !w.closed && !w.closing && !progressUIStopping && (failed(hr) || ctl == 0) {
		logf("progress window: no controller (0x%08x)", uint32(hr))
	}
	if w.closed || w.closing || progressUIStopping || failed(hr) || ctl == 0 {
		if ctl != 0 {
			comCall(ctl, ctlClose) // callback argument is borrowed, not ours to Release
		}
		w.close()
		w.releaseEnvironment()
		return
	}
	w.ctl = ctl
	comCall(ctl, 1)
	if failed(comCall(ctl, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&w.wv)))) || w.wv == 0 {
		w.close()
		return
	}
	var settings uintptr
	if !failed(comCall(w.wv, wvGetSettings, uintptr(unsafe.Pointer(&settings)))) && settings != 0 {
		comCall(settings, setPutStatusBar, 0)
		comCall(settings, setPutHostObjects, 0)
		if !debugMode {
			comCall(settings, setPutDevTools, 0)
		}
		comCall(settings, 2)
	}
	for _, event := range []struct {
		add, kind int
		iid       guid
	}{
		{progressAddTitle, progressTitleChanged, progressTitleIID},
		{progressAddClose, progressCloseRequested, progressCloseIID},
		{wvAddProcessFailed, progressProcessFailed, iidProcessFailed},
		{wvAddNewWindowRequestd, progressNewWindow, iidNewWindowReq},
	} {
		h := newProgressHandler(w, event.iid, event.kind)
		var token int64
		r := comCall(w.wv, event.add, h, uintptr(unsafe.Pointer(&token)))
		comCall(h, 2)
		if failed(r) {
			logf("progress window: event registration failed (0x%08x)", uint32(r))
			w.close()
			return
		}
		w.events = append(w.events, progressEvent{event.add + 1, token})
	}
	w.resize()
	// Let WebView2 navigate the requested popup itself. This preserves its named
	// WindowProxy, script-close semantics, and the existing environment/profile.
	if hr := comCall(w.request, progressPutNewWindow, w.wv); failed(hr) {
		logf("progress window: could not attach the popup (0x%08x)", uint32(hr))
		w.close()
		return
	}
	w.finishRequest()
	if !w.closed && !w.closing {
		visible, _, _ := pIsWindowVis.Call(w.hwnd)
		if minimized, _, _ := pIsIconic.Call(w.hwnd); minimized != 0 {
			visible = 0
		}
		comCall(w.ctl, ctlPutIsVisible, visible)
		// Respect minimization or a focus change while creation was pending.
		if foreground, _, _ := pGetForeground.Call(); foreground == w.hwnd && visible != 0 {
			comCall(w.ctl, ctlMoveFocus, focusProgrammatic)
		}
	}
}

func (w *progressWindow) releaseEnvironment() {
	if w.env != 0 && !w.creating {
		env := w.env
		w.env = 0
		comCall(env, 2)
	}
}

func (w *progressWindow) requestClose() {
	if !w.closed && !w.closing {
		w.closing = true
		pPostMessage.Call(w.hwnd, wmProgressDispose, w.serial, 0)
	}
}

func (w *progressWindow) close() {
	if w.closed {
		return
	}
	w.closed = true
	if progressWindows[w.id] == w {
		delete(progressWindows, w.id)
	}
	// Zero fields before COM calls: native operations can dispatch messages.
	hwnd, ctl, wv, events := w.hwnd, w.ctl, w.wv, w.events
	w.hwnd, w.ctl, w.wv, w.events = 0, 0, 0, nil
	delete(progressByHWND, hwnd)
	w.finishRequest() // unblocks window.open even when closed before creation
	if wv != 0 {
		for _, event := range events {
			comCall(wv, event.remove, uintptr(event.token)) // x64 token by value
		}
	}
	if ctl != 0 {
		comCall(ctl, ctlClose)
		comCall(ctl, 2)
	}
	if wv != 0 {
		comCall(wv, 2)
	}
	w.releaseEnvironment() // pending completion retains its env until it arrives
	if hwnd != 0 {
		pDestroyWindow.Call(hwnd)
	}
}

func (w *progressWindow) updateTitle() {
	var p uintptr
	if failed(comCall(w.wv, progressGetTitle, uintptr(unsafe.Pointer(&p)))) || p == 0 {
		return
	}
	defer pCoFree.Call(p)
	text := utf16PtrToString(p)
	if text == "" {
		text = shownName()
	}
	title, err := syscall.UTF16PtrFromString(text)
	if err == nil {
		pProgressSetTitle.Call(w.hwnd, uintptr(unsafe.Pointer(title)))
	}
}

func (w *progressWindow) focus() {
	if w.closed || w.closing || w.hwnd == 0 {
		return
	}
	cmd := uintptr(swShow)
	if m, _, _ := pIsIconic.Call(w.hwnd); m != 0 {
		cmd = swRestore
	}
	pShowWindow.Call(w.hwnd, cmd)
	pBringToTop.Call(w.hwnd)
	pSetForeground.Call(w.hwnd)
	if w.ctl != 0 {
		comCall(w.ctl, ctlPutIsVisible, 1)
		comCall(w.ctl, ctlMoveFocus, focusProgrammatic)
	}
}

func (w *progressWindow) resize() {
	if w.ctl != 0 && w.hwnd != 0 {
		var r progressRect
		pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
		comCall(w.ctl, ctlPutBounds, uintptr(unsafe.Pointer(&r)))
	}
}

func progressProc(hwnd, msg, wp, lp uintptr) uintptr {
	w := progressByHWND[hwnd]
	if w != nil {
		switch uint32(msg) {
		case wmProgressDispose:
			if wp == w.serial {
				w.close()
			}
			return 0
		case wmClose:
			w.close()
			return 0
		case wmNCDestroy:
			delete(progressByHWND, hwnd)
			w.hwnd = 0 // already being destroyed: do not call DestroyWindow again
			w.close()
		case wmSize:
			if w.ctl != 0 {
				visible := uintptr(1)
				if wp == 1 { // SIZE_MINIMIZED
					visible = 0
				}
				comCall(w.ctl, ctlPutIsVisible, visible)
				if visible != 0 {
					w.resize()
				}
			}
			return 0
		case wmMove:
			if w.ctl != 0 {
				comCall(w.ctl, ctlNotifyParentMove)
			}
		case wmActivate, wmSetFocus:
			if w.ctl != 0 && (uint32(msg) == wmSetFocus || wp&0xffff != 0) {
				comCall(w.ctl, ctlMoveFocus, focusProgrammatic)
			}
		case wmGetMinMaxInfo:
			s := dpiScale(hwnd)
			mmi := (*[10]int32)(ptrFrom(lp))
			mmi[6], mmi[7] = int32(360*s), int32(300*s)
			return 0
		case wmDpiChanged:
			r := (*progressRect)(ptrFrom(lp))
			pSetWindowPos.Call(hwnd, 0, uintptr(r.left), uintptr(r.top), uintptr(r.right-r.left), uintptr(r.bottom-r.top), 0x0014)
			w.resize()
			if w.ctl != 0 {
				comCall(w.ctl, ctlNotifyParentMove)
			}
			return 0
		}
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}

func shutdownProgressUI() {
	progressUIStopping = true
	for _, w := range progressWindows {
		w.close()
	}
	releaseWebView()
	if mw.hwnd != 0 {
		hwnd := mw.hwnd
		mw.hwnd = 0
		pDestroyWindow.Call(hwnd)
	}
	if progressClass != nil {
		inst, _, _ := pGetModuleHandle.Call(0)
		if ok, _, _ := pProgressUnregister.Call(uintptr(unsafe.Pointer(progressClass)), inst); ok != 0 {
			for _, icon := range progressClassIcons {
				if icon != 0 {
					pProgressDestroyIco.Call(icon)
				}
			}
			progressClass, progressClassIcons = nil, [2]uintptr{}
		}
	}
}
