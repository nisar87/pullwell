package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThinLogKeepsTheNewLinesAndSumsTheCounts(t *testing.T) {
	text := "2026-11-13 10:00:00 old line\n" +
		"2026-11-13 10:05:00 download 1 (file): problem: http-503\n" +
		"2026-11-13 10:05:01 counts in the last minute: api.add=2 note.done=1\n" +
		"2026-11-13 10:06:01 counts in the last minute: api.add=3\n" +
		"2026-11-13 10:07:00 same\n2026-11-13 10:07:01 same\n2026-11-13 10:07:02 same\n" +
		"2026-11-13 10:08:00 diagnostics: report sent (1 bytes)\n" +
		"2026-11-13 10:09:00 crash: goroutine 1\n\tmain.go:3 +0x1\n"
	out, newest, nn := thinLog(text, "2026-11-13 10:00:00", 1, 100000)
	if strings.Contains(out, "old line") || strings.Contains(out, "diagnostics:") {
		t.Fatalf("old or own lines kept:\n%s", out)
	}
	for _, want := range []string{"api.add=5", "note.done=1", "same (×3)", "problem: http-503", "goroutine 1 ⏎"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if newest != "2026-11-13 10:09:00" || nn != 1 {
		t.Fatalf("newest = %q %d", newest, nn)
	}
	// lines at the marked second that were already sent are skipped, later ones at that second are not
	again, _, _ := thinLog("2026-11-13 10:09:00 a\n2026-11-13 10:09:00 b\n2026-11-13 10:09:00 c\n", "2026-11-13 10:09:00", 2, 1000)
	if strings.TrimSpace(again) != "2026-11-13 10:09:00 c" {
		t.Fatalf("same-second lines: %q", again)
	}
	// a small limit drops the oldest lines and says so
	small, _, _ := thinLog(text, "2026-11-13 10:00:00", 1, 200)
	if !strings.Contains(small, "older line(s) left out") || !strings.Contains(small, "goroutine 1") || len(small) > 400 {
		t.Fatalf("limit not applied:\n%s", small)
	}
}

// reportServer stands in for Firestore and keeps what it was sent.
func reportServer(t *testing.T, status int) (*[]map[string]map[string]map[string]string, *int) {
	var got []map[string]map[string]map[string]string
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		b, _ := io.ReadAll(r.Body)
		var v map[string]map[string]map[string]string
		_ = json.Unmarshal(b, &v)
		got = append(got, v)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	old := diagURL
	diagURL = srv.URL
	t.Cleanup(func() { diagURL = old })
	return &got, &n
}

func setupReportTest(t *testing.T) string {
	root := t.TempDir()
	appRoot = root
	logInit(root)
	_ = os.MkdirAll(filepath.Join(root, "data"), 0o755)
	loadAppSettings()
	initScrub()
	t.Setenv("PULLWELL_DIAG_INSTALL", "test-unit")
	return root
}

func TestReportIsSentOnceWithScrubbedLogAndMarked(t *testing.T) {
	root := setupReportTest(t)
	got, n := reportServer(t, 200)
	logf("download 7 (file): started https://example.com/secret/file.zip as me@example.com")
	if err := sendReport(); err != nil {
		t.Fatal(err)
	}
	if *n != 1 {
		t.Fatalf("sent %d reports", *n)
	}
	f := (*got)[0]["fields"]
	for _, k := range []string{"install", "app", "version", "kind", "log", "at", "os", "cpu", "settings"} {
		if _, ok := f[k]; !ok {
			t.Fatalf("field %s missing", k)
		}
	}
	if f["install"]["stringValue"] != "test-unit" || f["app"]["stringValue"] != "pullwell" || f["kind"]["stringValue"] != "log" {
		t.Fatalf("fields: %v", f)
	}
	if lg := f["log"]["stringValue"]; strings.Contains(lg, "example.com") || strings.Contains(lg, "secret") || !strings.Contains(lg, "<link>") {
		t.Fatalf("log not scrubbed: %s", lg)
	}
	// nothing new now: no second report, and the lines are not sent twice
	if err := sendReport(); err != errNothingNew {
		t.Fatalf("second send: %v", err)
	}
	logf("something else happened")
	if err := sendReport(); err != nil {
		t.Fatal(err)
	}
	lg := (*got)[1]["fields"]["log"]["stringValue"]
	if strings.Contains(lg, "started") || !strings.Contains(lg, "something else") {
		t.Fatalf("lines sent twice or lost: %s", lg)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "report.sent")); err != nil {
		t.Fatal("mark not written")
	}
}

func TestReportKeepsTheMarkWhenTheServerRefuses(t *testing.T) {
	setupReportTest(t)
	_, _ = reportServer(t, 403)
	logf("one line")
	if err := sendReport(); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(diagMarkFile()); err == nil {
		t.Fatal("mark written after a refusal")
	}
}

func TestCrashReportCarriesTheCrashAndItIsNotSentAgain(t *testing.T) {
	root := setupReportTest(t)
	got, _ := reportServer(t, 200)
	_ = os.WriteFile(filepath.Join(root, "data", "crash.1.txt"), []byte("fatal error at C:\\Users\\"+os.Getenv("USER")+"\\x"), 0o644)
	logf("a line")
	if err := sendReport(); err != nil {
		t.Fatal(err)
	}
	f := (*got)[0]["fields"]
	if f["kind"]["stringValue"] != "crash" || !strings.Contains(f["crash"]["stringValue"], "fatal error") {
		t.Fatalf("crash report: %v", f)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "crash.1.txt")); err == nil {
		t.Fatal("crash file still there after it was sent")
	}
}

func TestTurningOffKeepsAnUnsentCrashAndStopsSending(t *testing.T) {
	root := setupReportTest(t)
	_ = os.WriteFile(filepath.Join(root, "data", "crash.1.txt"), []byte("boom"), 0o644)
	customRoot = false
	defer func() { customRoot = true }()
	t.Setenv("PULLWELL_DIAG_INSTALL", "")
	if diagOn() {
		t.Fatal("on before the notice was shown")
	}
	if err := diagTurn(true); err != nil || !diagOn() {
		t.Fatalf("turning on: %v", err)
	}
	if err := diagTurn(false); err != nil || diagOn() {
		t.Fatalf("turning off: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "crash.1.txt")); err == nil {
		t.Fatal("unsent crash report left to be sent")
	}
}

func TestATestCopyDoesNotSendWithoutATestInstall(t *testing.T) {
	setupReportTest(t)
	t.Setenv("PULLWELL_DIAG_INSTALL", "")
	customRoot = true
	_ = diagTurn(true)
	if diagOn() {
		t.Fatal("a copy with its own data folder must not send")
	}
	t.Setenv("PULLWELL_DIAG_INSTALL", "test-x")
	if !diagOn() {
		t.Fatal("a named test install should send")
	}
}

func TestInstallIDIsRandomAndKept(t *testing.T) {
	setupReportTest(t)
	t.Setenv("PULLWELL_DIAG_INSTALL", "")
	a := diagInstallID()
	if len(a) != 24 || a != diagInstallID() {
		t.Fatalf("id %q", a)
	}
	loadAppSettings()
	if diagInstallID() != a {
		t.Fatal("id not kept in the settings file")
	}
}
