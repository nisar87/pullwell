package main

// What the program writes to its log so a problem can be understood afterwards (see logfile.go for the file itself):
// a scrubber that keeps links, user names and folders out of every line, a record of how the last run ended, a crash
// file, a count of frequent events, and a watch for sleep, freezes and network changes.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---- scrubbing ------------------------------------------------------------------------------------------------------

var (
	reScheme  = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]{1,12}://[^\s"'<>)\]]*|(?i)\bmagnet:\?[^\s"'<>]*`)
	reEmail   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	scrubMu   sync.Mutex
	scrubHome []*regexp.Regexp
	scrubWith []string
)

// a folder is replaced only where a path begins, not in the middle of another one ("/root" inside "/tmp/x/root/y")
const (
	pathBefore = `(^|[\s"'=(\[:,;])`
	pathAfter  = `($|[\\/\s"')\]:,;])`
)

// initScrub learns what must never reach the log: the person's home folder, user name and computer name.
func initScrub() {
	scrubMu.Lock()
	defer scrubMu.Unlock()
	scrubHome, scrubWith = nil, nil
	add := func(s, with string, isPath bool) {
		if len(s) < 3 {
			return
		}
		for _, v := range []string{s, strings.ReplaceAll(s, `\`, `/`)} {
			re, to := `(?i)`+regexp.QuoteMeta(v), with
			if isPath { // RE2 has no look-ahead: keep the characters around the folder
				re, to = `(?i)`+pathBefore+regexp.QuoteMeta(v)+pathAfter, "${1}"+with+"${2}"
			}
			scrubHome = append(scrubHome, regexp.MustCompile(re))
			scrubWith = append(scrubWith, to)
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		add(h, "~", true)
	}
	add(os.Getenv("USERNAME"), "<user>", false)
	add(os.Getenv("USER"), "<user>", false)
	if h, err := os.Hostname(); err == nil {
		add(h, "<pc>", false)
	}
}

// scrub removes what must not be in a log line: addresses (the program's own page on this computer stays), e-mail
// addresses, the home folder, the user name and the computer name.
func scrub(s string) string {
	s = reScheme.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "http://127.0.0.1") {
			return m
		}
		return "<link>"
	})
	s = reEmail.ReplaceAllString(s, "<email>")
	scrubMu.Lock()
	defer scrubMu.Unlock()
	for i, re := range scrubHome {
		s = re.ReplaceAllString(s, scrubWith[i])
	}
	return s
}

// ---- counts of frequent events -------------------------------------------------------------------------------------

var (
	countMu sync.Mutex
	counts  = map[string]int{}
)

// logCount counts an event that can happen often; the counts are written once a minute (flushCounts).
func logCount(key string) {
	countMu.Lock()
	counts[key]++
	countMu.Unlock()
}

func flushCounts() {
	countMu.Lock()
	if len(counts) == 0 {
		countMu.Unlock()
		return
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, counts[k])
	}
	counts = map[string]int{}
	countMu.Unlock()
	logf("counts in the last minute: %s", strings.Join(parts, " "))
}

// ---- how the last run ended, and the crash file ---------------------------------------------------------------------

var runMarker, crashFile string

// switched off by tests: an open crash file would keep the test folder from being removed on Windows
var diagSetCrashOutput, diagWatch = true, true

// diagStart is called once this copy knows it is the only one running. It reports how the previous run ended, keeps
// a crash from it, and starts the crash file and the watch for this run.
func diagStart(dir string) {
	data := filepath.Join(dir, "data")
	runMarker = filepath.Join(data, "running")
	crashFile = filepath.Join(data, "crash.txt")

	switch b, err := os.ReadFile(runMarker); {
	case err != nil:
		logf("last run: none seen in this folder (first start)")
	case strings.HasPrefix(string(b), "ended"):
		logf("last run: ended normally")
	default:
		logf("last run: it did not end by itself (%s); a freeze, a crash, Windows shutting down, or the process was ended", strings.TrimSpace(string(b)))
		logCount("lastrun.unclean")
	}
	if b, err := os.ReadFile(crashFile); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		logf("last run: a crash was recorded; its first lines follow")
		lines := strings.Split(strings.TrimSpace(scrub(string(b))), "\n")
		for i, l := range lines {
			if i >= 24 {
				logf("crash: … (%d more lines in crash.1.txt)", len(lines)-i)
				break
			}
			logf("crash: %s", strings.TrimRight(l, "\r"))
		}
		_ = os.Rename(crashFile, filepath.Join(data, "crash.1.txt"))
		logCount("lastrun.crash")
	}
	_ = os.WriteFile(runMarker, []byte(fmt.Sprintf("running since %s, version %s", time.Now().Format("2006-01-02 15:04:05"), appVersion)), 0o644)
	if diagSetCrashOutput {
		if f, err := os.Create(crashFile); err == nil {
			// a crash of the Go runtime itself (not a recovered panic) writes its report here
			if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
				logf("crash file: %v", err)
			}
		} else {
			logf("crash file: %v", err)
		}
	}
	logf("session: %s %s (%s), %s %s/%s, %d CPU, Go %s", AppName, appVersion, appBuild, osVersion(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version())
	if diagWatch {
		go diagLoop()
	}
}

// diagStop is called when the program ends by its own choice.
func diagStop() {
	flushCounts()
	if runMarker != "" {
		_ = os.WriteFile(runMarker, []byte("ended "+time.Now().Format("2006-01-02 15:04:05")), 0o644)
	}
	if crashFile != "" {
		if st, err := os.Stat(crashFile); err == nil && st.Size() == 0 {
			_ = os.Remove(crashFile)
		}
	}
}

// ---- the watch: sleep, freezes, the network, memory --------------------------------------------------------------

func upInterfaces() int {
	ifs, err := net.Interfaces()
	if err != nil {
		return -1
	}
	n := 0
	for _, i := range ifs {
		if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 {
			if a, err := i.Addrs(); err == nil && len(a) > 0 {
				n++
			}
		}
	}
	return n
}

func diagLoop() {
	started := time.Now()
	last := time.Now().Round(0) // no monotonic reading: wall time keeps running while the PC sleeps
	nets := upInterfaces()
	logf("network: %d interface(s) up", nets)
	ticks := 0
	for range time.Tick(10 * time.Second) {
		now := time.Now().Round(0)
		if gap := now.Sub(last); gap > 45*time.Second {
			// time runs on while the PC sleeps; the monotonic clock may not, so compare wall time
			logf("wake: the program did not run for %d s (the PC slept, or the program was frozen)", int(gap.Seconds()))
			logCount("wake")
		}
		last = now
		ticks++
		if n := upInterfaces(); n != nets {
			logf("network: %d interface(s) up (was %d)", n, nets)
			logCount("network.change")
			nets = n
		}
		if ticks%6 == 0 {
			flushCounts()
		}
		if ticks%60 == 0 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			logf("stats: up %d min, %d goroutines, heap %d MB, %d GC", int(time.Since(started).Minutes()), runtime.NumGoroutine(), m.HeapAlloc>>20, m.NumGC)
		}
	}
}

// ---- errors in the window's own script ----------------------------------------------------------------------------------

var (
	jsErrMu   sync.Mutex
	jsErrSeen = map[string]bool{}
)

// logWindowError writes a script error from the window (at most 30 different ones per run).
func logWindowError(msg, src string, line int) {
	if len(msg) > 160 {
		msg = msg[:160]
	}
	if len(src) > 40 {
		src = src[:40]
	}
	key := fmt.Sprintf("%s|%s|%d", msg, src, line)
	jsErrMu.Lock()
	dup, full := jsErrSeen[key], len(jsErrSeen) >= 30
	if !dup && !full {
		jsErrSeen[key] = true
	}
	jsErrMu.Unlock()
	logCount("window.script-error")
	if dup || full {
		return
	}
	logf("window script error: %s (%s:%d)", strings.ReplaceAll(msg, "\n", " "), src, line)
}
