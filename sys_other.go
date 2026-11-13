//go:build !windows

package main

// Other systems: for development and tests only. The program runs without a window; open the printed address in
// a browser. The program itself is for Windows.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func instanceStart(root string, tray bool, link string) (release func(), first bool) {
	return func() {}, true
}

func uiHostName() string { return "browser" }

func runUI(url string, showWindow bool) {
	fmt.Printf("%s is running at %s (Ctrl+C to quit)\n", AppName, url)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case <-quitCh:
	}
}

func showError(msg string) { fmt.Fprintln(os.Stderr, msg) }

func showBalloon(title, text string) { logf("notification: %s", title) }

func sysPower(kind string) { logf("power action %q is only done on Windows", kind) }

func sysPowerCancel() error { return nil }

func sysOpenPath(path string, reveal bool) error {
	if reveal {
		path = filepath.Dir(path)
	}
	return exec.Command("xdg-open", path).Start()
}

func sysFreeSpace(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

func sysMarkFromInternet(path, link string) {}

func sysRun(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("no command")
	}
	return exec.CommandContext(ctx, args[0], args[1:]...).Run()
}

func autoStartGet() (bool, error) { return false, errors.New("only on Windows") }
func autoStartSet(on bool) error  { return errors.New("only on Windows") }

func swapAndRestart(newFile string) error { return errors.New("updating itself works on Windows only") }

func waitForExit(pid int, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) && syscall.Kill(pid, 0) == nil {
		time.Sleep(200 * time.Millisecond)
	}
}

func clipWatch(on bool) {}

func startMenuGet() bool                 { return false }
func startMenuSet(on bool) error         { return errors.New("only on Windows") }
func uninstallLater(withData bool) error { return errors.New("only on Windows") }

// readSystemProxy: on other systems, the usual environment variables.
func readSystemProxy() systemProxy {
	s := os.Getenv("HTTPS_PROXY")
	if s == "" {
		s = os.Getenv("https_proxy")
	}
	if s == "" {
		s = os.Getenv("HTTP_PROXY")
	}
	return systemProxy{Enabled: s != "", Server: strings.TrimPrefix(strings.TrimPrefix(s, "http://"), "https://"), Bypass: os.Getenv("NO_PROXY")}
}

func keepAwakeLoop() {}

func showMainWindow() {}

func toastsUsable() bool          { return false }
func showToastXML(x string) error { return errors.New("only on Windows") }
func registerLinkScheme() error   { return nil }
func unregisterLinkScheme()       {}
