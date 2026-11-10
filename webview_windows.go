//go:build windows

package main

// The program's own window: a normal Windows window with Microsoft's WebView2 inside, showing the local page.
// No WebView2Loader.dll and no third-party code: the installed WebView2 runtime is found in the registry and its
// EmbeddedBrowserWebView.dll is called directly, the way the open-source loaders do (OpenWebView2Loader; the Go
// port in github.com/wailsapp/go-webview2 was read to confirm the steps, design 4.1). If anything fails, the
// program falls back to an Edge or Chrome "app" window (sys_windows.go), so it always opens.
//
// Everything here runs on the UI thread (the tray's message loop, tray_windows.go): WebView2 must be used from
// the thread that created it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	pCoTaskMemAlloc   = ole32.NewProc("CoTaskMemAlloc")
	pSetDpiCtx        = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetDPIAware      = user32.NewProc("SetProcessDPIAware")
	pGetDpiForSystem  = user32.NewProc("GetDpiForSystem")
	pGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	pSetAppID         = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	pShellExecute     = shell32.NewProc("ShellExecuteW")
	pGetClientRect    = user32.NewProc("GetClientRect")
	pGetWindowRect    = user32.NewProc("GetWindowRect")
	pIsZoomed         = user32.NewProc("IsZoomed")
	pLoadCursor       = user32.NewProc("LoadCursorW")
	pSendMessage      = user32.NewProc("SendMessageW")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pEnumMonitors     = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfo   = user32.NewProc("GetMonitorInfoW")
	pDwmSetWindowAttr = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
)

const (
	wmShowMain        = 0x0400 + 10 // posted to the tray window: show the main window
	wmSize            = 0x0005
	wmMove            = 0x0003
	wmActivate        = 0x0006
	wmSetFocus        = 0x0007
	wmGetMinMaxInfo   = 0x0024
	wmDpiChanged      = 0x02E0
	wmSetIcon         = 0x0080
	wsOverlappedWin   = 0x00CF0000
	cwUseDefault      = 0x80000000
	swHide            = 0
	swShowNormal      = 1
	swShowMaximized   = 3
	swShow            = 5
	swRestore         = 9
	hrENoInterface    = 0x80004002
	focusProgrammatic = 0
)

// ---- COM plumbing --------------------------------------------------------------------------------------------------

type guid [16]byte

// mustGUID parses "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" into the byte order Windows uses.
func mustGUID(s string) guid {
	var g guid
	h := strings.ReplaceAll(strings.Trim(s, "{}"), "-", "")
	if len(h) != 32 {
		panic("bad GUID " + s)
	}
	b := make([]byte, 16)
	for i := range b {
		v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			panic("bad GUID " + s)
		}
		b[i] = byte(v)
	}
	// Data1 (4 bytes), Data2 and Data3 (2 bytes each) are little-endian; Data4 stays as written.
	g[0], g[1], g[2], g[3] = b[3], b[2], b[1], b[0]
	g[4], g[5] = b[5], b[4]
	g[6], g[7] = b[7], b[6]
	copy(g[8:], b[8:])
	return g
}

var (
	iidUnknown       = mustGUID("00000000-0000-0000-C000-000000000046")
	iidEnvOptions    = mustGUID("2fde08a8-1e9a-4766-8c05-95a9ceb9d1c5")
	iidEnvCompleted  = mustGUID("4e8a3389-c9d8-4bd2-b6b5-124fee6cc14d")
	iidCtlCompleted  = mustGUID("6c4819f3-c9b7-4260-8127-c9f5bde7f68c")
	iidNewWindowReq  = mustGUID("d4c185fe-c81c-4989-97af-2d3fa7ab5651")
	iidProcessFailed = mustGUID("79e0aea4-990b-42d9-aa1d-0fcc2e5bc7f1")
)

// comObj is a COM object made in Go: its first word points at the table of functions. The objects live in package
// variables for the whole run, so the garbage collector never frees them while WebView2 holds them.
type comObj struct {
	vtbl uintptr
	iid  guid
}

func newComObj(iid guid, fns ...uintptr) *comObj {
	tbl := append([]uintptr{cbQueryInterface, cbAddRef, cbRelease}, fns...)
	vtables = append(vtables, tbl) // kept alive
	return &comObj{vtbl: uintptr(unsafe.Pointer(&tbl[0])), iid: iid}
}

var vtables [][]uintptr

func (o *comObj) ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Callbacks are made once (Windows allows only about 2000 for a whole process), in comInit, which uiInit calls
// before any window exists.
var (
	cbQueryInterface, cbAddRef, cbRelease                           uintptr
	optionsObj, envDoneObj, ctlDoneObj, newWindowObj, procFailedObj *comObj
)

func comInit() {
	cbQueryInterface = syscall.NewCallback(func(this, riid, ppv uintptr) uintptr {
		o := (*comObj)(ptrFrom(this))
		want := *(*guid)(ptrFrom(riid))
		if want == iidUnknown || want == o.iid {
			*(*uintptr)(ptrFrom(ppv)) = this
			return 0
		}
		*(*uintptr)(ptrFrom(ppv)) = 0
		return hrENoInterface
	})
	cbAddRef = syscall.NewCallback(func(this uintptr) uintptr { return 1 })
	cbRelease = syscall.NewCallback(func(this uintptr) uintptr { return 1 })

	// ICoreWebView2EnvironmentOptions: no extra browser switches, the default language, the oldest runtime we accept.
	optStr := func(val string) uintptr {
		return syscall.NewCallback(func(this, out uintptr) uintptr {
			*(*uintptr)(ptrFrom(out)) = coTaskString(val)
			return 0
		})
	}
	optPut := syscall.NewCallback(func(this, v uintptr) uintptr { return 0 })
	optOff := syscall.NewCallback(func(this, out uintptr) uintptr { *(*int32)(ptrFrom(out)) = 0; return 0 })
	optionsObj = newComObj(iidEnvOptions, optStr(""), optPut, optStr(""), optPut, optStr(webview2MinVersion), optPut, optOff, optPut)

	envDoneObj = newComObj(iidEnvCompleted, syscall.NewCallback(func(this, hr, env uintptr) uintptr { onEnvDone(hr, env); return 0 }))
	ctlDoneObj = newComObj(iidCtlCompleted, syscall.NewCallback(func(this, hr, ctl uintptr) uintptr { onCtlDone(hr, ctl); return 0 }))
	newWindowObj = newComObj(iidNewWindowReq, syscall.NewCallback(func(this, sender, args uintptr) uintptr { onNewWindow(args); return 0 }))
	procFailedObj = newComObj(iidProcessFailed, syscall.NewCallback(func(this, sender, args uintptr) uintptr { onProcessFailed(args); return 0 }))
}

// coTaskString copies s into memory from CoTaskMemAlloc: WebView2 frees strings it receives with CoTaskMemFree.
func coTaskString(s string) uintptr {
	u, _ := syscall.UTF16FromString(s)
	p, _, _ := pCoTaskMemAlloc.Call(uintptr(len(u) * 2))
	if p == 0 {
		return 0
	}
	copy(unsafe.Slice((*uint16)(ptrFrom(p)), len(u)), u)
	return p
}

// comCall calls method number idx of the COM object at obj (0-2 are QueryInterface, AddRef, Release).
func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vt := *(*uintptr)(ptrFrom(obj))
	fn := *(*uintptr)(ptrFrom(vt + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

// Method numbers in the WebView2 interfaces (WebView2.h, in the order the header declares them).
const (
	envCreateController = 3

	ctlPutIsVisible     = 4
	ctlPutBounds        = 6
	ctlMoveFocus        = 12
	ctlNotifyParentMove = 23
	ctlClose            = 24
	ctlGetCoreWebView2  = 25

	wvGetSettings          = 3
	wvNavigate             = 5
	wvAddProcessFailed     = 25
	wvReload               = 31
	wvAddNewWindowRequestd = 44

	setPutStatusBar   = 10
	setPutDevTools    = 12
	setPutHostObjects = 16

	nwGetURI     = 3
	nwPutHandled = 6

	pfGetKind = 3
)

// ---- finding the runtime -------------------------------------------------------------------------------------------

// findWebView2 returns EmbeddedBrowserWebView.dll of the installed WebView2 runtime (stable channel), machine-wide
// first, then for this user.
func findWebView2() (dll, version string, err error) {
	const key = `Software\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	for _, root := range []syscall.Handle{syscall.HKEY_LOCAL_MACHINE, syscall.HKEY_CURRENT_USER} {
		folder, ok := regString(root, key, "EBWebView")
		if !ok || folder == "" {
			continue
		}
		version = filepath.Base(folder)
		if !verAtLeast(version, webview2MinVersion) {
			continue
		}
		dll = filepath.Join(folder, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
		if _, e := os.Stat(dll); e == nil {
			return dll, version, nil
		}
	}
	return "", "", errors.New("the WebView2 runtime is not installed")
}

func regString(root syscall.Handle, path, name string) (string, bool) {
	p, _ := syscall.UTF16PtrFromString(path)
	var h syscall.Handle
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ|0x0200 /* KEY_WOW64_32KEY */, &h) != nil {
		return "", false
	}
	defer syscall.RegCloseKey(h)
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, size uint32
	if syscall.RegQueryValueEx(h, n, nil, &typ, nil, &size) != nil || size == 0 || size > 1<<16 || (typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ) {
		return "", false
	}
	buf := make([]uint16, size/2+1)
	if syscall.RegQueryValueEx(h, n, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size) != nil {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

// ---- the main window ----------------------------------------------------------------------------------------------

var mw struct {
	hwnd       uintptr
	env, ctl   uintptr
	wv         uintptr
	url        string
	normal     winRect // last size and place while neither minimised nor maximised
	starting   bool
	shownOnce  bool
	hintShown  bool
	failedOnce bool
}

var (
	webviewFailed atomic.Bool
	mainProcCb    = syscall.NewCallback(mainProc)
)

// openWindow shows the window, or brings it to the front. Safe from any goroutine.
func openWindow(url string) {
	if uiMode == "app" || uiMode == "browser" || webviewFailed.Load() {
		openAppModeWindow(url)
		return
	}
	if h := trayWnd.Load(); h != 0 {
		pPostMessage.Call(h, wmShowMain, 0, 0)
		return
	}
	openAppModeWindow(url)
}

// showMain runs on the UI thread.
func showMain() {
	if mw.hwnd == 0 && !createMain() {
		go openAppModeWindow(trayURL)
		return
	}
	cmd := uintptr(swShow)
	if m, _, _ := pIsIconic.Call(mw.hwnd); m != 0 {
		cmd = swRestore
	} else if !mw.shownOnce && loadPlacement().Max {
		cmd = swShowMaximized // it was maximised when it was last closed: open it maximised again
	}
	mw.shownOnce = true
	pShowWindow.Call(mw.hwnd, cmd)
	pBringToTop.Call(mw.hwnd)
	pSetForeground.Call(mw.hwnd)
	if mw.ctl != 0 {
		comCall(mw.ctl, ctlPutIsVisible, 1)
		comCall(mw.ctl, ctlMoveFocus, focusProgrammatic)
	} else if mw.wv == 0 && !mw.starting {
		startWebView() // after a crash of the browser process
	}
}

func createMain() bool {
	inst, _, _ := pGetModuleHandle.Call(0)
	cls, _ := syscall.UTF16PtrFromString(AppName + "Main-" + appRootHash)
	cur, _, _ := pLoadCursor.Call(0, 32512)                                                           // IDC_ARROW
	wc := wndClassEx{Instance: inst, WndProc: mainProcCb, ClassName: cls, Cursor: cur, Background: 6, /* COLOR_WINDOW+1 */
		Icon: programIcon(inst, 32, 13, 141, 128), IconSm: programIcon(inst, 16, 13, 141, 128)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))) // fails harmlessly when already registered
	r := startRect()
	title, _ := syscall.UTF16PtrFromString(AppName)
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), wsOverlappedWin,
		uintptr(r.L), uintptr(r.T), uintptr(r.w()), uintptr(r.h()), 0, 0, inst, 0)
	if hwnd == 0 {
		logf("window: could not create the main window: %v", err)
		webviewFailed.Store(true)
		return false
	}
	mw.hwnd = hwnd
	s := dpiScale(hwnd)
	big, small := programIcon(inst, int(32*s), 13, 141, 128), programIcon(inst, int(16*s), 13, 141, 128)
	pSendMessage.Call(hwnd, wmSetIcon, 1, big)
	pSendMessage.Call(hwnd, wmSetIcon, 0, small)
	if systemUsesDarkMode() {
		on := int32(1)
		pDwmSetWindowAttr.Call(hwnd, 20 /* DWMWA_USE_IMMERSIVE_DARK_MODE */, uintptr(unsafe.Pointer(&on)), 4)
	}
	if !startWebView() {
		pDestroyWindow.Call(hwnd)
		mw.hwnd = 0
		webviewFailed.Store(true)
		return false
	}
	return true
}

// startWebView asks the runtime for an environment; the rest happens in onEnvDone and onCtlDone.
func startWebView() bool {
	if uiMode == "app" || uiMode == "browser" {
		return false
	}
	dll, ver, err := findWebView2()
	if err != nil {
		logf("window: %v; using an Edge or Chrome window instead", err)
		return false
	}
	d, err := syscall.LoadDLL(dll)
	if err != nil {
		logf("window: could not load %s: %v", dll, err)
		return false
	}
	create, err := d.FindProc("CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		logf("window: the WebView2 runtime %s has no entry point we know: %v", ver, err)
		return false
	}
	data := filepath.Join(appRoot, "data", "webview2")
	_ = os.MkdirAll(data, 0o755)
	dp, _ := syscall.UTF16PtrFromString(data)
	mw.starting = true
	hr, _, _ := create.Call(1, 0 /* installed runtime */, uintptr(unsafe.Pointer(dp)), optionsObj.ptr(), envDoneObj.ptr())
	runtime.KeepAlive(dp)
	if failed(hr) {
		mw.starting = false
		logf("window: WebView2 %s refused to start (0x%08x)", ver, uint32(hr))
		return false
	}
	logf("window: starting WebView2 %s", ver)
	return true
}

func onEnvDone(hr, env uintptr) {
	if failed(hr) || env == 0 {
		webViewGaveUp(fmt.Sprintf("no environment (0x%08x)", uint32(hr)))
		return
	}
	comCall(env, 1) // AddRef: we keep it
	mw.env = env
	if r := comCall(env, envCreateController, mw.hwnd, ctlDoneObj.ptr()); failed(r) {
		webViewGaveUp(fmt.Sprintf("no controller (0x%08x)", uint32(r)))
	}
}

func onCtlDone(hr, ctl uintptr) {
	mw.starting = false
	if failed(hr) || ctl == 0 {
		webViewGaveUp(fmt.Sprintf("the controller failed (0x%08x)", uint32(hr)))
		return
	}
	comCall(ctl, 1)
	mw.ctl = ctl
	var wv uintptr
	if r := comCall(ctl, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&wv))); failed(r) || wv == 0 {
		webViewGaveUp(fmt.Sprintf("no web view (0x%08x)", uint32(r)))
		return
	}
	mw.wv = wv
	var st uintptr
	if r := comCall(wv, wvGetSettings, uintptr(unsafe.Pointer(&st))); !failed(r) && st != 0 {
		comCall(st, setPutStatusBar, 0)
		comCall(st, setPutHostObjects, 0)
		if !debugMode {
			comCall(st, setPutDevTools, 0)
		}
		comCall(st, 2)
	}
	var token int64
	if r := comCall(wv, wvAddNewWindowRequestd, newWindowObj.ptr(), uintptr(unsafe.Pointer(&token))); failed(r) {
		logf("window: could not watch for new windows (0x%08x)", uint32(r))
	}
	if r := comCall(wv, wvAddProcessFailed, procFailedObj.ptr(), uintptr(unsafe.Pointer(&token))); failed(r) {
		logf("window: could not watch for crashes (0x%08x)", uint32(r))
	}
	resizeWebView()
	u, _ := syscall.UTF16PtrFromString(trayURL)
	if r := comCall(wv, wvNavigate, uintptr(unsafe.Pointer(u))); failed(r) {
		logf("window: could not open the page (0x%08x)", uint32(r))
	}
	runtime.KeepAlive(u)
	vis, _, _ := pIsWindowVis.Call(mw.hwnd)
	comCall(ctl, ctlPutIsVisible, vis)
	if vis != 0 {
		comCall(ctl, ctlMoveFocus, focusProgrammatic)
	}
	setUIHost("webview2")
	logf("window: ready")
}

// webViewGaveUp: WebView2 could not be used; the window is closed and the Edge/Chrome window takes over.
func webViewGaveUp(why string) {
	logf("window: WebView2 could not be used (%s); using an Edge or Chrome window instead", why)
	mw.starting = false
	releaseWebView()
	if mw.hwnd != 0 {
		pDestroyWindow.Call(mw.hwnd)
		mw.hwnd = 0
	}
	webviewFailed.Store(true)
	go openAppModeWindow(trayURL)
}

func releaseWebView() {
	if mw.wv != 0 {
		comCall(mw.wv, 2)
		mw.wv = 0
	}
	if mw.ctl != 0 {
		comCall(mw.ctl, ctlClose)
		comCall(mw.ctl, 2)
		mw.ctl = 0
	}
	if mw.env != 0 {
		comCall(mw.env, 2)
		mw.env = 0
	}
}

// onNewWindow: a page asked for a new window (a link with target=_blank). Web links open in the default browser;
// nothing else is opened.
func onNewWindow(args uintptr) {
	var p uintptr
	comCall(args, nwGetURI, uintptr(unsafe.Pointer(&p)))
	comCall(args, nwPutHandled, 1)
	if p == 0 {
		return
	}
	uri := utf16PtrToString(p)
	pCoFree.Call(p)
	if !safeExternalURL(uri) {
		logf("window: refused to open %q", uri)
		return
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	u, _ := syscall.UTF16PtrFromString(uri)
	pShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(u)), 0, 0, swShowNormal)
}

// onProcessFailed: a WebView2 process crashed. A page crash is reloaded; if the browser itself ended, the web view
// is made again (now if the window is open, else the next time it is opened).
func onProcessFailed(args uintptr) {
	var kind int32
	comCall(args, pfGetKind, uintptr(unsafe.Pointer(&kind)))
	logf("window: a WebView2 process ended (kind %d)", kind)
	if kind != 0 && mw.wv != 0 { // the page's process: reload it
		comCall(mw.wv, wvReload)
		return
	}
	releaseWebView()
	if v, _, _ := pIsWindowVis.Call(mw.hwnd); v != 0 && !mw.failedOnce {
		mw.failedOnce = true
		startWebView()
	}
}

func resizeWebView() {
	if mw.ctl == 0 || mw.hwnd == 0 {
		return
	}
	var r winRect
	pGetClientRect.Call(mw.hwnd, uintptr(unsafe.Pointer(&r)))
	comCall(mw.ctl, ctlPutBounds, uintptr(unsafe.Pointer(&r))) // a RECT is passed by address on x64
}

func mainProc(hwnd, msg, wp, lp uintptr) uintptr {
	defer func() {
		if r := recover(); r != nil {
			logf("window: recovered from a crash: %v", r)
		}
	}()
	switch uint32(msg) {
	case wmSize:
		if wp == 1 { // minimised
			if mw.ctl != 0 {
				comCall(mw.ctl, ctlPutIsVisible, 0)
			}
			return 0
		}
		if mw.ctl != 0 {
			comCall(mw.ctl, ctlPutIsVisible, 1)
		}
		resizeWebView()
		rememberNormal(hwnd)
		return 0
	case wmMove:
		if mw.ctl != 0 {
			comCall(mw.ctl, ctlNotifyParentMove)
		}
		rememberNormal(hwnd)
	case wmActivate, wmSetFocus:
		if mw.ctl != 0 && (uint32(msg) == wmSetFocus || wp&0xffff != 0) {
			comCall(mw.ctl, ctlMoveFocus, focusProgrammatic)
		}
	case wmGetMinMaxInfo:
		s := dpiScale(hwnd)
		mmi := (*[10]int32)(ptrFrom(lp)) // ptReserved, ptMaxSize, ptMaxPosition, ptMinTrackSize, ptMaxTrackSize
		mmi[6], mmi[7] = int32(float64(winMinW)*s), int32(float64(winMinH)*s)
		return 0
	case wmDpiChanged:
		r := (*winRect)(ptrFrom(lp))
		pSetWindowPos.Call(hwnd, 0, uintptr(r.L), uintptr(r.T), uintptr(r.w()), uintptr(r.h()), 0x0014) // SWP_NOZORDER|SWP_NOACTIVATE
		return 0
	case wmClose: // closing the window keeps the program running in the notification area
		savePlacement()
		pShowWindow.Call(hwnd, swHide)
		if mw.ctl != 0 {
			comCall(mw.ctl, ctlPutIsVisible, 0)
		}
		if !mw.hintShown && !closeHintSeen() {
			mw.hintShown = true
			showBalloon(AppName+" is still running", "Downloads continue. Click the icon here to open the window again.")
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}

func rememberNormal(hwnd uintptr) {
	if z, _, _ := pIsZoomed.Call(hwnd); z != 0 {
		return
	}
	if m, _, _ := pIsIconic.Call(hwnd); m != 0 {
		return
	}
	var r winRect
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if r.w() > 0 && r.h() > 0 {
		mw.normal = r
	}
}

func dpiScale(hwnd uintptr) float64 {
	if pGetDpiForWindow.Find() == nil && hwnd != 0 {
		if d, _, _ := pGetDpiForWindow.Call(hwnd); d >= 96 {
			return float64(d) / 96
		}
	}
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d >= 96 {
			return float64(d) / 96
		}
	}
	return 1
}

// ---- where the window opens ---------------------------------------------------------------------------------------

type placement struct {
	L, T, R, B int
	Max        bool
}

func placementFile() string { return filepath.Join(appRoot, "data", "window.json") }

func loadPlacement() placement {
	var p placement
	if b, err := os.ReadFile(placementFile()); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

// windowPlacement is WINDOWPLACEMENT: Windows keeps the normal size and place of a window even while it is
// maximised (and on which monitor), so that is what is saved, not something tracked by hand.
type windowPlacement struct {
	Length  uint32
	Flags   uint32
	ShowCmd uint32
	PtMin   [2]int32
	PtMax   [2]int32
	Normal  winRect
}

var pGetWindowPlacement = user32.NewProc("GetWindowPlacement")

func savePlacement() {
	if mw.hwnd == 0 {
		return
	}
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := pGetWindowPlacement.Call(mw.hwnd, uintptr(unsafe.Pointer(&wp))); r == 0 || wp.Normal.w() <= 0 {
		return
	}
	p := placement{L: wp.Normal.L, T: wp.Normal.T, R: wp.Normal.R, B: wp.Normal.B}
	z, _, _ := pIsZoomed.Call(mw.hwnd)
	m, _, _ := pIsIconic.Call(mw.hwnd)
	p.Max = z != 0 || (m != 0 && wp.Flags&2 != 0) // WPF_RESTORETOMAXIMIZED: minimised from maximised
	if b, err := json.Marshal(p); err == nil {
		_ = os.WriteFile(placementFile(), b, 0o644)
	}
}

func startRect() winRect {
	p := loadPlacement()
	mons, primary := monitors()
	r := fixWindowRectScaled(winRect{p.L, p.T, p.R, p.B}, mons, primary, dpiScale(0))
	mw.normal = r
	return r
}

var (
	monList    []winRect
	monPrimary winRect
	monCb      = syscall.NewCallback(func(hmon, hdc, rc, data uintptr) uintptr {
		var mi struct {
			Size  uint32
			Mon   winRect
			Work  winRect
			Flags uint32
		}
		mi.Size = uint32(unsafe.Sizeof(mi))
		if r, _, _ := pGetMonitorInfo.Call(hmon, uintptr(unsafe.Pointer(&mi))); r != 0 {
			monList = append(monList, mi.Work)
			if mi.Flags&1 != 0 {
				monPrimary = mi.Work
			}
		}
		return 1
	})
)

// monitors returns the work areas of all monitors and of the main one (UI thread only).
func monitors() ([]winRect, winRect) {
	monList, monPrimary = nil, winRect{}
	pEnumMonitors.Call(0, 0, monCb, 0)
	if monPrimary == (winRect{}) && len(monList) > 0 {
		monPrimary = monList[0]
	}
	return monList, monPrimary
}

// closeHintSeen: the "still running" notice is shown once per installation.
func closeHintSeen() bool {
	f := filepath.Join(appRoot, "data", "close-hint-shown")
	if _, err := os.Stat(f); err == nil {
		return true
	}
	_ = os.WriteFile(f, []byte(time.Now().Format(time.RFC3339)), 0o644)
	return false
}

func systemUsesDarkMode() bool {
	v, ok := regDWORD(syscall.HKEY_CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "AppsUseLightTheme")
	return ok && v == 0
}

func regDWORD(root syscall.Handle, path, name string) (uint32, bool) {
	p, _ := syscall.UTF16PtrFromString(path)
	var h syscall.Handle
	if syscall.RegOpenKeyEx(root, p, 0, syscall.KEY_READ, &h) != nil {
		return 0, false
	}
	defer syscall.RegCloseKey(h)
	n, _ := syscall.UTF16PtrFromString(name)
	var typ, v uint32
	size := uint32(4)
	if syscall.RegQueryValueEx(h, n, nil, &typ, (*byte)(unsafe.Pointer(&v)), &size) != nil || typ != syscall.REG_DWORD {
		return 0, false
	}
	return v, true
}

func utf16PtrToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*uint16)(ptrFrom(p + uintptr(n)*2)) != 0 && n < 1<<16 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(ptrFrom(p)), n))
}

// uiInit runs once at start, before any window exists: sharp text on high-DPI screens, and the program's own
// taskbar identity (so its window is not grouped with Edge).
func uiInit() {
	comInit()
	dpiSet := false
	if pSetDpiCtx.Find() == nil {
		r, _, _ := pSetDpiCtx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
		dpiSet = r != 0
	}
	if !dpiSet {
		pSetDPIAware.Call()
	}
	if id, err := syscall.UTF16PtrFromString(appID); err == nil {
		pSetAppID.Call(uintptr(unsafe.Pointer(id)))
	}
}

// programIcon is the program's own icon (from the resources built by scripts/mkres) at n pixels, or the drawn one
// when the program was built without them.
func programIcon(inst uintptr, n int, r, g, b byte) uintptr {
	if h, _, _ := pLoadImage.Call(inst, 1, 1 /* IMAGE_ICON */, uintptr(n), uintptr(n), 0); h != 0 {
		return h
	}
	if n > 24 { // the drawn icon needs a width the mask rows divide into
		return makeIconSized(32, r, g, b)
	}
	return makeIconSized(16, r, g, b)
}
