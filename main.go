package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"pullwell/dm"
)

var (
	quitCh   = make(chan struct{})
	quitOnce sync.Once
)

// requestQuit asks the program to stop (tray menu, or a signal on other systems).
func requestQuit() { quitOnce.Do(func() { close(quitCh) }) }

func main() {
	root := flag.String("root", "", "folder for the data folder (default: the folder of the program)")
	tray := flag.Bool("tray", false, "start in the notification area without opening the window")
	version := flag.Bool("version", false, "print the version and exit")
	ui := flag.String("ui", "auto", "how to show the window: auto, webview2, app (Edge or Chrome) or browser")
	debug := flag.Bool("debug", false, "allow the browser developer tools in the window")
	flag.Parse()
	uiMode, debugMode = *ui, *debug
	if *version {
		fmt.Println(AppName, appVersion)
		return
	}

	dir := *root
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			fail("cannot find the program's own folder: %v", err)
		}
		dir = filepath.Dir(exe)
	}
	dir, _ = filepath.Abs(dir)
	appRoot = dir
	logInit(dir)
	logf("%s %s starting; data folder %s", AppName, appVersion, filepath.Join(dir, "data"))

	// One copy per data folder: a second start shows the first one's window and ends.
	release, first := instanceStart(dir, *tray)
	if !first {
		logf("already running: handed over to the first copy")
		return
	}
	defer release()

	ln, port, err := listenFirstFree(appPorts)
	if err != nil {
		fail("%s cannot start: %v", AppName, err)
	}
	setupEngine(dir, port)
	if err := dm.SetEnabled(true); err != nil {
		logf("could not save the engine switch: %v", err)
	}
	handler, err := routes(port, uiHostName)
	if err != nil {
		fail("%s cannot start: %v", AppName, err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logf("local server stopped: %v", err)
		}
	}()
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	logf("listening on %s", url)

	runUI(url, !*tray) // returns when the person quits

	logf("quitting")
	done := make(chan struct{})
	go func() { dm.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		logf("the engine did not stop within 5 seconds; quitting anyway (unfinished downloads resume at the next start)")
	}
	_ = srv.Close()
}

// fail tells the person why the program cannot run, then ends it.
func fail(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	logf("%s", msg)
	showError(msg)
	os.Exit(1)
}
