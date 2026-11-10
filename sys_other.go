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
	"syscall"
)

func instanceStart(root string, tray bool) (release func(), first bool) { return func() {}, true }

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

func notify(title, text string) { logf("notification: %s: %s", title, text) }

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
