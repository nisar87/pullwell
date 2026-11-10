package dm

import (
	"os"
	"testing"
	"time"
)

// Run with DM_BENCH=1 go test -run TestBench -v ./dm   (a loopback speed check, not part of the normal run)
func TestBenchThroughput(t *testing.T) {
	if os.Getenv("DM_BENCH") == "" {
		t.Skip("set DM_BENCH=1")
	}
	size := int64(512 << 20)
	for round := 0; round < 3; round++ {
		s := newSim(size)
		env := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0), newSimLine("C", "127.0.0.3", 0), newSimLine("D", "127.0.0.4", 0))
		tk, d, err := runDl(t, env, s.URL+"/file.bin", dlSpec{})
		mustOK(t, tk, err, size)
		t.Logf("unthrottled 4 lines, 512 MB: %.2fs = %.0f MB/s", d.Seconds(), float64(size)/d.Seconds()/1e6)
		s.Close()
	}
}

// Four lines of different speed: the finish time shows how well the end of the file is shared.
func TestBenchMixedLines(t *testing.T) {
	if os.Getenv("DM_BENCH") == "" {
		t.Skip("set DM_BENCH=1")
	}
	size := int64(120 << 20)
	var sum time.Duration
	for round := 0; round < 3; round++ {
		s := newSim(size)
		env := newSimEnv(newSimLine("A", "127.0.0.1", 40<<20), newSimLine("B", "127.0.0.2", 20<<20), newSimLine("C", "127.0.0.3", 8<<20), newSimLine("D", "127.0.0.4", 2<<20))
		tk, d, err := runDl(t, env, s.URL+"/file.bin", dlSpec{})
		mustOK(t, tk, err, size)
		ideal := float64(size) / float64(70<<20)
		t.Logf("mixed lines 40+20+8+2 MB/s, 120 MB: %.2fs (ideal %.2fs)", d.Seconds(), ideal)
		sum += d
		s.Close()
	}
}

func TestBenchRamp(t *testing.T) {
	if os.Getenv("DM_BENCH") == "" {
		t.Skip("set DM_BENCH=1")
	}
	for _, c := range []struct {
		name string
		caps []float64
		size int64
	}{{"one 40MB/s line, 40 MB", []float64{40 << 20}, 40 << 20}, {"two 40MB/s lines, 80 MB", []float64{40 << 20, 40 << 20}, 80 << 20}, {"small 8 MB file, one 40MB/s line", []float64{40 << 20}, 8 << 20}} {
		s := newSim(c.size)
		var ls []*simLine
		var tot float64
		for i, cp := range c.caps {
			ls = append(ls, newSimLine(string(rune('A'+i)), "127.0.0."+string(rune('1'+i)), cp))
			tot += cp
		}
		tk, d, err := runDl(t, newSimEnv(ls...), s.URL+"/file.bin", dlSpec{})
		mustOK(t, tk, err, c.size)
		t.Logf("%s: %.2fs (ideal %.2fs) = %.0f%%", c.name, d.Seconds(), float64(c.size)/tot, 100*float64(c.size)/tot/d.Seconds())
		s.Close()
	}
}

// A server that gives each connection only 2 MB/s: more connections are the only way to go faster.
func TestBenchPerConnectionLimit(t *testing.T) {
	if os.Getenv("DM_BENCH") == "" {
		t.Skip("set DM_BENCH=1")
	}
	size := int64(48 << 20)
	s := newSim(size)
	defer s.Close()
	s.set(func() { s.perConn = 2 << 20 })
	tk, d, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 100<<20)), s.URL+"/file.bin", dlSpec{})
	mustOK(t, tk, err, size)
	t.Logf("48 MB, server gives 2 MB/s per connection, 8 connections possible: %.1fs (best possible %.1fs)", d.Seconds(), float64(size)/(16<<20))
}

// Torrent over two lines with different speeds: the peers should spread so that both lines are full.
func TestBenchTorrentTwoLines(t *testing.T) {
	if os.Getenv("DM_BENCH") == "" {
		t.Skip("set DM_BENCH=1")
	}
	tt := makeTorrent("bench.bin", []tFile{{"", 160 << 20}}, 1<<20)
	var addrs []string
	for i := 0; i < 8; i++ {
		s := newSeeder(t, tt)
		s.rate = 4 << 20 // each peer alone gives 4 MB/s
		addrs = append(addrs, s.addr())
	}
	root := t.TempDir() + "/bench.bin"
	env := newSimEnv(newSimLine("A", "127.0.0.1", 20<<20), newSimLine("B", "127.0.0.2", 10<<20))
	tk := newBtTask(btSpec{Meta: tt.meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers(addrs)
	ctx, cancel := ctxTimeout(120 * time.Second)
	defer cancel()
	st := time.Now()
	if err := tk.Run(ctx); err != nil {
		t.Fatal(err)
	}
	d := time.Since(st)
	pr := tk.Progress()
	t.Logf("torrent 160 MB, lines 20+10 MB/s, 8 peers at 4 MB/s each: %.1fs = %.1f MB/s (lines can carry 30; 8 peers can give 32)", d.Seconds(), 160/d.Seconds())
	for _, l := range pr.Lines {
		t.Logf("  line %s carried %d MB", l.Name, l.Bytes>>20)
	}
}
