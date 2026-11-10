package main

// The Windows side of copied links: the tray window listens for clipboard changes (AddClipboardFormatListener, no
// polling) only while the setting is on, and reads the copied text when it changes.

import (
	"syscall"
	"unsafe"
)

const (
	wmClipboardUpdate = 0x031D
	wmClipWatch       = 0x0400 + 11 // posted to the tray window: wParam 1 = listen, 0 = stop
	cfUnicodeText     = 13
)

var (
	pAddClipListener = user32.NewProc("AddClipboardFormatListener")
	pRemClipListener = user32.NewProc("RemoveClipboardFormatListener")
	pOpenClipboard   = user32.NewProc("OpenClipboard")
	pCloseClipboard  = user32.NewProc("CloseClipboard")
	pGetClipData     = user32.NewProc("GetClipboardData")
	pGlobalLock      = kernel32.NewProc("GlobalLock")
	pGlobalUnlock    = kernel32.NewProc("GlobalUnlock")
	pGlobalSize      = kernel32.NewProc("GlobalSize")
	clipListening    bool
)

// clipWatch switches listening on or off; safe from any goroutine.
func clipWatch(on bool) {
	if h := trayWnd.Load(); h != 0 {
		w := uintptr(0)
		if on {
			w = 1
		}
		pPostMessage.Call(h, wmClipWatch, w, 0)
	}
}

// clipListen runs on the tray thread.
func clipListen(hwnd uintptr, on bool) {
	if on == clipListening {
		return
	}
	if on {
		if r, _, err := pAddClipListener.Call(hwnd); r == 0 {
			logf("copied links: cannot listen to the clipboard: %v", err)
			return
		}
	} else {
		pRemClipListener.Call(hwnd)
	}
	clipListening = on
	logf("copied links: %v", on)
}

// clipText reads the copied text (tray thread). The clipboard can be busy for a moment in another program; then
// the change is simply skipped.
func clipText(hwnd uintptr) string {
	if r, _, _ := pOpenClipboard.Call(hwnd); r == 0 {
		return ""
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	size, _, _ := pGlobalSize.Call(h)
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer pGlobalUnlock.Call(h)
	n := int(size / 2)
	if n > 8192 {
		n = 8192 // a link is never this long; do not copy a whole document
	}
	u := unsafe.Slice((*uint16)(ptrFrom(p)), n)
	for i, c := range u {
		if c == 0 {
			u = u[:i]
			break
		}
	}
	return syscall.UTF16ToString(u)
}

func onClipboardUpdate(hwnd uintptr) {
	t := clipText(hwnd)
	if t == "" {
		return
	}
	fg, _, _ := pGetForeground.Call()
	ours := fg != 0 && fg == mw.hwnd
	go safely("copied link", func() { offerCopiedLink(t, ours) })
}
