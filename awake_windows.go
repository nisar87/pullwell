package main

// Keep the PC awake while something downloads: Windows' sleep timer would otherwise put it to sleep in the middle of
// a long download. Only the system is kept awake, not the screen, and only while a download is running. The setting
// lives in Options → Schedule ("Keep the PC awake while downloading", on by default).

import (
	"runtime"
	"time"
)

var pSetExecState = kernel32.NewProc("SetThreadExecutionState")

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// keepAwakeLoop runs for the life of the program. The request belongs to the thread that made it, so the loop keeps
// to one thread.
func keepAwakeLoop() {
	runtime.LockOSThread()
	awake := false
	for {
		want := keepAwakeOn() && downloadsActive() > 0
		if want != awake {
			if want {
				pSetExecState.Call(esContinuous | esSystemRequired)
				logf("keeping the PC awake while downloading")
			} else {
				pSetExecState.Call(esContinuous)
				logf("the PC may sleep again")
			}
			awake = want
		}
		time.Sleep(20 * time.Second)
	}
}
