package main

// Real Windows notifications (toasts) with buttons. A toast belongs to the program's AppUserModelID, which Windows
// knows from the Start menu entry; without that entry Windows would drop the toast silently, so then the classic
// tray balloon is used instead. A click on the toast or one of its buttons opens a pullwell:// link (registered for
// the current user), which Windows hands to the program; applink.go checks it.

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	combase           = syscall.NewLazyDLL("combase.dll")
	pRoInitialize     = combase.NewProc("RoInitialize")
	pRoGetFactory     = combase.NewProc("RoGetActivationFactory")
	pRoActivate       = combase.NewProc("RoActivateInstance")
	pWinCreateString  = combase.NewProc("WindowsCreateString")
	pWinDeleteString  = combase.NewProc("WindowsDeleteString")
	iidToastMgrStatic = mustGUID("{50ac103f-d235-4598-bbef-98fe4d1a3ad4}")
	iidToastFactory   = mustGUID("{04124b20-82c6-4229-b109-fd9ed4662b53}")
	iidXmlDocIO       = mustGUID("{6cd0e74e-ee65-4489-9ebf-ca43e87ba637}")
	iidXmlDoc         = mustGUID("{f7f3a506-1e87-42d6-bcfb-b8c809fa5494}")

	toastCh   chan string // toast XML, shown by the toast thread
	toastOnce sync.Once
	toastBad  bool // the toast machinery failed once: balloons from then on
	toastMu   sync.Mutex
)

func hstring(s string) (uintptr, error) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	if hr, _, _ := pWinCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h))); failed(hr) {
		return 0, fmt.Errorf("WindowsCreateString %#x", hr)
	}
	return h, nil
}

// showToastXML shows one toast on the toast thread (WinRT wants a thread of its own, set up for it).
func showToastXML(x string) error {
	if err := pRoInitialize.Find(); err != nil {
		return err
	}
	toastOnce.Do(func() {
		toastCh = make(chan string, 16)
		go func() {
			runtime.LockOSThread()
			pRoInitialize.Call(1) // RO_INIT_MULTITHREADED
			for x := range toastCh {
				if err := toastNow(x); err != nil {
					logf("notification: Windows refused the toast (%v); using the classic notification", err)
					toastMu.Lock()
					toastBad = true
					toastMu.Unlock()
				}
			}
		}()
	})
	select {
	case toastCh <- x:
		return nil
	default:
		return errors.New("too many notifications at once")
	}
}

func toastNow(x string) error {
	release := func(p uintptr) {
		if p != 0 {
			comCall(p, 2)
		}
	}
	clsXML, err := hstring("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return err
	}
	defer pWinDeleteString.Call(clsXML)
	var insp uintptr
	if hr, _, _ := pRoActivate.Call(clsXML, uintptr(unsafe.Pointer(&insp))); failed(hr) || insp == 0 {
		return fmt.Errorf("XmlDocument %#x", hr)
	}
	defer release(insp)
	var io uintptr
	if hr := comCall(insp, 0, uintptr(unsafe.Pointer(&iidXmlDocIO)), uintptr(unsafe.Pointer(&io))); failed(hr) {
		return fmt.Errorf("IXmlDocumentIO %#x", hr)
	}
	defer release(io)
	hx, err := hstring(x)
	if err != nil {
		return err
	}
	defer pWinDeleteString.Call(hx)
	if hr := comCall(io, 6, hx); failed(hr) { // LoadXml
		return fmt.Errorf("LoadXml %#x", hr)
	}
	var doc uintptr
	if hr := comCall(insp, 0, uintptr(unsafe.Pointer(&iidXmlDoc)), uintptr(unsafe.Pointer(&doc))); failed(hr) {
		return fmt.Errorf("IXmlDocument %#x", hr)
	}
	defer release(doc)

	clsToast, _ := hstring("Windows.UI.Notifications.ToastNotification")
	defer pWinDeleteString.Call(clsToast)
	var fac uintptr
	if hr, _, _ := pRoGetFactory.Call(clsToast, uintptr(unsafe.Pointer(&iidToastFactory)), uintptr(unsafe.Pointer(&fac))); failed(hr) {
		return fmt.Errorf("ToastNotification factory %#x", hr)
	}
	defer release(fac)
	var toast uintptr
	if hr := comCall(fac, 6, doc, uintptr(unsafe.Pointer(&toast))); failed(hr) { // CreateToastNotification
		return fmt.Errorf("CreateToastNotification %#x", hr)
	}
	defer release(toast)

	clsMgr, _ := hstring("Windows.UI.Notifications.ToastNotificationManager")
	defer pWinDeleteString.Call(clsMgr)
	var mgr uintptr
	if hr, _, _ := pRoGetFactory.Call(clsMgr, uintptr(unsafe.Pointer(&iidToastMgrStatic)), uintptr(unsafe.Pointer(&mgr))); failed(hr) {
		return fmt.Errorf("ToastNotificationManager %#x", hr)
	}
	defer release(mgr)
	id, _ := hstring(appID)
	defer pWinDeleteString.Call(id)
	var notifier uintptr
	if hr := comCall(mgr, 7, id, uintptr(unsafe.Pointer(&notifier))); failed(hr) { // CreateToastNotifierWithId
		return fmt.Errorf("CreateToastNotifierWithId %#x", hr)
	}
	defer release(notifier)
	if hr := comCall(notifier, 6, toast); failed(hr) { // Show
		return fmt.Errorf("Show %#x", hr)
	}
	return nil
}

// toastsUsable: toasts need the Start menu entry (it carries the AppUserModelID) and the pullwell:// link.
func toastsUsable() bool {
	toastMu.Lock()
	bad := toastBad
	toastMu.Unlock()
	return !bad && startMenuGet() && linkSchemeRegistered()
}

// ---- the pullwell:// link, registered for the current user ----

const schemeKey = `Software\Classes\` // + appScheme

func linkSchemeRegistered() bool {
	v, ok := regString(syscall.HKEY_CURRENT_USER, schemeKey+appScheme+`\shell\open\command`, "")
	exe := exePath()
	return ok && exe != "" && strings.Contains(strings.ToLower(v), strings.ToLower(exe))
}

// registerLinkScheme points pullwell:// at this program (kept up to date at every start, like the Start menu entry).
func registerLinkScheme() error {
	exe := exePath()
	if exe == "" {
		return errors.New("the program's own path is not known")
	}
	base := schemeKey + appScheme
	for _, kv := range [][3]string{
		{base, "", "URL:" + AppName},
		{base, "URL Protocol", ""},
		{base + `\DefaultIcon`, "", `"` + exe + `",0`},
		{base + `\shell\open\command`, "", `"` + exe + `" "%1"`},
	} {
		if err := regSetString(kv[0], kv[1], kv[2]); err != nil {
			return err
		}
	}
	return nil
}

func unregisterLinkScheme() {
	p, _ := syscall.UTF16PtrFromString(schemeKey + appScheme)
	pRegDeleteTree.Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(p)))
}

var (
	pRegCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
	pRegDeleteTree  = advapi32.NewProc("RegDeleteTreeW")
)

func regSetString(path, name, value string) error {
	p, _ := syscall.UTF16PtrFromString(path)
	var h syscall.Handle
	if r, _, _ := pRegCreateKeyEx.Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(p)), 0, 0, 0, syscall.KEY_SET_VALUE, 0, uintptr(unsafe.Pointer(&h)), 0); r != 0 {
		return fmt.Errorf("could not write %s (error %d)", path, r)
	}
	defer syscall.RegCloseKey(h)
	n, _ := syscall.UTF16PtrFromString(name)
	v, _ := syscall.UTF16FromString(value)
	if r, _, _ := pRegSetValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0, syscall.REG_SZ, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2)); r != 0 {
		return fmt.Errorf("could not write %s (error %d)", path, r)
	}
	return nil
}

var _ = os.Getpid
