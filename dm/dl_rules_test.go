package dm

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func setCfg(t *testing.T, m *dlManager, f func(c *dlConfig)) {
	t.Helper()
	m.mu.Lock()
	c := m.cfg
	f(&c)
	m.cfg = dlFixConfig(c)
	m.mu.Unlock()
}

func TestDLCallAvoidKeepsCallLineFree(t *testing.T) {
	size := int64(8 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)))
	var call atomic.Bool
	m.callInfo = func() (bool, string) { return call.Load(), "A" }
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "downloading", 5*time.Second)
	time.Sleep(900 * time.Millisecond)
	call.Store(true)
	time.Sleep(1500 * time.Millisecond) // workers on A finish their request and leave
	before := s.bytesFrom("127.0.0.1")
	time.Sleep(1500 * time.Millisecond)
	if grew := s.bytesFrom("127.0.0.1") - before; grew > 600<<10 {
		t.Fatalf("the call's line still carried %d bytes during the call", grew)
	}
	call.Store(false)
	v := waitState(t, m, id, "done", 60*time.Second)
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
}

func TestDLCallPauseAndResume(t *testing.T) {
	size := int64(6 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	setCfg(t, m, func(c *dlConfig) { c.CallMode = "pause" })
	var call atomic.Bool
	m.callInfo = func() (bool, string) { return call.Load(), "A" }
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "downloading", 5*time.Second)
	call.Store(true)
	v := waitState(t, m, id, "queued", 10*time.Second)
	if !strings.Contains(v.Note, "call") {
		t.Fatalf("note %q", v.Note)
	}
	time.Sleep(1200 * time.Millisecond)
	if st := m.views()[0].State; st != "queued" {
		t.Fatalf("started during a call: %s", st)
	}
	call.Store(false)
	v = waitState(t, m, id, "done", 60*time.Second)
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
}

func TestDLDownloadHoursWindow(t *testing.T) {
	size := int64(2 * MiB)
	s := newSim(size)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	now := time.Now()
	setCfg(t, m, func(c *dlConfig) {
		c.Window = dlWindow{Enabled: true, From: now.Add(2 * time.Hour).Format("15:04"), To: now.Add(3 * time.Hour).Format("15:04")}
	})
	id := addOne(t, m, s.URL+"/file.bin")
	time.Sleep(1500 * time.Millisecond)
	v := m.views()[0]
	if v.State != "queued" || !strings.Contains(v.Note, "download hours") {
		t.Fatalf("%+v", v)
	}
	setCfg(t, m, func(c *dlConfig) { c.Window.Enabled = false })
	waitState(t, m, id, "done", 30*time.Second)
}

func TestDLSpeedLimits(t *testing.T) {
	size := int64(4 * MiB)
	for name, set := range map[string]func(m *dlManager, id string){
		"all":      nil,
		"line":     nil,
		"download": nil,
	} {
		_ = set
		s := newSim(size)
		m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
		switch name {
		case "all":
			setCfg(t, m, func(c *dlConfig) { c.LimitAll = 1 * MiBf })
		case "line":
			setCfg(t, m, func(c *dlConfig) { c.LimitLines = map[string]float64{"A": 1 * MiBf} })
		}
		st := time.Now()
		out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin", Limit: map[bool]float64{true: 1 * MiBf}[name == "download"]}})
		if err != nil {
			t.Fatal(err)
		}
		id := out["added"].([]*dlItem)[0].ID
		waitState(t, m, id, "done", 40*time.Second)
		if d := time.Since(st); d < 3*time.Second || d > 12*time.Second {
			t.Errorf("limit %s: 4 MiB at 1 MiB/s took %v", name, d)
		}
		s.Close()
	}
}

func TestDLLimitCanBeChangedWhileRunning(t *testing.T) {
	size := int64(24 * MiB)
	s := newSim(size)
	s.perConn = 6 * MiB
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "downloading", 5*time.Second)
	time.Sleep(500 * time.Millisecond)
	if _, err := m.handle(dlReq{Action: "limit", ID: id, LimitBytes: 0.5 * MiBf}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	d0 := m.views()[0].Done
	time.Sleep(2 * time.Second)
	d1 := m.views()[0].Done
	if rate := float64(d1-d0) / 2; rate > 1.2*MiBf {
		t.Fatalf("limit not applied while running: %.1f MiB/s", rate/MiBf)
	}
}

func TestDLExcludedLineIsNeverUsed(t *testing.T) {
	size := int64(5 * MiB)
	s := newSim(size)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)))
	setCfg(t, m, func(c *dlConfig) { c.ExcludeLines = []string{"B"} })
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "done", 30*time.Second)
	if s.bytesFrom("127.0.0.2") != 0 {
		t.Fatal("excluded line was used")
	}
}

func TestDLPresetPausesAndSlows(t *testing.T) {
	size := int64(3 * MiB)
	s := newSim(size)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	var what atomic.Value
	what.Store("pause")
	m.presetInfo = func() (string, string) { return "Gaming", what.Load().(string) }
	id := addOne(t, m, s.URL+"/file.bin")
	time.Sleep(1500 * time.Millisecond)
	v := m.views()[0]
	if v.State != "queued" || !strings.Contains(v.Note, "Gaming") {
		t.Fatalf("%+v", v)
	}
	what.Store("slow") // slow = about 1.5 MiB/s with no known capacity
	st := time.Now()
	waitState(t, m, id, "done", 30*time.Second)
	if d := time.Since(st); d < 1200*time.Millisecond {
		t.Fatalf("slow preset did not slow the download: %v", d)
	}
}

func TestDLChecksum(t *testing.T) {
	size := int64(1*MiB + 5)
	s := newSim(size)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	good := simHash(size)
	for _, c := range []struct{ hash, want string }{{"sha256:" + good, "ok"}, {strings.ToUpper(good), "ok"}, {fmt.Sprintf("%x", sha256.Sum256([]byte("x"))), "bad"}} {
		out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin?h=" + c.want + c.hash[:4], Hash: c.hash}})
		if err != nil {
			t.Fatal(err)
		}
		id := out["added"].([]*dlItem)[0].ID
		waitState(t, m, id, "done", 20*time.Second)
		end := time.Now().Add(5 * time.Second)
		var got string
		for time.Now().Before(end) {
			for _, v := range m.views() {
				if v.ID == id {
					got = v.Verify
				}
			}
			if got == "ok" || got == "bad" {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if got != c.want {
			t.Errorf("hash %q: verify=%q want %q", c.hash[:8], got, c.want)
		}
	}
	for _, bad := range []string{"xyz", "abcd", strings.Repeat("g", 64)} {
		if _, err := dlCleanHash(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestDLDuplicatesStartAtAndMove(t *testing.T) {
	s := newSim(MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	m.cfg.MaxActive = 1
	later := time.Now().Add(1 * time.Hour).Unix()
	out, _ := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/a.bin", StartAt: later}})
	idA := out["added"].([]*dlItem)[0].ID
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/a.bin"}})
	if err != nil || out["duplicate"] != true {
		t.Fatalf("duplicate not noticed: %v %v", out, err)
	}
	out, err = m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/a.bin", Force: true, Start: func() *bool { b := false; return &b }()}})
	if err != nil || out["duplicate"] == true {
		t.Fatalf("force failed: %v %v", out, err)
	}
	time.Sleep(1200 * time.Millisecond)
	for _, v := range m.views() {
		if v.ID == idA && (v.State != "queued" || !strings.Contains(v.Note, "Starts at")) {
			t.Fatalf("scheduled item: %+v", v)
		}
	}
	// reorder
	ids := func() []string {
		var r []string
		for _, v := range m.views() {
			r = append(r, v.ID)
		}
		return r
	}
	first := ids()
	if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/c.bin", Force: true, Start: func() *bool { b := false; return &b }()}}); err != nil {
		t.Fatal(err)
	}
	all := ids()
	last := all[len(all)-1]
	if _, err := m.handle(dlReq{Action: "move", ID: last, Move: "top"}); err != nil {
		t.Fatal(err)
	}
	if ids()[0] != last {
		t.Fatalf("not moved to top: %v", ids())
	}
	_, _ = m.handle(dlReq{Action: "move", ID: last, Move: "down"})
	if ids()[1] != last || len(first) != 2 {
		t.Fatalf("move down: %v", ids())
	}
}

func TestDLAfterAllPowerAction(t *testing.T) {
	s := newSim(MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	got := make(chan string, 2)
	m.power = func(k string) { got <- k }
	setCfg(t, m, func(c *dlConfig) { c.AfterAll = "sleep" })
	time.Sleep(1200 * time.Millisecond)
	select {
	case k := <-got:
		t.Fatalf("fired with nothing downloaded: %s", k)
	default:
	}
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "done", 20*time.Second)
	select {
	case k := <-got:
		if k != "sleep" {
			t.Fatal(k)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no power action after the downloads finished")
	}
	if m.config().AfterAll != "" {
		t.Fatal("the action must switch itself off after firing")
	}
}

func TestDLConfigSanitised(t *testing.T) {
	c := dlFixConfig(dlConfig{Dir: "/x", MaxActive: 3, CallMode: "weird", AfterAll: "format c:", LimitAll: -5,
		LimitLines: map[string]float64{"A": 5, "": 3, "B": -1}, Window: dlWindow{Enabled: true, From: "25:99", To: "07:00"}})
	if c.CallMode != "avoid" || c.AfterAll != "" || c.LimitAll != 0 || len(c.LimitLines) != 1 || c.Window.Enabled {
		t.Fatalf("%+v", c)
	}
	_ = os.Getpid
}

func TestDLBackupKeepsDownloadSettings(t *testing.T) {
	old := dlMgr
	defer func() { dlMgr = old }()
	dlMgr = newTestMgr(t, newSimEnv())
	dir := t.TempDir()
	restore := func(c dlConfig) { b, _ := json.Marshal(c); BackupImport(b) }
	restore(dlConfig{Dir: dir, MaxActive: 5, CallMode: "pause", LimitAll: 2 * MiBf, ExcludeLines: []string{"4G"}})
	c := dlMgr.config()
	if c.Dir != dir || c.MaxActive != 5 || c.CallMode != "pause" || c.LimitAll != 2*MiBf || len(c.ExcludeLines) != 1 {
		t.Fatalf("%+v", c)
	}
	restore(dlConfig{Dir: "/proc/definitely/not/writable/here", MaxActive: 2})
	if c := dlMgr.config(); c.Dir != dir || c.MaxActive != 2 {
		t.Fatalf("bad folder must keep the old one: %+v", c)
	}
}
