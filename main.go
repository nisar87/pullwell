package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	waitPid := flag.Int("wait-pid", 0, "after an update: wait until this process (the old version) has ended")
	updated := flag.String("updated", "", "after an update: the version that was replaced")
	flag.Parse()
	uiMode, debugMode = *ui, *debug
	if *version {
		fmt.Println(AppName, appVersion)
		return
	}

	dir := *root
	customRoot = dir != ""
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			fail("cannot find the program's own folder: %v", err)
		}
		dir = filepath.Dir(exe)
		// a file named "separate-copy" next to the program makes it a separate copy (a test copy, for example),
		// exactly as if it had been started with -root on its own folder: its own data, and it leaves the Start
		// menu entry, the link registration and Start with Windows of the installed copy alone
		if _, err := os.Stat(filepath.Join(dir, separateMarker)); err == nil {
			customRoot = true
		} else if !writableDir(filepath.Join(dir, "data")) {
			// a protected folder (Program Files, for example): keep the data in the person's own app data folder
			if la := os.Getenv("LOCALAPPDATA"); la != "" {
				dir = filepath.Join(la, appDataDir)
			}
		}
	}
	dir, _ = filepath.Abs(dir)
	appRoot = dir
	logInit(dir)
	initScrub()
	loadUILang()
	logf("%s %s starting; data folder %s", AppName, appVersion, filepath.Join(dir, "data"))

	if *waitPid > 0 { // the old version is still closing: wait for it to release the port and the lock
		waitForExit(*waitPid, 60*time.Second)
	}
	if *updated != "" {
		logf("updated from %s", *updated)
	}
	loadUpdateCfg()
	loadAppSettings()
	go keepStartMenu()

	// One copy per data folder: a second start shows the first one's window and ends.
	// A link on the command line (the program followed by https://...) opens the Add dialog with it, here or in the running copy.
	link := ""
	appLinkArg := ""
	if a := flag.Args(); len(a) > 0 && isWebLink(a[0]) {
		link = strings.TrimSpace(a[0])
	} else if len(a) > 0 && strings.HasPrefix(strings.ToLower(a[0]), appScheme+":") {
		appLinkArg = strings.TrimSpace(a[0]) // a click on one of our notifications
	}
	handTo := link
	if appLinkArg != "" {
		handTo = appLinkArg
	}
	release, first := instanceStart(dir, *tray, handTo)
	if !first {
		logf("already running: handed over to the first copy")
		return
	}
	defer release()
	diagStart(dir)
	defer diagStop()
	if link != "" {
		setPendingAdd(link)
	}

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
	go updateLoop()
	diagReportLoop()
	go keepAwakeLoop()
	if *updated != "" {
		go func() {
			time.Sleep(3 * time.Second)
			if takeWentBack() {
				showBalloon(AppName+" went back", "Now version "+appVersion+" ("+appBuild+") again.")
			} else {
				showBalloon(AppName+" updated", "Now version "+appVersion+" ("+appBuild+").")
			}
		}()
	}

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
	diagStop()
	showError(msg)
	os.Exit(1)
}

// writableDir reports whether a folder exists or can be made, and files can be written in it.
func writableDir(dir string) bool {
	_, statErr := os.Stat(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		if os.IsNotExist(statErr) {
			_ = os.Remove(dir) // made just now for the test: leave nothing behind
		}
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}
