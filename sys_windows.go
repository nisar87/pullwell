//go:build windows

package main

// Windows functions the engine needs. Ported from NetMerge's sys_windows.go (tested on real PCs), without PowerShell.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const createNoWindow = 0x08000000

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	user32         = syscall.NewLazyDLL("user32.dll")
	shell32        = syscall.NewLazyDLL("shell32.dll")
	ole32          = syscall.NewLazyDLL("ole32.dll")
	pCoInit        = ole32.NewProc("CoInitializeEx")
	pCoUninit      = ole32.NewProc("CoUninitialize")
	pCoFree        = ole32.NewProc("CoTaskMemFree")
	pParseName     = shell32.NewProc("SHParseDisplayName")
	pOpenSelect    = shell32.NewProc("SHOpenFolderAndSelectItems")
	pMessageBox    = user32.NewProc("MessageBoxW")
	pEnumWindows   = user32.NewProc("EnumWindows")
	pGetClassName  = user32.NewProc("GetClassNameW")
	pGetWindowText = user32.NewProc("GetWindowTextW")
	pIsWindowVis   = user32.NewProc("IsWindowVisible")
	pIsIconic      = user32.NewProc("IsIconic")
	pShowWindow    = user32.NewProc("ShowWindow")
	pBringToTop    = user32.NewProc("BringWindowToTop")
	pSetForeground = user32.NewProc("SetForegroundWindow")
	pKeybdEvent    = user32.NewProc("keybd_event")
	pGetForeground = user32.NewProc("GetForegroundWindow")
	pLoadImage     = user32.NewProc("LoadImageW")
	pWinThread     = user32.NewProc("GetWindowThreadProcessId")
	pAttachInput   = user32.NewProc("AttachThreadInput")
	pSetWindowPos  = user32.NewProc("SetWindowPos")
	pSwitchTo      = user32.NewProc("SwitchToThisWindow")
	pCurThread     = kernel32.NewProc("GetCurrentThreadId")
	pDiskFree      = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func hiddenCmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return c
}

func showError(msg string) {
	t, _ := syscall.UTF16PtrFromString(AppName)
	m, _ := syscall.UTF16PtrFromString(msg)
	pMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10) // MB_ICONERROR
}

// ---- the window: Edge or Chrome in "app" mode for now (WebView2 comes in a later step) ------------------------------

var (
	focusCb         = syscall.NewCallback(enumFocus)
	focusMu         sync.Mutex
	focusedExisting bool
	uiHostV         atomic.Value // how the page is shown: "webview2", "app-window" or "browser"
)

func setUIHost(h string) { uiHostV.Store(h) }

func uiHostName() string {
	if h, ok := uiHostV.Load().(string); ok {
		return h
	}
	return "app-window"
}

// enumFocus looks for the app window (an Edge/Chrome "app" window whose title is the product name) and brings it up.
func enumFocus(hwnd, _ uintptr) uintptr {
	if v, _, _ := pIsWindowVis.Call(hwnd); v == 0 {
		return 1
	}
	cls := make([]uint16, 64)
	pGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 64)
	if syscall.UTF16ToString(cls) != "Chrome_WidgetWin_1" {
		return 1
	}
	title := make([]uint16, 128)
	pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), 128)
	if !strings.HasPrefix(syscall.UTF16ToString(title), AppName) {
		return 1
	}
	if m, _, _ := pIsIconic.Call(hwnd); m != 0 {
		pShowWindow.Call(hwnd, 9) // SW_RESTORE
	}
	pBringToTop.Call(hwnd)
	pSetForeground.Call(hwnd)
	focusedExisting = true
	return 0
}

func focusExistingWindow() bool {
	focusMu.Lock()
	defer focusMu.Unlock()
	focusedExisting = false
	pEnumWindows.Call(focusCb, 0)
	return focusedExisting
}

// openAppModeWindow shows the page in an Edge or Chrome "app" window (the fallback when WebView2 cannot be used),
// or brings the open one to the front.
func openAppModeWindow(url string) {
	if uiMode == "browser" {
		setUIHost("browser")
		_ = hiddenCmd("rundll32", "url.dll,FileProtocolHandler", url).Start()
		return
	}
	if focusExistingWindow() {
		return
	}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("LocalAppData"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("LocalAppData"), `Google\Chrome\Application\chrome.exe`),
	}
	profile := filepath.Join(appRoot, "data", "ui-profile")
	for _, c := range candidates {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		cmd := exec.Command(c, "--app="+url, "--user-data-dir="+profile, "--window-size=1180,860",
			"--no-first-run", "--no-default-browser-check", "--disable-extensions")
		if err := cmd.Start(); err == nil {
			setUIHost("app-window")
			go cmd.Wait()
			return
		}
	}
	logf("no Edge or Chrome found: opening the page in the default browser")
	setUIHost("browser")
	_ = hiddenCmd("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// ---- functions the download engine needs ---------------------------------------------------------------------------

// sysOpenPath opens a file with its normal program, or shows it selected in Explorer. The command line is written by
// hand because Explorer and rundll32 want the path in quotes exactly like this, spaces and all.
func sysOpenPath(path string, reveal bool) error {
	path = filepath.Clean(path)
	if reveal {
		if err := revealInShell(path); err == nil {
			return nil
		}
		c := hiddenCmd("explorer.exe")
		c.SysProcAttr.CmdLine = `explorer.exe /select,"` + path + `"`
		return c.Start()
	}
	c := hiddenCmd("rundll32.exe")
	c.SysProcAttr.CmdLine = `rundll32.exe url.dll,FileProtocolHandler "` + path + `"`
	return c.Start()
}

// revealInShell opens the file's folder in Explorer with the file selected (SHOpenFolderAndSelectItems).
func revealInShell(path string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	hr, _, _ := pCoInit.Call(0, 2) // apartment threaded
	if int32(hr) >= 0 {
		defer pCoUninit.Call()
	}
	var pidl uintptr
	hr, _, _ = pParseName.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
	if int32(hr) < 0 || pidl == 0 {
		return fmt.Errorf("the shell could not find %s (0x%x)", path, uint32(hr))
	}
	defer pCoFree.Call(pidl)
	hr, _, _ = pOpenSelect.Call(pidl, 0, 0, 0)
	if int32(hr) < 0 {
		return fmt.Errorf("the shell could not show %s (0x%x)", path, uint32(hr))
	}
	// Explorer opens behind the window we were clicked in; bring it up (NetMerge 3.11.13-3.11.16, handover 7.4).
	go raiseExplorer(filepath.Base(filepath.Dir(path)))
	return nil
}

var (
	raiseMu   sync.Mutex
	raiseWant string
	raiseCb   = syscall.NewCallback(enumRaise)
	raiseDone bool
	raiseSeen []string
)

func enumRaise(hwnd, _ uintptr) uintptr {
	if v, _, _ := pIsWindowVis.Call(hwnd); v == 0 {
		return 1
	}
	cls := make([]uint16, 64)
	pGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), 64)
	if syscall.UTF16ToString(cls) != "CabinetWClass" {
		return 1
	}
	title := make([]uint16, 260)
	pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), 260)
	// the title is the folder name, on some Windows builds followed by " - File Explorer"
	tt := strings.ToLower(syscall.UTF16ToString(title))
	w := strings.ToLower(raiseWant)
	if tt != w && !strings.HasPrefix(tt, w+" - ") {
		raiseSeen = append(raiseSeen, syscall.UTF16ToString(title))
		return 1
	}
	if m, _, _ := pIsIconic.Call(hwnd); m != 0 {
		pShowWindow.Call(hwnd, 9)
	}
	fg, _, _ := pGetForeground.Call()
	var fgThread uintptr
	if fg != 0 {
		fgThread, _, _ = pWinThread.Call(fg, 0)
	}
	me, _, _ := pCurThread.Call()
	attached := false
	if fgThread != 0 && fgThread != me {
		r, _, _ := pAttachInput.Call(me, fgThread, 1)
		attached = r != 0
	}
	pKeybdEvent.Call(0x12, 0, 0, 0)
	pKeybdEvent.Call(0x12, 0, 2, 0)
	pBringToTop.Call(hwnd)
	ok, _, _ := pSetForeground.Call(hwnd)
	pSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, 0, 0, 0x0043) // HWND_TOPMOST, no move/size, show
	pSetWindowPos.Call(hwnd, ^uintptr(1), 0, 0, 0, 0, 0x0043) // HWND_NOTOPMOST
	pSwitchTo.Call(hwnd, 1)
	if attached {
		pAttachInput.Call(me, fgThread, 0)
	}
	logf("show in folder: raised the %q window (focus accepted: %v)", raiseWant, ok != 0)
	raiseDone = true
	return 0
}

func raiseExplorer(folder string) {
	raiseMu.Lock()
	defer raiseMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	raiseWant = folder
	for i := 0; i < 15; i++ {
		raiseDone = false
		raiseSeen = nil
		pEnumWindows.Call(raiseCb, 0)
		if raiseDone {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	logf("show in folder: no Explorer window titled %q found (Explorer windows seen: %q)", folder, raiseSeen)
}

// sysPower sleeps or shuts the PC down after the downloads (a shutdown has a one-minute warning that can be cancelled).
func sysPower(kind string) {
	switch kind {
	case "shutdown":
		_ = hiddenCmd("shutdown.exe", "/s", "/t", "60", "/c", AppName+": downloads finished").Start()
	case "sleep":
		_ = hiddenCmd("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Start()
	}
}

func sysPowerCancel() error { return hiddenCmd("shutdown.exe", "/a").Run() }

// sysFreeSpace returns the bytes a normal program can still write to the drive that holds dir.
func sysFreeSpace(dir string) (int64, error) {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	r, _, e := pDiskFree.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		return 0, e
	}
	return int64(avail), nil
}

// sysMarkFromInternet writes the "this file came from the internet" mark that browsers write, so SmartScreen and
// Office protected view treat the file like any other download.
func sysMarkFromInternet(path, link string) {
	ref := ""
	if link != "" && len(link) < 2000 && !strings.ContainsAny(link, "\r\n") {
		if i := strings.IndexByte(link, '?'); i >= 0 {
			link = link[:i]
		}
		ref = "HostUrl=" + link + "\r\n"
	}
	_ = os.WriteFile(path+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n"+ref), 0o644)
}

// sysRun runs the after-download command without a shell and without a window, and waits (at most an hour).
func sysRun(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("no command")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return c.Run()
}
