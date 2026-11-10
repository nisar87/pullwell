//go:build windows

package main

// "Start with Windows": one value in the current user's Run key. No administrator rights, no scheduled task,
// no PowerShell (design D9). The value is this program's path with -tray, so it starts in the notification area.

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	pRegSetValueEx  = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValue = advapi32.NewProc("RegDeleteValueW")
)

func autoStartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `" -tray`, nil
}

// autoStartGet: is this program registered to start with Windows? An entry for another copy of the program (another
// folder) counts as off for this one.
func autoStartGet() (bool, error) {
	cur, ok := regString(syscall.HKEY_CURRENT_USER, runKey, AppName)
	if !ok {
		return false, nil
	}
	want, err := autoStartCommand()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(cur, want), nil
}

func autoStartSet(on bool) error {
	p, _ := syscall.UTF16PtrFromString(runKey)
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, p, 0, syscall.KEY_SET_VALUE|syscall.KEY_QUERY_VALUE, &h); err != nil {
		return err
	}
	defer syscall.RegCloseKey(h)
	name, _ := syscall.UTF16PtrFromString(AppName)
	if !on {
		r, _, _ := pRegDeleteValue.Call(uintptr(h), uintptr(unsafe.Pointer(name)))
		if r != 0 && r != 2 { // 2 = ERROR_FILE_NOT_FOUND: already off
			return syscall.Errno(r)
		}
		return nil
	}
	cmd, err := autoStartCommand()
	if err != nil {
		return err
	}
	v, _ := syscall.UTF16FromString(cmd)
	r, _, _ := pRegSetValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, syscall.REG_SZ, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}
