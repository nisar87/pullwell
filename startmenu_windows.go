package main

// Start menu entry and uninstall. The entry is a shortcut in the person's own Start menu folder (no administrator
// rights), carrying the program's AppUserModelID so Windows search finds the program and notifications show its
// name and icon instead of the bare ID. It is kept pointing at the program's current place on every start.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	clsidShellLink    = mustGUID("{00021401-0000-0000-C000-000000000046}")
	iidShellLinkW     = mustGUID("{000214F9-0000-0000-C000-000000000046}")
	iidPersistFile    = mustGUID("{0000010b-0000-0000-C000-000000000046}")
	iidPropertyStore  = mustGUID("{886d8eeb-8cf2-4446-8d02-cdba1dbdcf99}")
	pkeyAppUserModel  = propKey{mustGUID("{9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3}"), 5}
)

type propKey struct {
	fmt guid
	pid uint32
}

type propVariant struct {
	vt         uint16
	r1, r2, r3 uint16
	p          uintptr
	pad        uintptr
}

func startMenuFile() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, AppName+".lnk")
}

func startMenuGet() bool {
	_, err := os.Stat(startMenuFile())
	return err == nil
}

// startMenuSet writes or removes the shortcut. COM needs a thread of its own, set up for it.
func startMenuSet(on bool) error {
	if !on {
		err := os.Remove(startMenuFile())
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if os.Getenv("APPDATA") == "" {
		return errors.New("the Start menu folder is not known")
	}
	exe := exePath()
	if exe == "" {
		return errors.New("the program's own path is not known")
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		pCoInit.Call(0, 2) // COINIT_APARTMENTTHREADED
		defer pCoUninit.Call()
		done <- writeShortcut(startMenuFile(), exe)
	}()
	return <-done
}

func writeShortcut(lnk, exe string) error {
	var sl uintptr
	if hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, 1 /* CLSCTX_INPROC_SERVER */, uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&sl))); failed(hr) || sl == 0 {
		return errors.New("Windows could not make a shortcut")
	}
	defer comCall(sl, 2)
	str := func(s string) uintptr { p, _ := syscall.UTF16PtrFromString(s); return uintptr(unsafe.Pointer(p)) }
	keep := []string{exe, filepath.Dir(exe), "A download manager", lnk, appID}
	comCall(sl, 20, str(keep[0]))    // SetPath
	comCall(sl, 9, str(keep[1]))     // SetWorkingDirectory
	comCall(sl, 7, str(keep[2]))     // SetDescription
	comCall(sl, 17, str(keep[0]), 0) // SetIconLocation: the program's first icon
	var ps uintptr
	if hr := comCall(sl, 0, uintptr(unsafe.Pointer(&iidPropertyStore)), uintptr(unsafe.Pointer(&ps))); !failed(hr) && ps != 0 {
		id, _ := syscall.UTF16PtrFromString(keep[4])
		pv := propVariant{vt: 31 /* VT_LPWSTR */, p: uintptr(unsafe.Pointer(id))}
		comCall(ps, 6, uintptr(unsafe.Pointer(&pkeyAppUserModel)), uintptr(unsafe.Pointer(&pv))) // SetValue
		comCall(ps, 7)                                                                           // Commit
		comCall(ps, 2)
		runtime.KeepAlive(id)
	}
	var pf uintptr
	if hr := comCall(sl, 0, uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&pf))); failed(hr) || pf == 0 {
		return errors.New("Windows could not save the shortcut")
	}
	defer comCall(pf, 2)
	_ = os.MkdirAll(filepath.Dir(lnk), 0o755)
	if hr := comCall(pf, 6, str(keep[3]), 1); failed(hr) { // Save
		return errors.New("Windows could not save the shortcut")
	}
	runtime.KeepAlive(keep)
	return nil
}

// uninstallLater removes the program file (and the previous version kept for "Go back") a few seconds after the
// program has quit, and the data folder when asked. Downloaded files are never touched.
func uninstallLater(withData bool) error {
	exe := exePath()
	if exe == "" {
		return errors.New("the program's own path is not known")
	}
	cmd := `cmd.exe /c ping -n 4 127.0.0.1 >nul & del /f /q "` + exe + `" "` + previousFile() + `"`
	if withData {
		cmd += ` & rmdir /s /q "` + filepath.Join(appRoot, "data") + `"`
	}
	c := hiddenCmd(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
	c.SysProcAttr.CmdLine = cmd
	return c.Start()
}
