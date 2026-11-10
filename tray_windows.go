//go:build windows

package main

// The notification-area icon and the single-instance hand-over. Ported from NetMerge's sys_tray_windows.go.
// Click: open the window. Right-click: Open, Pause all, Resume all, Quit. The tooltip shows what is downloading.
// The same hidden window receives a second start's message (WM_COPYDATA) and then shows the window.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"pullwell/dm"
)

var (
	pRegisterClassEx  = user32.NewProc("RegisterClassExW")
	pCreateWindowEx   = user32.NewProc("CreateWindowExW")
	pDefWindowProc    = user32.NewProc("DefWindowProcW")
	pGetMessage       = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessage  = user32.NewProc("DispatchMessageW")
	pPostMessage      = user32.NewProc("PostMessageW")
	pSendMessageTO    = user32.NewProc("SendMessageTimeoutW")
	pSetTimer         = user32.NewProc("SetTimer")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenu       = user32.NewProc("AppendMenuW")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pCreateIcon       = user32.NewProc("CreateIcon")
	pRegisterWinMsg   = user32.NewProc("RegisterWindowMessageW")
	pFindWindow       = user32.NewProc("FindWindowW")
	pAllowSetFg       = user32.NewProc("AllowSetForegroundWindow")
	pShellNotifyIcon  = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	pCreateMutex      = kernel32.NewProc("CreateMutexW")
	pCloseHandle      = kernel32.NewProc("CloseHandle")
)

const (
	wmTray       = 0x0400 + 1 // WM_USER+1
	wmQuitTray   = 0x0400 + 2
	wmTimer      = 0x0113
	wmClose      = 0x0010
	wmCopyData   = 0x004A
	wmLBtnUp     = 0x0202
	wmRBtnUp     = 0x0205
	ninBalloonUp = 0x0405 // NIN_BALLOONUSERCLICK
	nimAdd       = 0
	nimModify    = 1
	nimDelete    = 2
	nifMessage   = 1
	nifIcon      = 2
	nifTip       = 4
	nifInfo      = 0x10
	errExists    = 183 // ERROR_ALREADY_EXISTS
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        [16]byte
	BalloonIcon     uintptr
}

type winMsg struct {
	Wnd     uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
}

type copyDataStruct struct {
	Data uintptr
	Size uint32
	Ptr  uintptr
}

var (
	trayWnd        atomic.Uintptr
	trayURL        string
	trayIdle       uintptr
	trayBusy       uintptr
	trayTaskbarMsg uint32
	trayClass      string
	appRootHash    string
	trayProcCb     = syscall.NewCallback(trayProc)
)

// instanceKey is a short hash of the data folder: one copy runs per data folder (design D5).
func instanceKey(root string) string {
	h := sha256.Sum256([]byte(strings.ToLower(root)))
	return hex.EncodeToString(h[:6])
}

// instanceStart takes the single-instance lock. When another copy already runs for this data folder, it asks that
// copy to show its window (unless started with -tray) and returns first=false.
func instanceStart(root string, tray bool, link string) (release func(), first bool) {
	appRootHash = instanceKey(root)
	trayClass = AppName + "Tray-" + appRootHash
	name, _ := syscall.UTF16PtrFromString(`Local\` + AppName + "-" + appRootHash)
	h, _, err := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		logf("single instance: could not create the lock (%v); starting anyway", err)
		return func() {}, true
	}
	if errno, ok := err.(syscall.Errno); !ok || errno != errExists {
		return func() { pCloseHandle.Call(h) }, true
	}
	pCloseHandle.Call(h)
	if link != "" {
		handOver("add " + link)
	} else if !tray {
		handOver("show")
	}
	return nil, false
}

// handOver sends a command to the running copy and lets it take the focus (this copy was started by the person, so
// Windows lets it pass that right on; design section 3).
func handOver(cmd string) {
	cls, _ := syscall.UTF16PtrFromString(trayClass)
	var hwnd uintptr
	for i := 0; i < 50 && hwnd == 0; i++ { // the first copy may still be starting
		hwnd, _, _ = pFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
		if hwnd == 0 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if hwnd == 0 {
		logf("single instance: the running copy has no window yet; nothing handed over")
		return
	}
	var pid uint32
	pWinThread.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	pAllowSetFg.Call(uintptr(pid))
	b := []byte(cmd)
	cds := copyDataStruct{Data: 1, Size: uint32(len(b)), Ptr: uintptr(unsafe.Pointer(&b[0]))}
	var res uintptr
	pSendMessageTO.Call(hwnd, wmCopyData, 0, uintptr(unsafe.Pointer(&cds)), 0x0002, 5000, uintptr(unsafe.Pointer(&res))) // SMTO_ABORTIFHUNG
	runtime.KeepAlive(b)
}

// makeIcon draws the 16x16 tray icon: a coloured circle with a white down arrow.
func makeIcon(r, g, b byte) uintptr { return makeIconSized(16, r, g, b) }

// makeIconSized draws the icon at n x n pixels (16 for the tray and title bar, 32 for the taskbar).
func makeIconSized(n int, r, g, b byte) uintptr {
	xor := make([]byte, n*n*4)
	and := make([]byte, n*n/8)
	c, rad := float64(n-1)/2, float64(n)*0.45
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-c, float64(y)-c
			i := (y*n + x) * 4
			if dx*dx+dy*dy <= rad*rad {
				xor[i], xor[i+1], xor[i+2], xor[i+3] = b, g, r, 255
				// a white down arrow: a shaft and a head, scaled from the 16-pixel drawing
				fx, fy := float64(x)*16/float64(n), float64(y)*16/float64(n)
				shaft := fx >= 6.8 && fx < 9.2 && fy >= 3 && fy < 10
				head := fy >= 8 && fy < 12 && fx >= 4+(fy-8) && fx < 12-(fy-8)
				if shaft || head {
					xor[i], xor[i+1], xor[i+2] = 255, 255, 255
				}
			}
		}
	}
	h, _, _ := pCreateIcon.Call(0, uintptr(n), uintptr(n), 1, 32, uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
	return h
}

// trayTip: "<name> - 2 downloading, 4.2 MB/s" (Windows allows 127 characters).
func trayTip() (string, bool) {
	b := dm.Brief()
	active, _ := b["active"].(int)
	queued, _ := b["queued"].(int)
	speed, _ := b["speed"].(float64)
	return trayTipText(active, queued, speed), active > 0
}

func trayNotify(msg uintptr) {
	hwnd := trayWnd.Load()
	if hwnd == 0 {
		return
	}
	tip, busy := trayTip()
	nid := notifyIconData{Wnd: hwnd, ID: 1, Flags: nifMessage | nifIcon | nifTip, CallbackMessage: wmTray, Icon: trayIdle}
	nid.Size = uint32(unsafe.Sizeof(nid))
	if busy {
		nid.Icon = trayBusy
	}
	t, _ := syscall.UTF16FromString(tip)
	copy(nid.Tip[:127], t)
	pShellNotifyIcon.Call(msg, uintptr(unsafe.Pointer(&nid)))
}

func trayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	add := func(id uintptr, text string) {
		p, _ := syscall.UTF16PtrFromString(text)
		pAppendMenu.Call(menu, 0, id, uintptr(unsafe.Pointer(p)))
	}
	add(1, "Open "+AppName)
	add(5, "Add a download...")
	add(6, "Open the download folder")
	pAppendMenu.Call(menu, 0x800, 0, 0)
	add(2, "Pause all")
	add(3, "Resume all")
	pAppendMenu.Call(menu, 0x800, 0, 0) // separator
	add(4, "Quit "+AppName)
	var pt [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForeground.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, 0x100|0x80, uintptr(pt[0]), uintptr(pt[1]), 0, hwnd, 0) // TPM_RETURNCMD|TPM_NONOTIFY
	pPostMessage.Call(hwnd, 0, 0, 0)
	switch cmd {
	case 1:
		go safely("tray open", func() { openWindow(trayURL) })
	case 2:
		go safely("pause all", dm.PauseAll)
	case 3:
		go safely("resume all", dm.ResumeAll)
	case 4:
		requestQuit()
	case 5: // the Add dialog, with the copied link in it when there is one
		l := copiedLink(clipText(hwnd))
		if l == "" {
			l = " "
		}
		setPendingAdd(l)
		go safely("tray add", func() { openWindow(trayURL) })
	case 6:
		go safely("tray folder", func() {
			dir := dm.DownloadDir()
			_ = os.MkdirAll(dir, 0o755)
			_ = sysOpenPath(dir, false)
		})
	}
}

func trayProc(hwnd, msg, wp, lp uintptr) uintptr {
	defer func() {
		if r := recover(); r != nil {
			logf("tray: recovered from a crash: %v", r)
		}
	}()
	switch uint32(msg) {
	case wmTray:
		switch lp & 0xffff {
		case ninBalloonUp:
			logf("tray: notification clicked")
			balloonClicked() // the Add dialog for a copied link, or the finished / problem downloads
			go safely("tray open", func() { openWindow(trayURL) })
		case wmLBtnUp:
			go safely("tray open", func() { openWindow(trayURL) })
		case wmRBtnUp:
			trayMenu(hwnd)
		}
		return 0
	case wmCopyData: // a second start
		cds := (*copyDataStruct)(ptrFrom(lp))
		if cds != nil && cds.Data == 1 && cds.Size > 0 && cds.Size <= 9000 {
			cmd := string(unsafe.Slice((*byte)(ptrFrom(cds.Ptr)), cds.Size))
			logf("second start: %q", cmd)
			if cmd == "show" {
				go safely("show", func() { openWindow(trayURL) })
			} else if l, ok := strings.CutPrefix(cmd, "add "); ok && isWebLink(l) {
				setPendingAdd(l) // started with a link (the program followed by a link): the Add dialog shows it
				go safely("show", func() { openWindow(trayURL) })
			}
		}
		return 1
	case wmTimer:
		trayNotify(nimModify)
		return 0
	case wmShowMain:
		showMain()
		return 0
	case wmClipWatch:
		clipListen(hwnd, wp == 1)
		return 0
	case wmClipboardUpdate:
		onClipboardUpdate(hwnd)
		return 0
	case wmQuitTray, wmClose:
		savePlacement()
		trayNotify(nimDelete)
		trayWnd.Store(0)
		pPostMessage.Call(0, 0x0012, 0, 0) // WM_QUIT
		return 0
	}
	if trayTaskbarMsg != 0 && uint32(msg) == trayTaskbarMsg { // Explorer restarted: put the icon back
		trayNotify(nimAdd)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}

// runUI runs the tray icon and opens the window; it returns when the person quits.
func runUI(url string, showWindow bool) {
	uiInit()
	trayURL = url
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		trayLoop(ready)
	}()
	<-ready
	if showWindow {
		go safely("open window", func() { openWindow(url) })
	}
	<-quitCh
	if h := trayWnd.Load(); h != 0 {
		pPostMessage.Call(h, wmQuitTray, 0, 0)
	}
	time.Sleep(200 * time.Millisecond) // let the icon go away before the process ends
}

func trayLoop(ready chan struct{}) {
	readyOnce := func() {
		select {
		case <-ready:
		default:
			close(ready)
		}
	}
	defer readyOnce()
	pCoInit.Call(0, 2) // single-threaded COM apartment: WebView2 needs it on this thread
	trayIdle, trayBusy = makeIcon(110, 120, 130), makeIcon(14, 159, 144)
	inst, _, _ := pGetModuleHandle.Call(0)
	cls, _ := syscall.UTF16PtrFromString(trayClass)
	wc := wndClassEx{Instance: inst, WndProc: trayProcCb, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		logf("tray: could not register the window class")
		return
	}
	title, _ := syscall.UTF16PtrFromString(AppName)
	hwnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		logf("tray: could not create the window")
		return
	}
	tb, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	m, _, _ := pRegisterWinMsg.Call(uintptr(unsafe.Pointer(tb)))
	trayTaskbarMsg = uint32(m)
	trayWnd.Store(hwnd)
	if clipboardOn() {
		clipListen(hwnd, true)
	}
	trayNotify(nimAdd)
	pSetTimer.Call(hwnd, 1, 2000, 0)
	readyOnce()
	var msg winMsg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// showBalloon shows a short notification next to the tray icon.
func showBalloon(title, text string) {
	hwnd := trayWnd.Load()
	if hwnd == 0 {
		return
	}
	setBalloonLink("") // a new notification replaces any copied-link offer on screen
	setBalloonView("")
	nid := notifyIconData{Wnd: hwnd, ID: 1, Flags: nifInfo, InfoFlags: 1}
	if ic := balloonIcon(); ic != 0 {
		nid.InfoFlags, nid.BalloonIcon = 4|0x20, ic // NIIF_USER | NIIF_LARGE_ICON: the program's own icon
	}
	nid.Size = uint32(unsafe.Sizeof(nid))
	t, _ := syscall.UTF16FromString(text)
	copy(nid.Info[:255], t)
	h, _ := syscall.UTF16FromString(title)
	copy(nid.InfoTitle[:63], h)
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// ptrFrom turns an address that Windows passed in a message into a pointer. The memory belongs to Windows (or to
// the sending process, mapped for the duration of the message), so the Go garbage collector never moves it.
func ptrFrom(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func safely(what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			logf("%s: recovered from a crash: %v", what, fmt.Sprint(r))
		}
	}()
	f()
}

var balloonIco uintptr

// balloonIcon is the program's icon at the size Windows uses for large notification icons (loaded once; 0 when the
// program was built without its icon resource, and then Windows shows its "information" icon).
func balloonIcon() uintptr {
	if balloonIco == 0 {
		inst, _, _ := pGetModuleHandle.Call(0)
		n := int(32 * dpiScale(0))
		balloonIco, _, _ = pLoadImage.Call(inst, 1, 1, uintptr(n), uintptr(n), 0)
	}
	return balloonIco
}

// showMainWindow brings up the main window (from any goroutine).
func showMainWindow() { safely("show window", func() { openWindow(trayURL) }) }
