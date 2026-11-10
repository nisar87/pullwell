package dm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDLBasicMultiLine(t *testing.T) {
	s := newSim(9*MiB + 12345)
	defer s.Close()
	a, b, c := newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0), newSimLine("C", "127.0.0.3", 0)
	tk, _, err := runDl(t, newSimEnv(a, b, c), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, 9*MiB+12345)
	if filepath.Base(tk.FinalPath()) != "out.bin" {
		t.Fatalf("name %s", tk.FinalPath())
	}
	if _, e := os.Stat(dlStatePath(tk.spec.Path)); e == nil {
		t.Fatal("state file left behind")
	}
	if _, e := os.Stat(dlPartPath(tk.spec.Path)); e == nil {
		t.Fatal("partial file left behind")
	}
}

// Phase 0 question: does spreading one file over lines really help? Lines are capped (A 8 MiB/s, B 3 MiB/s) and the
// server gives each connection only 4 MiB/s, like many real servers do.
func TestDLPhase0SpeedGain(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	size := int64(16 * MiB)
	run := func(name string, spec dlSpec, ls ...*simLine) (time.Duration, *simSrv) {
		s := newSim(size)
		s.perConn = 4 * MiB
		defer s.Close()
		tk, d, err := runDl(t, newSimEnv(ls...), s.URL+"/file.bin", spec)
		mustOK(t, tk, err, size)
		t.Logf("%-34s %5.1fs  %5.1f MiB/s", name, d.Seconds(), float64(size)/MiB/d.Seconds())
		return d, s
	}
	one, _ := run("one connection on line A", dlSpec{Lines: []string{"A"}, MaxPerLine: 1}, newSimLine("A", "127.0.0.1", 8*MiB))
	multi, _ := run("many connections on line A only", dlSpec{Lines: []string{"A"}}, newSimLine("A", "127.0.0.1", 8*MiB))
	merged, s := run("all lines (A + B)", dlSpec{}, newSimLine("A", "127.0.0.1", 8*MiB), newSimLine("B", "127.0.0.2", 3*MiB))
	if merged > one*6/10 {
		t.Fatalf("merging did not help enough: %v vs %v", merged, one)
	}
	if merged > multi {
		t.Logf("note: merged (%v) not faster than multi on A (%v) - ramp-up cost on a short file", merged, multi)
	}
	share := float64(s.bytesFrom("127.0.0.2")) / float64(size)
	t.Logf("line B carried %.0f%% of the file", share*100)
	if share < 0.12 {
		t.Fatalf("slow line B did too little work (%.2f)", share)
	}
}

func TestDLIPLockFallsBackAndIsRemembered(t *testing.T) {
	size := int64(8 * MiB)
	s := newSim(size)
	s.lockIP = true
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0))
	tk, _, err := runDl(t, env, s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
	if s.bytesFrom("127.0.0.2") != 0 {
		t.Fatalf("locked address got data")
	}
	pr := tk.Progress()
	left := false
	for _, l := range pr.Lines {
		if l.Name == "B" && l.Status == "left out" {
			left = true
		}
	}
	if !left {
		t.Fatalf("line B should be left out: %+v", pr.Lines)
	}
	time.Sleep(100 * time.Millisecond)
	if !env.HostLocked(dlHostOf(s.URL)) {
		t.Fatal("host was not remembered")
	}
	// second time: one line from the start, no refused requests to the other address
	s2 := newSim(size)
	s2.lockIP = true
	s2.owner = "127.0.0.1"
	defer s2.Close()
	env.LockHost(dlHostOf(s2.URL))
	tk2, _, err := runDl(t, env, s2.URL+"/file.bin", dlSpec{})
	mustOK(t, tk2, err, size)
	if tk2.Progress().Note == "" {
		t.Fatal("expected a note about one-line mode")
	}
}

func TestDLNoRangeStreams(t *testing.T) {
	size := int64(3 * MiB)
	s := newSim(size)
	s.noRange = true
	defer s.Close()
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
}

func TestDLUnknownLengthStreams(t *testing.T) {
	size := int64(2*MiB + 7)
	s := newSim(size)
	s.noRange = true
	s.chunked = true
	defer s.Close()
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
}

func TestDLConnectionLimit(t *testing.T) {
	size := int64(6 * MiB)
	s := newSim(size)
	s.maxPerIP = 2
	s.perConn = 6 * MiB
	defer s.Close()
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
}

func TestDLDroppedConnectionsRecover(t *testing.T) {
	size := int64(8 * MiB)
	s := newSim(size)
	s.dropEvery = 700 << 10
	defer s.Close()
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
}

func TestDLFileChangedStops(t *testing.T) {
	size := int64(8 * MiB)
	s := newSim(size)
	s.changeAfter = 4
	s.perConn = 3 * MiB
	defer s.Close()
	_, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)), s.URL+"/file.bin", dlSpec{})
	if err == nil || !errors.Is(err, errDlChanged) {
		t.Fatalf("expected 'changed', got %v", err)
	}
}

func TestDLResumeAfterPause(t *testing.T) {
	size := int64(12 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0))
	path := filepath.Join(t.TempDir(), "r.bin")
	tk := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(700 * time.Millisecond); cancel() }()
	if err := tk.Run(ctx); err == nil {
		t.Fatal("expected a pause")
	}
	pr := tk.Progress()
	if pr.Done <= 0 || pr.Done >= size {
		t.Fatalf("pause came at a bad time: %d", pr.Done)
	}
	if _, err := os.Stat(dlStatePath(path)); err != nil {
		t.Fatal("no state file after pause")
	}
	sent1 := s.bytesFrom("127.0.0.1") + s.bytesFrom("127.0.0.2")
	tk2 := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
	if err := tk2.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fileHash(t, tk2.FinalPath()) != simHash(size) {
		t.Fatal("wrong content after resume")
	}
	sent2 := s.bytesFrom("127.0.0.1") + s.bytesFrom("127.0.0.2") - sent1
	if sent2 > size-pr.Done+4*MiB {
		t.Fatalf("resume downloaded too much again: %d of %d (had %d)", sent2, size, pr.Done)
	}
}

func TestDLResumeDiscardsChangedFile(t *testing.T) {
	size := int64(6 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	path := filepath.Join(t.TempDir(), "c.bin")
	tk := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(900 * time.Millisecond); cancel() }()
	_ = tk.Run(ctx)
	s.mu.Lock()
	s.etag = `"other"`
	s.mu.Unlock()
	tk2 := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
	if err := tk2.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fileHash(t, tk2.FinalPath()) != simHash(size) {
		t.Fatal("mixed old and new file")
	}
}

func TestDLLineGoesDown(t *testing.T) {
	size := int64(10 * MiB)
	s := newSim(size)
	s.perConn = 3 * MiB
	defer s.Close()
	a, b := newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)
	go func() { time.Sleep(700 * time.Millisecond); b.alive.Store(false) }()
	tk, _, err := runDl(t, newSimEnv(a, b), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
}

func TestDLOnlyChosenLines(t *testing.T) {
	size := int64(4 * MiB)
	s := newSim(size)
	defer s.Close()
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)), s.URL+"/file.bin", dlSpec{Lines: []string{"A"}})
	mustOK(t, tk, err, size)
	if s.bytesFrom("127.0.0.2") != 0 {
		t.Fatal("unchosen line was used")
	}
}

func TestDLRedirectAndName(t *testing.T) {
	s := newSim(MiB)
	defer s.Close()
	info, err := dlProbe(context.Background(), dlLineClient(newSimLine("A", "127.0.0.1", 0).line(), true), s.URL+"/redir", nil)
	if err != nil || info.Name != "sim file.bin" || !info.Ranges || info.Size != MiB {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestDLHTMLWarning(t *testing.T) {
	s := newSim(MiB)
	s.html = true
	defer s.Close()
	info, err := dlProbe(context.Background(), dlLineClient(newSimLine("A", "127.0.0.1", 0).line(), true), s.URL+"/x.zip", nil)
	if err != nil || info.Warning != "html" {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestDLNotFoundReports(t *testing.T) {
	s := newSim(MiB)
	s.fails404 = true
	defer s.Close()
	_, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)), s.URL+"/x", dlSpec{})
	var he dlHTTPErr
	if !errors.As(err, &he) || he.Code != 404 {
		t.Fatalf("%v", err)
	}
}

func TestDLRefusesLocalAddressUnlessTyped(t *testing.T) {
	s := newSim(MiB)
	defer s.Close()
	c := dlLineClient(newSimLine("A", "127.0.0.1", 0).line(), false)
	req, _ := http.NewRequest("GET", s.URL+"/file.bin", nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("a public-looking client must not reach the local network")
	}
}

func TestDLEmptyFile(t *testing.T) {
	s := newSim(0)
	defer s.Close()
	// a zero-length file: server answers 416 to the probe
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)), s.URL+"/file.bin", dlSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if fi, e := os.Stat(tk.FinalPath()); e != nil || fi.Size() != 0 {
		t.Fatal("expected empty file")
	}
}

func TestDLSafeName(t *testing.T) {
	cases := map[string]string{
		"../../evil.exe": "evil.exe", `a\b\c.txt`: "c.txt", "CON": "_CON", "x:y?.zip": "x_y_.zip", "  ..  ": "download", "": "download",
	}
	for in, want := range cases {
		if got := dlSafeName(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	if n := dlPickName(`attachment; filename*=UTF-8''na%C3%AFve.pdf`, "http://x/y"); n != "naïve.pdf" {
		t.Errorf("disposition: %q", n)
	}
	if n := dlPickName("", "http://x/a%20b.iso?token=1"); n != "a b.iso" {
		t.Errorf("url: %q", n)
	}
}

func TestDLBucketPaces(t *testing.T) {
	var b dlBucket
	b.setRate(1 * MiB)
	st := time.Now()
	for i := 0; i < 20; i++ {
		_ = b.wait(context.Background(), 64<<10)
	}
	if d := time.Since(st); d < 700*time.Millisecond || d > 2*time.Second {
		t.Fatalf("1.25 MiB at 1 MiB/s took %v", d)
	}
}

// A line that simply has not finished its first piece yet (for example right after a resume) must not be taken for refused.
func TestDLSlowStartingLineIsNotLeftOut(t *testing.T) {
	size := int64(12 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0))
	path := filepath.Join(t.TempDir(), "n.bin")
	tk := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(900 * time.Millisecond); cancel() }()
	_ = tk.Run(ctx)
	tk2 := newDlTask(dlSpec{URL: s.URL + "/file.bin", Path: path}, env)
	if err := tk2.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, l := range tk2.Progress().Lines {
		if l.Status == "left out" || l.Bytes == 0 {
			t.Fatalf("line %s was wrongly left out: %+v", l.Name, tk2.Progress().Lines)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if env.HostLocked(dlHostOf(s.URL)) {
		t.Fatal("host wrongly remembered as locked")
	}
}

// A server that only answers requests with a Referer (anti-hotlinking) refuses a pasted link. The download manager
// then asks again with the site's own address as the referer, as IDM does, and the file comes out complete.
// (Found on a real PC with https://speed.cloudflare.com/__down?bytes=157286400.)
func TestDLRefusedWithoutRefererRetriesWithSiteReferer(t *testing.T) {
	s := newSim(3*MiB + 777)
	s.needReferer = true
	defer s.Close()
	a := newSimLine("A", "127.0.0.1", 0)
	tk, _, err := runDl(t, newSimEnv(a), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, 3*MiB+777)
}

// A referer that the person or the browser gave is never replaced.
func TestDLGivenRefererIsKept(t *testing.T) {
	s := newSim(1 * MiB)
	s.needReferer = true
	defer s.Close()
	c := &http.Client{}
	_, h, err := dlProbeReferer(context.Background(), c, s.URL+"/file.bin", map[string]string{"Referer": "http://elsewhere.test/"})
	if err != nil || h["Referer"] != "http://elsewhere.test/" {
		t.Fatalf("err=%v header=%v", err, h)
	}
	// and a server that refuses everything is still reported as refused
	r := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", 403) }))
	defer r.Close()
	if _, _, err := dlProbeReferer(context.Background(), c, r.URL+"/x", nil); err == nil {
		t.Fatal("a server that always refuses must still be refused")
	}
}
