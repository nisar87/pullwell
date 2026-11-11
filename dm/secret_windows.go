//go:build windows

package dm

import (
	"errors"
	"syscall"
	"unsafe"
)

// The password is sealed with the Windows data protection of the current user (DPAPI): the saved file is useless on
// another PC or for another Windows user.

var (
	crypt32          = syscall.NewLazyDLL("crypt32.dll")
	kernel32dm       = syscall.NewLazyDLL("kernel32.dll")
	procProtect      = crypt32.NewProc("CryptProtectData")
	procUnprotect    = crypt32.NewProc("CryptUnprotectData")
	procLocalFreeDPI = kernel32dm.NewProc("LocalFree")
)

type dataBlob struct {
	cb   uint32
	data *byte
}

func dlBlob(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cb: uint32(len(b)), data: &b[0]}
}

func dlRunDPAPI(proc *syscall.LazyProc, in []byte) ([]byte, error) {
	var out dataBlob
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(dlBlob(in))), 0, 0, 0, 0, 1 /* CRYPTPROTECT_UI_FORBIDDEN */, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errors.New("Windows data protection refused")
	}
	defer procLocalFreeDPI.Call(uintptr(unsafe.Pointer(out.data)))
	res := make([]byte, out.cb)
	if out.cb > 0 {
		copy(res, unsafe.Slice(out.data, out.cb))
	}
	return res, nil
}

func dlSeal(b []byte) ([]byte, error)   { return dlRunDPAPI(procProtect, b) }
func dlUnseal(b []byte) ([]byte, error) { return dlRunDPAPI(procUnprotect, b) }
