//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

var (
	pOpenProcess = kernel32.NewProc("OpenProcess")
	pWaitSingle  = kernel32.NewProc("WaitForSingleObject")
)

// swapAndRestart puts newFile in place of the running program and starts it: the running file is renamed to
// <Name>-previous.exe (Windows allows renaming a running program), newFile takes its name, the new program starts
// with -wait-pid so it waits until this one has ended, and this one quits. Every step is undone if a later one fails.
func swapAndRestart(newFile string) error {
	exe, prev := exePath(), previousFile()
	if exe == "" {
		return fmt.Errorf("cannot find the running program")
	}
	if err := os.Remove(prev); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove the old previous version: %v", err)
	}
	if err := os.Rename(exe, prev); err != nil {
		return fmt.Errorf("could not move the running program aside: %v", err)
	}
	if err := os.Rename(newFile, exe); err != nil {
		_ = os.Rename(prev, exe)
		return fmt.Errorf("could not put the new version in place: %v", err)
	}
	cmd := exec.Command(exe, restartArgs(os.Args[1:], os.Getpid(), windowShown())...)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200} // CREATE_NEW_PROCESS_GROUP
	if err := cmd.Start(); err != nil {
		_ = os.Rename(exe, newFile)
		_ = os.Rename(prev, exe)
		return fmt.Errorf("could not start the new version: %v", err)
	}
	logf("update: started the new version (process %d); quitting", cmd.Process.Pid)
	go func() { time.Sleep(300 * time.Millisecond); requestQuit() }()
	return nil
}

// waitForExit waits until the process pid has ended (at most limit).
func waitForExit(pid int, limit time.Duration) {
	h, _, _ := pOpenProcess.Call(0x00100000 /* SYNCHRONIZE */, 0, uintptr(pid))
	if h == 0 {
		return // already gone
	}
	defer pCloseHandle.Call(h)
	pWaitSingle.Call(h, uintptr(limit.Milliseconds()))
}

// windowShown: is the program's own window open? (An Edge/Chrome window reconnects by itself after the restart.)
func windowShown() bool {
	if mw.hwnd == 0 {
		return false
	}
	v, _, _ := pIsWindowVis.Call(mw.hwnd)
	return v != 0
}
