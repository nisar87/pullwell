package dm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestMgr(t *testing.T, env *simEnv) *dlManager {
	t.Helper()
	oldRoot := H.Root
	H.Root = t.TempDir()
	t.Cleanup(func() { H.Root = oldRoot })
	m := newDlManager(env.Lines)
	m.cfg.Dir = filepath.Join(H.Root, "dl")
	m.startLoop()
	t.Cleanup(func() { m.shutdown() })
	return m
}

func waitState(t *testing.T, m *dlManager, id, want string, d time.Duration) dlView {
	t.Helper()
	end := time.Now().Add(d)
	var last dlView
	for time.Now().Before(end) {
		for _, v := range m.views() {
			if v.ID == id {
				last = v
				if v.State == want {
					return v
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("download never reached %q (state %q, err %q)", want, last.State, last.Err)
	return last
}

func addOne(t *testing.T, m *dlManager, link string) string {
	t.Helper()
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: link, Force: true}})
	if err != nil {
		t.Fatal(err)
	}
	return out["added"].([]*dlItem)[0].ID
}

func TestDLManagerEndToEnd(t *testing.T) {
	size := int64(6*MiB + 99)
	s := newSim(size)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)))
	id := addOne(t, m, s.URL+"/file.bin")
	v := waitState(t, m, id, "done", 30*time.Second)
	if v.Name != "sim file.bin" || v.Size != size || v.Done != size {
		t.Fatalf("%+v", v)
	}
	if filepath.Base(filepath.Dir(v.Path)) != "Other" {
		t.Fatalf("category folder: %s", v.Path)
	}
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
	if len(v.Bytes) == 0 {
		t.Fatal("per-line bytes not kept")
	}
	// same name again does not overwrite
	id2 := addOne(t, m, s.URL+"/file.bin")
	v2 := waitState(t, m, id2, "done", 30*time.Second)
	if v2.Path == v.Path || !strings.Contains(v2.Name, "(2)") {
		t.Fatalf("name clash: %s %s", v.Path, v2.Path)
	}
}

func TestDLManagerPauseResume(t *testing.T) {
	size := int64(10 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "downloading", 5*time.Second)
	time.Sleep(900 * time.Millisecond)
	if _, err := m.handle(dlReq{Action: "pause", ID: id}); err != nil {
		t.Fatal(err)
	}
	v := waitState(t, m, id, "paused", 10*time.Second)
	if v.Done <= 0 || v.Done >= size {
		t.Fatalf("done=%d", v.Done)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := m.handle(dlReq{Action: "resume", ID: id}); err != nil {
		t.Fatal(err)
	}
	v = waitState(t, m, id, "done", 40*time.Second)
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
}

func TestDLManagerSurvivesRestart(t *testing.T) {
	size := int64(10 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	m := newTestMgr(t, env)
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "downloading", 5*time.Second)
	time.Sleep(900 * time.Millisecond)
	m.shutdown() // NetMerge closes
	m2 := newDlManager(env.Lines)
	m2.load()
	got := m2.views()
	if len(got) != 1 || got[0].State != "queued" || got[0].Done <= 0 {
		t.Fatalf("after restart: %+v", got)
	}
	m2.startLoop()
	defer m2.shutdown()
	v := waitState(t, m2, id, "done", 40*time.Second)
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
}

func TestDLManagerRetriesThenWorks(t *testing.T) {
	size := int64(2 * MiB)
	s := newSim(size)
	s.failFirst = 2 // the probe fails on both lines once
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)))
	id := addOne(t, m, s.URL+"/file.bin")
	v := waitState(t, m, id, "done", 40*time.Second)
	if v.Retries < 1 {
		t.Fatalf("expected a retry: %+v", v)
	}
}

func TestDLManagerNotFoundIsAnError(t *testing.T) {
	s := newSim(MiB)
	s.fails404 = true
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	id := addOne(t, m, s.URL+"/gone.zip")
	v := waitState(t, m, id, "error", 15*time.Second)
	if !strings.Contains(v.Err, "not found") {
		t.Fatalf("message: %q", v.Err)
	}
	if _, err := m.handle(dlReq{Action: "remove", ID: id}); err != nil {
		t.Fatal(err)
	}
	if len(m.views()) != 0 {
		t.Fatal("not removed")
	}
}

func TestDLManagerQueueLimit(t *testing.T) {
	size := int64(4 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	m.cfg.MaxActive = 1
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, addOne(t, m, s.URL+"/file.bin?n="+string(rune('a'+i))))
	}
	maxSeen := 0
	end := time.Now().Add(60 * time.Second)
	for time.Now().Before(end) {
		run, done := 0, 0
		for _, v := range m.views() {
			if v.State == "downloading" {
				run++
			}
			if v.State == "done" {
				done++
			}
		}
		if run > maxSeen {
			maxSeen = run
		}
		if done == 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if maxSeen != 1 {
		t.Fatalf("expected exactly 1 at a time, saw %d", maxSeen)
	}
	for _, id := range ids {
		waitState(t, m, id, "done", time.Second)
	}
}

func TestDLManagerKeepsCookiesOutOfTheView(t *testing.T) {
	s := newSim(MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	_, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin", Header: map[string]string{"Cookie": "secret=1", "X-Evil": "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m.snapshot())
	if strings.Contains(string(b), "secret=1") || strings.Contains(string(b), "X-Evil") {
		t.Fatalf("header leaked to the page: %s", b)
	}
	m.save()
	raw, _ := os.ReadFile(dlFile())
	if !strings.Contains(string(raw), "secret=1") || strings.Contains(string(raw), "X-Evil") {
		t.Fatal("cookie must be saved for resuming and unknown headers dropped")
	}
	if fi, _ := os.Stat(dlFile()); fi != nil && fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("downloads.json is readable by others: %v", fi.Mode())
	}
}

func TestDLManagerAddValidation(t *testing.T) {
	m := newTestMgr(t, newSimEnv())
	for _, bad := range []string{"", "gopher://x/y", "javascript:alert(1)", "file:///etc/passwd", "notaurl", "http://"} {
		if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: bad}}); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: "http://x/y", Dir: "relative/dir"}}); err == nil {
		t.Error("accepted a relative folder")
	}
	if _, err := m.handle(dlReq{Action: "config", Config: &dlConfig{Dir: "relative"}}); err == nil {
		t.Error("accepted a relative folder in settings")
	}
	if _, err := m.handle(dlReq{Action: "config", Config: &dlConfig{Dir: filepath.Join(t.TempDir(), "ok"), MaxActive: 99}}); err != nil {
		t.Error(err)
	}
	if m.config().MaxActive != 3 {
		t.Error("out-of-range value should be fixed")
	}
}

func TestDLMobileOnlyWhenAllowed(t *testing.T) {
	a, mob := []dlLine{{Name: "LAN"}}, []dlLine{{Name: "4G"}}
	if got := dlPickLines(a, mob, false); len(got) != 1 || got[0].Name != "LAN" {
		t.Fatal("mobile used without permission")
	}
	if got := dlPickLines(a, mob, true); len(got) != 2 {
		t.Fatal("mobile not used when allowed")
	}
	if got := dlPickLines(nil, mob, false); len(got) != 1 {
		t.Fatal("mobile should be used when it is the only line")
	}
}

func TestDLFriendlyMessages(t *testing.T) {
	cases := map[string]bool{
		`Get "http://x": dial tcp 1.2.3.4:80: connect: connection refused`:                 true,
		`dial tcp: lookup nope.invalid: no such host`:                                      true,
		`tls: failed to verify certificate: x509: certificate signed by unknown authority`: false,
		`context deadline exceeded`:                                                        true,
	}
	for in, retry := range cases {
		msg, r := dlFriendly(errors.New(in))
		if strings.Contains(msg, "dial tcp") || strings.Contains(msg, "x509") || r != retry {
			t.Errorf("%q -> %q retry=%v", in, msg, r)
		}
	}
}
