package dm

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// withOff runs a test with the manager off and an empty product folder, like a fresh install.
func withOff(t *testing.T) string {
	t.Helper()
	oldRoot, oldOn, oldMgr := H.Root, Enabled(), dlMgr
	root := t.TempDir()
	H.Root = root
	enabledFlag.set(false)
	dlMgr = newDlManager(func() []dlLine { return nil })
	t.Cleanup(func() {
		stop()
		H.Root, dlMgr = oldRoot, oldMgr
		enabledFlag.set(oldOn)
	})
	return root
}

func TestOffByDefaultTouchesNothing(t *testing.T) {
	root := withOff(t)
	before := runtime.NumGoroutine()
	LoadSwitch() // no saved choice: stays off
	if Enabled() {
		t.Fatal("must be off by default")
	}
	if Brief() != nil || BackupExport() != nil {
		t.Fatal("an off manager reports nothing")
	}
	mux := http.NewServeMux()
	Routes(mux)
	for _, p := range []string{"/ext/ping", "/ext/pair", "/ext/status", "/ext/add"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", p, strings.NewReader("{}")))
		if rec.Code != 404 {
			t.Fatalf("%s answered %d while off", p, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/downloads", nil))
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("snapshot while off: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/downloads", strings.NewReader(`{"action":"add","urls":["http://example.com/a.bin"]}`)))
	if rec.Code != 409 {
		t.Fatalf("add while off: %d", rec.Code)
	}
	if len(dlMgr.items) != 0 {
		t.Fatal("an item was added while off")
	}
	if es, _ := os.ReadDir(root); len(es) != 0 {
		t.Fatalf("an off manager wrote files: %v", es)
	}
	time.Sleep(50 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("goroutines %d -> %d while off", before, after)
	}
}

func TestSwitchOnOffAndRemembered(t *testing.T) {
	root := withOff(t)
	H.Root = root
	if err := SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("not on")
	}
	if _, err := os.Stat(filepath.Join(root, "data", "downloads-switch.json")); err != nil {
		t.Fatal("choice not saved")
	}
	if Brief() == nil {
		t.Fatal("no brief while on")
	}
	if err := SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if Enabled() || Brief() != nil {
		t.Fatal("still on")
	}
	// a restart with the choice "on" starts it again; "off" does not
	if err := SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	stop() // as if the product quit (the saved choice stays on)
	LoadSwitch()
	if !Enabled() {
		t.Fatal("the saved choice was not honoured")
	}
	// turning it off and on again works repeatedly
	for i := 0; i < 3; i++ {
		_ = SetEnabled(false)
		_ = SetEnabled(true)
	}
	if !Enabled() {
		t.Fatal("flapping broke the switch")
	}
}

func TestPanicsStayInside(t *testing.T) {
	withOff(t)
	logged := make(chan string, 4)
	old := H.Log
	H.Log = func(f string, a ...any) { logged <- f }
	defer func() { H.Log = old }()
	done := make(chan struct{})
	goSafe("test", func() { defer close(done); panic("boom") })
	<-done
	select {
	case <-logged:
	case <-time.After(time.Second):
		t.Fatal("the crash was not logged")
	}
	H.Log = old
	rec := httptest.NewRecorder()
	safeHandler(func(w http.ResponseWriter, r *http.Request) { panic("handler") })(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatalf("handler crash gave %d", rec.Code)
	}
}
