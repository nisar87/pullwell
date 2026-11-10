package dm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDLPieceLayoutHugeFiles(t *testing.T) {
	for _, size := range []int64{1, 1 << 20, 100 << 20, 5 << 30, 40 << 30, 1 << 40, 16 << 40} {
		ps := dlPieceSize(size)
		n := (size + ps - 1) / ps
		if ps%65536 != 0 || ps < dlMinPiece || n < 1 || n > 4200 {
			t.Errorf("size %d: piece %d, %d pieces", size, ps, n)
		}
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}

// A crash can happen at any moment. Whatever the state file and the partial file look like then (the state file is
// never newer than the data), the next start must finish the file correctly.
func TestDLCrashAtRandomMoments(t *testing.T) {
	size := int64(20 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 2*MiB), newSimLine("B", "127.0.0.2", 2*MiB))
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 6; i++ {
		dir := t.TempDir()
		path := filepath.Join(dir, "c.bin")
		tk := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = tk.Run(ctx); close(done) }()
		time.Sleep(time.Duration(2300+rng.Intn(1500)) * time.Millisecond)
		crash := t.TempDir()
		cpath := filepath.Join(crash, "c.bin")
		if _, err := os.Stat(dlStatePath(path)); err == nil {
			copyFile(t, dlStatePath(path), dlStatePath(cpath)) // state first, then the data: the data is never older
		}
		copyFile(t, dlPartPath(path), dlPartPath(cpath))
		cancel()
		<-done
		tk2 := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: cpath}, env)
		if err := tk2.Run(context.Background()); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if fileHash(t, tk2.FinalPath()) != simHash(size) {
			t.Fatalf("round %d: file is corrupt after a crash", i)
		}
	}
}

func TestDLDamagedStateFilesStartOver(t *testing.T) {
	size := int64(3 * MiB)
	s := newSim(size)
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	cases := map[string]func(path string){
		"garbage state": func(p string) {
			os.WriteFile(dlStatePath(p), []byte("{not json"), 0o644)
			os.WriteFile(dlPartPath(p), make([]byte, size), 0o644)
		},
		"empty state": func(p string) {
			os.WriteFile(dlStatePath(p), nil, 0o644)
			os.WriteFile(dlPartPath(p), make([]byte, size), 0o644)
		},
		"state without data": func(p string) {
			os.WriteFile(dlStatePath(p), []byte(`{"v":1,"size":3145728,"pieceSize":524288,"have":[524288]}`), 0o644)
		},
		"wrong size": func(p string) {
			os.WriteFile(dlStatePath(p), []byte(`{"v":1,"size":99,"pieceSize":524288,"have":[1]}`), 0o644)
			os.WriteFile(dlPartPath(p), make([]byte, 99), 0o644)
		},
		"wrong piece count": func(p string) {
			os.WriteFile(dlStatePath(p), []byte(fmt.Sprintf(`{"v":1,"size":%d,"pieceSize":524288,"have":[1,2]}`, size)), 0o644)
			os.WriteFile(dlPartPath(p), make([]byte, size), 0o644)
		},
		"negative have": func(p string) {
			os.WriteFile(dlStatePath(p), []byte(fmt.Sprintf(`{"v":1,"size":%d,"pieceSize":524288,"have":[-5,-1,0,0,0,0]}`, size)), 0o644)
			os.WriteFile(dlPartPath(p), make([]byte, size), 0o644)
		},
		"huge piece size": func(p string) {
			os.WriteFile(dlStatePath(p), []byte(fmt.Sprintf(`{"v":1,"size":%d,"pieceSize":9223372036854775807,"have":[1]}`, size)), 0o644)
			os.WriteFile(dlPartPath(p), make([]byte, size), 0o644)
		},
		"tiny piece size": func(p string) {
			os.WriteFile(dlStatePath(p), []byte(fmt.Sprintf(`{"v":1,"size":%d,"pieceSize":1,"have":[1]}`, size)), 0o644)
			os.WriteFile(dlPartPath(p), make([]byte, size), 0o644)
		},
	}
	for name, mk := range cases {
		path := filepath.Join(t.TempDir(), "d.bin")
		mk(path)
		tk := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := tk.Run(ctx)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if fileHash(t, tk.FinalPath()) != simHash(size) {
			t.Errorf("%s: wrong content", name)
		}
	}
}

func TestDLAllLinesDownGivesUpInsteadOfHanging(t *testing.T) {
	old := dlNoLineGrace
	dlNoLineGrace = 1500 * time.Millisecond
	defer func() { dlNoLineGrace = old }()
	size := int64(12 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	a, b := newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)
	go func() { time.Sleep(600 * time.Millisecond); a.alive.Store(false); b.alive.Store(false) }()
	st := time.Now()
	_, _, err := runDl(t, newSimEnv(a, b), s.URL+"/file.bin", dlSpec{})
	if err == nil || !strings.Contains(err.Error(), "no connection") {
		t.Fatalf("expected a 'no connection' error, got %v", err)
	}
	if time.Since(st) > 10*time.Second {
		t.Fatal("took too long to give up")
	}
}

func TestDLNotEnoughDiskSpace(t *testing.T) {
	old := dlFreeSpace
	dlFreeSpace = func(string) (int64, error) { return 5 << 20, nil }
	defer func() { dlFreeSpace = old }()
	s := newSim(50 * MiB)
	defer s.Close()
	_, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)), s.URL+"/file.bin", dlSpec{})
	if err == nil {
		t.Fatal("must refuse to start")
	}
	msg, retry := dlFriendly(err)
	if retry || !strings.Contains(msg, "disk is full") {
		t.Fatalf("%q retry=%v", msg, retry)
	}
}

func TestDLManyItemsStayFast(t *testing.T) {
	m := newTestMgr(t, newSimEnv())
	no := false
	var urls []string
	for i := 0; i < 5000; i++ {
		urls = append(urls, fmt.Sprintf("http://example.invalid/f%d.zip", i))
	}
	out, err := m.handle(dlReq{Action: "add", URLs: urls[:500], dlAdd: dlAdd{Start: &no}})
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	for i := 500; i < 5000; i += 500 {
		if _, err := m.handle(dlReq{Action: "add", URLs: urls[i : i+500], dlAdd: dlAdd{Start: &no}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: "http://example.invalid/one-too-many"}}); err == nil {
		t.Fatal("list should be capped")
	}
	// the best of ten runs: a busy machine (other tests, a fuzzer) slows single runs but not the best one
	best := time.Hour
	for i := 0; i < 10; i++ {
		st := time.Now()
		_ = m.snapshot()
		if d := time.Since(st); d < best {
			best = d
		}
	}
	if best > 150*time.Millisecond {
		t.Fatalf("snapshot of 5000 items takes %v", best)
	}
	st := time.Now()
	st = time.Now()
	m.save()
	if d := time.Since(st); d > 500*time.Millisecond {
		t.Fatalf("saving 5000 items takes %v", d)
	}
}

func TestDLWebLinksCannotReachTheLocalNetwork(t *testing.T) {
	s := newSim(MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin", FromWeb: true}})
	if err != nil {
		t.Fatal(err)
	}
	id := out["added"].([]*dlItem)[0].ID
	v := waitState(t, m, id, "error", 15*time.Second)
	if !strings.Contains(v.Err, "inside your own network") {
		t.Fatalf("message: %q", v.Err)
	}
	if s.bytesFrom("127.0.0.1") != 0 {
		t.Fatal("data was fetched from the local network")
	}
}

func TestDLErrorTextHidesLinkSecrets(t *testing.T) {
	got := dlFmtErr(errors.New(`Get "https://cdn.example.com/f.zip?token=SECRET123&sig=abc": dial tcp: lookup failed`))
	if strings.Contains(got, "SECRET123") || strings.Contains(got, "sig=") || !strings.Contains(got, "cdn.example.com/f.zip") {
		t.Fatal(got)
	}
}

func TestDLDirValidation(t *testing.T) {
	for _, bad := range []string{"", "rel/dir", "../x", "C:rel", "\x00/x"} {
		if _, err := dlCleanDir(bad); err == nil && filepath.IsAbs(bad) == false {
			t.Errorf("accepted %q", bad)
		}
	}
	if d, err := dlCleanDir(t.TempDir() + "/a/../b"); err != nil || strings.Contains(d, "..") {
		t.Errorf("not cleaned: %q %v", d, err)
	}
}

func TestDLCookiesDroppedAfterFinish(t *testing.T) {
	s := newSim(MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, _ := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin", Header: map[string]string{"Cookie": "sid=1"}}})
	id := out["added"].([]*dlItem)[0].ID
	waitState(t, m, id, "done", 20*time.Second)
	m.save()
	raw, _ := os.ReadFile(dlFile())
	if strings.Contains(string(raw), "sid=1") {
		t.Fatal("cookie kept after the download finished")
	}
}

// Everything at once: lines going down and up, a server that drops connections and limits them, downloads being
// paused and resumed. Every file must still come out right and nothing may be left running.
func TestDLChaosSoak(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	size := int64(10 * MiB)
	s := newSim(size)
	s.perConn = 2 * MiB
	defer s.Close()
	a, b := newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)
	m := newTestMgr(t, newSimEnv(a, b))
	m.cfg.MaxActive = 3
	g0 := runtime.NumGoroutine()
	var ids []string
	for i := 0; i < 6; i++ {
		ids = append(ids, addOne(t, m, fmt.Sprintf("%s/file.bin?n=%d", s.URL, i)))
	}
	var stop atomic.Bool
	go func() {
		rng := rand.New(rand.NewSource(3))
		for !stop.Load() {
			time.Sleep(time.Duration(300+rng.Intn(700)) * time.Millisecond)
			switch rng.Intn(6) {
			case 0:
				a.alive.Store(rng.Intn(2) == 0)
			case 1:
				b.alive.Store(rng.Intn(2) == 0)
			case 2:
				s.set(func() { s.dropEvery = int64(rng.Intn(2)) * 800 << 10 })
			case 3:
				s.set(func() { s.maxPerIP = rng.Intn(4) })
			case 4:
				_ = m.pause(ids[rng.Intn(len(ids))])
			case 5:
				_ = m.resume(ids[rng.Intn(len(ids))])
			}
			if !a.alive.Load() && !b.alive.Load() { // never leave everything down for long
				a.alive.Store(true)
			}
		}
	}()
	time.Sleep(14 * time.Second)
	stop.Store(true)
	a.alive.Store(true)
	b.alive.Store(true)
	s.set(func() { s.dropEvery, s.maxPerIP = 0, 0 })
	for _, id := range ids {
		_ = m.resume(id)
	}
	for _, id := range ids {
		v := waitState(t, m, id, "done", 120*time.Second)
		if fileHash(t, v.Path) != simHash(size) {
			t.Fatalf("%s is corrupt after the chaos", id)
		}
	}
	time.Sleep(2 * time.Second)
	if g := runtime.NumGoroutine(); g > g0+30 {
		t.Fatalf("goroutines left behind: %d -> %d", g0, g)
	}
}
