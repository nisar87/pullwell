//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	flagDetached         = 0x00000008 // DETACHED_PROCESS
	flagNewGroup         = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	flagBreakawayFromJob = 0x01000000 // CREATE_BREAKAWAY_FROM_JOB: the browser may end its host's whole job
)

// browserHostKeys: where each browser looks for native messaging hosts (per user, no administrator rights).
var browserHostKeys = []struct {
	key     string
	firefox bool
}{
	{`Software\Google\Chrome\NativeMessagingHosts\`, false},
	{`Software\Microsoft\Edge\NativeMessagingHosts\`, false},
	{`Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\`, false},
	{`Software\Chromium\NativeMessagingHosts\`, false},
	{`Software\Mozilla\NativeMessagingHosts\`, true},
}

// registerNativeHost writes the manifests next to the program's data and points every browser at them. It runs at each
// start, so a program that was moved or replaced always registers its own current path.
func registerNativeHost() {
	exe, err := os.Executable()
	if err != nil || customRoot {
		return
	}
	dir := filepath.Join(appRoot, "data")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	cp := filepath.Join(dir, "browser-host-chrome.json")
	fp := filepath.Join(dir, "browser-host-firefox.json")
	for _, o := range readOrigins(cp) {
		learnOrigin(o)
	}
	c, f := hostManifests(exe, originList())
	if os.WriteFile(cp, c, 0o644) != nil || os.WriteFile(fp, f, 0o644) != nil {
		logf("browser starter: could not write its manifests")
		return
	}
	for _, b := range browserHostKeys {
		p := cp
		if b.firefox {
			p = fp
		}
		if err := regSetString(b.key+nativeHostName(), "", p); err != nil {
			logf("browser starter: %v", err)
		}
	}
	logf("browser starter registered (%s)", exe)
}

func readOrigins(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m struct {
		Allowed []string `json:"allowed_origins"`
	}
	_ = json.Unmarshal(b, &m)
	return m.Allowed
}

// onNewOrigin: an extension with an address we have not seen asked something; allow it to start the program too.
func onNewOrigin(o string) {
	if learnOrigin(o) {
		logf("browser starter: allowing the extension %s", o)
		go registerNativeHost()
	}
}

// startProgram starts this program in the notification area, apart from the browser that started the host.
func startProgram() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, flags := range []uint32{flagDetached | flagNewGroup | flagBreakawayFromJob, flagDetached | flagNewGroup} {
		c := exec.Command(exe, "-tray")
		c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		if err = c.Start(); err == nil {
			_ = c.Process.Release()
			return nil
		}
	}
	return err
}
