package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// osVersion is "Windows 10.0.19045" (the build number says 10 or 11: 22000 and up is Windows 11).
func osVersion() string {
	var v struct {
		size                    uint32
		major, minor, build     uint32
		platform                uint32
		csd                     [128]uint16
		spMajor, spMinor, suite uint16
		productType, reserved   uint8
	}
	v.size = uint32(unsafe.Sizeof(v))
	if r, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&v))); r != 0 {
		return "Windows"
	}
	return fmt.Sprintf("Windows %d.%d.%d", v.major, v.minor, v.build)
}
