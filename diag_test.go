package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() { diagSetCrashOutput, diagWatch, diagSend = false, false, false }

func logText(t *testing.T) string {
	b, _ := os.ReadFile(logPath)
	return string(b)
}

func TestScrubKeepsPersonalDataOutOfTheLog(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("USERNAME", "Nisar Ahmad")
	initScrub()
	for _, in := range []string{
		"fetch https://secret.example/a/b.zip?token=abc failed",
		"magnet:?xt=urn:btih:abcdef&dn=Private+Name",
		"mail me at person@example.com please",
		"saved " + filepath.Join(home, "Downloads", "x.zip"),
		"hello Nisar Ahmad",
	} {
		out := scrub(in)
		for _, bad := range []string{"secret.example", "abcdef", "person@", "Private", "token", "Nisar Ahmad"} {
			if strings.Contains(out, bad) {
				t.Errorf("scrub(%q) = %q still has %q", in, out, bad)
			}
		}
		if home != "" && len(home) >= 3 && strings.Contains(strings.ToLower(out), strings.ToLower(home)) {
			t.Errorf("scrub(%q) = %q still has the home folder", in, out)
		}
	}
	if got := scrub("listening on http://127.0.0.1:9197/"); got != "listening on http://127.0.0.1:9197/" {
		t.Errorf("the program's own page must stay readable, got %q", got)
	}
	if got := scrub("WebView2 130.0.2849.80 ready"); got != "WebView2 130.0.2849.80 ready" {
		t.Errorf("version numbers must stay, got %q", got)
	}
}

func TestLastRunAndCrashFile(t *testing.T) {
	root := t.TempDir()
	logInit(root)
	runMarker, crashFile = "", ""
	t.Cleanup(func() { runMarker, crashFile = "", "" })

	// first run: nothing before it
	diagStart(root)
	if _, err := os.Stat(runMarker); err != nil {
		t.Fatal("a running copy leaves its marker")
	}
	diagStop()
	if b, _ := os.ReadFile(runMarker); !strings.HasPrefix(string(b), "ended") {
		t.Fatalf("a clean stop says so in the marker, got %q", b)
	}
	if !strings.Contains(logText(t), "last run: none seen in this folder") {
		t.Fatalf("the very first start is reported as such:\n%s", logText(t))
	}

	// second run starts after a clean stop
	diagStart(root)
	if !strings.Contains(logText(t), "last run: ended normally") {
		t.Fatalf("a clean stop is reported as normal:\n%s", logText(t))
	}
	// third run starts although the second never stopped, and left a crash behind
	if err := os.WriteFile(crashFile, []byte("fatal error: something\n\ngoroutine 1 [running]:\nmain.x()\n\tC:\\Users\\"+os.Getenv("USERNAME")+"\\go\\x.go:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diagStart(root)
	txt := logText(t)
	for _, want := range []string{"last run: it did not end by itself", "last run: a crash was recorded", "crash: fatal error: something"} {
		if !strings.Contains(txt, want) {
			t.Errorf("log lacks %q:\n%s", want, txt)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "data", "crash.1.txt")); err != nil {
		t.Error("the crash is kept as crash.1.txt")
	}
	diagStop()
}

func TestCountsAndScriptErrors(t *testing.T) {
	logInit(t.TempDir())
	logCount("b")
	logCount("a")
	logCount("a")
	flushCounts()
	if !strings.Contains(logText(t), "counts in the last minute: a=2 b=1") {
		t.Fatalf("counts are written sorted:\n%s", logText(t))
	}
	jsErrMu.Lock()
	jsErrSeen = map[string]bool{}
	jsErrMu.Unlock()
	for i := 0; i < 3; i++ {
		logWindowError("x is not defined", "app.js", 12)
	}
	if n := strings.Count(logText(t), "window script error: x is not defined (app.js:12)"); n != 1 {
		t.Fatalf("the same script error is written once, got %d", n)
	}
}
