package dm

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type hlsOpt struct {
	n         int
	enc       bool
	explicit  bool // explicit IV
	fmp4      bool
	byteRange bool
	master    bool
	live      bool
	drm       bool
	sepAudio  bool
	seqStart  int
	failSeg   map[int]int
	forbidIP  map[string]bool
	token     bool
}

type hlsTestSrv struct {
	*httptest.Server
	o     hlsOpt
	segs  [][]byte
	init  []byte
	key   []byte
	mu    sync.Mutex
	hits  map[string]int
	ipB   map[string]int64
	fails map[int]int
}

func newHLSSrv(t *testing.T, o hlsOpt) *hlsTestSrv {
	if o.n == 0 {
		o.n = 24
	}
	s := &hlsTestSrv{o: o, hits: map[string]int{}, ipB: map[string]int64{}, fails: map[int]int{}}
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < o.n; i++ {
		b := make([]byte, 100000+rng.Intn(300000))
		rng.Read(b)
		s.segs = append(s.segs, b)
	}
	s.init = make([]byte, 777)
	rng.Read(s.init)
	s.key = make([]byte, 16)
	rng.Read(s.key)
	for k, v := range o.failSeg {
		s.fails[k] = v
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *hlsTestSrv) enc(i int, plain []byte) []byte {
	iv := make([]byte, 16)
	if s.o.explicit {
		copy(iv, []byte(fmt.Sprintf("explicit-iv-%04d", 0)))
	} else {
		seq := s.o.seqStart + i
		for k := 0; k < 8; k++ {
			iv[15-k] = byte(uint64(seq) >> (8 * uint(k)))
		}
	}
	pad := 16 - len(plain)%16
	p := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	blk, _ := aes.NewCipher(s.key)
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(blk, iv).CryptBlocks(out, p)
	return out
}

func (s *hlsTestSrv) expected() []byte {
	var out []byte
	if s.o.fmp4 {
		out = append(out, s.init...)
	}
	for _, g := range s.segs {
		out = append(out, g...)
	}
	return out
}

func (s *hlsTestSrv) media() string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n")
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", s.o.seqStart)
	if s.o.fmp4 {
		b.WriteString("#EXT-X-MAP:URI=\"init.mp4\"\n")
	}
	if s.o.drm {
		b.WriteString("#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://x\",KEYFORMAT=\"com.apple.streamingkeydelivery\"\n")
	} else if s.o.enc {
		iv := ""
		if s.o.explicit {
			iv = fmt.Sprintf(",IV=0x%x", []byte(fmt.Sprintf("explicit-iv-%04d", 0)))
		}
		fmt.Fprintf(&b, "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"%s\n", iv)
	}
	var off int64
	for i, g := range s.segs {
		b.WriteString("#EXTINF:2.000,\n")
		ext := "ts"
		if s.o.fmp4 {
			ext = "m4s"
		}
		tok := ""
		if s.o.token {
			tok = "?tok=abc"
		}
		if s.o.byteRange {
			n := int64(len(g))
			fmt.Fprintf(&b, "#EXT-X-BYTERANGE:%d@%d\nall.bin\n", n, off)
			off += n
			continue
		}
		fmt.Fprintf(&b, "seg%d.%s%s\n", i, ext, tok)
	}
	if !s.o.live {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	return b.String()
}

func (s *hlsTestSrv) bytesOn(ip string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ipB[ip]
}

func (s *hlsTestSrv) put(w http.ResponseWriter, r *http.Request, ip string, b []byte) {
	if rg := r.Header.Get("Range"); strings.HasPrefix(rg, "bytes=") {
		var a, z int64
		if n, _ := fmt.Sscanf(rg, "bytes=%d-%d", &a, &z); n == 2 && a <= z && z < int64(len(b)) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, z, len(b)))
			w.Header().Set("Content-Length", fmt.Sprint(z-a+1))
			w.WriteHeader(206)
			w.Write(b[a : z+1])
			s.mu.Lock()
			s.ipB[ip] += z - a + 1
			s.mu.Unlock()
			return
		}
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	w.Write(b)
	s.mu.Lock()
	s.ipB[ip] += int64(len(b))
	s.mu.Unlock()
}

func (s *hlsTestSrv) serve(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	p := r.URL.Path
	s.mu.Lock()
	s.hits[p]++
	s.mu.Unlock()
	switch {
	case p == "/v/master.m3u8":
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		extra := ""
		if s.o.sepAudio {
			extra = "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aud\",NAME=\"en\",DEFAULT=YES,URI=\"audio.m3u8\"\n"
		}
		aud := ""
		if s.o.sepAudio {
			aud = ",AUDIO=\"aud\""
		}
		fmt.Fprintf(w, "#EXTM3U\n%s#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360%s\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1920x1080%s\nindex.m3u8\n", extra, aud, aud)
	case p == "/v/index.m3u8":
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprint(w, s.media())
	case p == "/v/low.m3u8":
		w.Header().Set("Content-Type", "application/x-mpegURL")
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:2,\nlow0.ts\n#EXT-X-ENDLIST\n")
	case p == "/v/key.bin":
		w.Write(s.key)
	case p == "/v/init.mp4":
		s.put(w, r, ip, s.init)
	case p == "/v/all.bin":
		if s.o.forbidIP[ip] {
			http.Error(w, "no", 403)
			return
		}
		var all []byte
		for _, g := range s.segs {
			all = append(all, g...)
		}
		s.put(w, r, ip, all)
	case strings.HasPrefix(p, "/v/seg"):
		var i int
		fmt.Sscanf(strings.TrimPrefix(p, "/v/seg"), "%d", &i)
		if i < 0 || i >= len(s.segs) {
			http.NotFound(w, r)
			return
		}
		if s.o.forbidIP[ip] || (s.o.token && r.URL.Query().Get("tok") != "abc") {
			http.Error(w, "no", 403)
			return
		}
		s.mu.Lock()
		f := s.fails[i]
		if f > 0 {
			s.fails[i]--
		}
		s.mu.Unlock()
		if f > 0 {
			http.Error(w, "oops", 500)
			return
		}
		b := s.segs[i]
		if s.o.enc {
			b = s.enc(i, b)
		}
		s.put(w, r, ip, b)
	default:
		http.NotFound(w, r)
	}
}

func hlsRun(t *testing.T, env dlEnv, link string, path string) (*dlTask, error) {
	t.Helper()
	tk := newDlTask(dlSpec{URL: link, Path: path, Lines: nil}, env)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := tk.Run(ctx)
	return tk, err
}

func twoLines() (*simEnv, *simLine, *simLine) {
	a, b := newSimLine("A", "127.0.0.1", 1.5*MiB), newSimLine("B", "127.0.0.2", 1.5*MiB)
	return newSimEnv(a, b), a, b
}

func checkHLS(t *testing.T, s *hlsTestSrv, tk *dlTask, err error, path string) {
	t.Helper()
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	got, e := os.ReadFile(tk.FinalPath())
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, s.expected()) {
		t.Fatalf("the joined video differs from the original parts (%d vs %d bytes)", len(got), len(s.expected()))
	}
	for _, left := range []string{hlsDir(path), hlsStatePath(path), dlPartPath(path)} {
		if _, e := os.Stat(left); e == nil {
			t.Errorf("left behind: %s", left)
		}
	}
}

func TestHLSDownloadOverTwoLines(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{})
	env, _, _ := twoLines()
	path := filepath.Join(t.TempDir(), "v.ts")
	st := time.Now()
	tk, err := hlsRun(t, env, s.URL+"/v/index.m3u8", path)
	checkHLS(t, s, tk, err, path)
	if s.bytesOn("127.0.0.1") == 0 || s.bytesOn("127.0.0.2") == 0 {
		t.Fatalf("both lines must carry parts: %v", s.ipB)
	}
	var total int64
	for _, g := range s.segs {
		total += int64(len(g))
	}
	if d := time.Since(st); float64(total)/d.Seconds() < 2.0*MiB {
		t.Fatalf("two 1.5 MiB/s lines gave only %.1f MiB/s", float64(total)/d.Seconds()/MiB)
	}
}

func TestHLSEncryptedStreams(t *testing.T) {
	for name, o := range map[string]hlsOpt{
		"iv from part number": {enc: true, seqStart: 5},
		"explicit iv":         {enc: true, explicit: true},
		"iv from number 0":    {enc: true},
	} {
		s := newHLSSrv(t, o)
		env, _, _ := twoLines()
		path := filepath.Join(t.TempDir(), "e.ts")
		tk, err := hlsRun(t, env, s.URL+"/v/index.m3u8", path)
		t.Run(name, func(t *testing.T) { checkHLS(t, s, tk, err, path) })
	}
}

func TestHLSFmp4AndByteRanges(t *testing.T) {
	for name, o := range map[string]hlsOpt{"fmp4": {fmp4: true, n: 10}, "byte ranges": {byteRange: true, n: 12}, "fmp4 encrypted": {fmp4: true, enc: true, n: 8}} {
		s := newHLSSrv(t, o)
		env, _, _ := twoLines()
		path := filepath.Join(t.TempDir(), "m.mp4")
		tk, err := hlsRun(t, env, s.URL+"/v/index.m3u8", path)
		t.Run(name, func(t *testing.T) { checkHLS(t, s, tk, err, path) })
	}
}

func TestHLSMasterPicksBestQuality(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{n: 6})
	env, _, _ := twoLines()
	path := filepath.Join(t.TempDir(), "m.ts")
	tk, err := hlsRun(t, env, s.URL+"/v/master.m3u8", path)
	checkHLS(t, s, tk, err, path)
	s.mu.Lock()
	lowHits := s.hits["/v/low.m3u8"] + s.hits["/v/low0.ts"]
	s.mu.Unlock()
	if lowHits != 0 {
		t.Fatal("the low quality stream was fetched")
	}
}

func TestHLSRefusals(t *testing.T) {
	cases := map[string]struct {
		o    hlsOpt
		url  string
		want string
	}{
		"live": {hlsOpt{live: true}, "/v/index.m3u8", "live stream"},
		"drm":  {hlsOpt{drm: true}, "/v/index.m3u8", "copy-protected"},
	}
	for name, c := range cases {
		s := newHLSSrv(t, c.o)
		env, _, _ := twoLines()
		_, err := hlsRun(t, env, s.URL+c.url, filepath.Join(t.TempDir(), "x.ts"))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
			continue
		}
		msg, retry := dlFriendly(err)
		if retry || msg != err.Error() {
			t.Errorf("%s: friendly=%q retry=%v", name, msg, retry)
		}
	}
}

func TestHLSNotReallyAPlaylistFallsBack(t *testing.T) {
	size := int64(3 * MiB)
	s := newSim(size)
	defer s.Close()
	// the sim server serves the file at any path: ask for it under a ".m3u8" name
	env, _, _ := twoLines()
	path := filepath.Join(t.TempDir(), "o.bin")
	tk, err := hlsRun(t, env, s.URL+"/file.bin?x=1#a.m3u8", path)
	mustOK(t, tk, err, size)
	srvPath := s.URL + "/file.m3u8"
	tk, err = hlsRun(t, env, srvPath, filepath.Join(t.TempDir(), "o2.bin"))
	mustOK(t, tk, err, size)
}

func TestHLSFailuresAndLineLoss(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{failSeg: map[int]int{3: 2, 7: 3, 11: 1}, forbidIP: map[string]bool{"127.0.0.2": true}})
	env, _, _ := twoLines()
	path := filepath.Join(t.TempDir(), "f.ts")
	tk, err := hlsRun(t, env, s.URL+"/v/index.m3u8", path)
	checkHLS(t, s, tk, err, path)
	if s.bytesOn("127.0.0.2") != 0 {
		t.Fatal("the refused line should not have carried data")
	}
}

func TestHLSGivesUpOnAPartThatNeverWorks(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{n: 5, failSeg: map[int]int{2: 1000}})
	env, _, _ := twoLines()
	old := dlNoLineGrace
	defer func() { dlNoLineGrace = old }()
	st := time.Now()
	_, err := hlsRun(t, env, s.URL+"/v/index.m3u8", filepath.Join(t.TempDir(), "g.ts"))
	if err == nil {
		t.Fatal("must fail")
	}
	if time.Since(st) > 50*time.Second {
		t.Fatal("took too long")
	}
}

func TestHLSResumeKeepsFinishedParts(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{n: 30})
	a, b := newSimLine("A", "127.0.0.1", 0.8*MiB), newSimLine("B", "127.0.0.2", 0.8*MiB)
	env := newSimEnv(a, b)
	path := filepath.Join(t.TempDir(), "r.ts")
	tk := newDlTask(dlSpec{URL: s.URL + "/v/index.m3u8", Path: path}, env)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tk.Run(ctx) }()
	time.Sleep(3 * time.Second)
	cancel()
	<-done
	have := 0
	finished := map[int]bool{}
	ents, _ := os.ReadDir(hlsDir(path))
	for _, e := range ents {
		var n int
		if !strings.HasSuffix(e.Name(), ".tmp") && e.Name() != "init" {
			if _, err := fmt.Sscanf(e.Name(), "%d", &n); err == nil {
				finished[n] = true
				have++
			}
		}
	}
	if have < 3 || have > 28 {
		t.Fatalf("expected a partly finished download, %d parts on disk", have)
	}
	s.mu.Lock()
	before := map[string]int{}
	for k, v := range s.hits {
		before[k] = v
	}
	s.mu.Unlock()
	a.bk.setRate(0)
	b.bk.setRate(0)
	env2 := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0))
	tk2, err := hlsRun(t, env2, s.URL+"/v/index.m3u8", path)
	checkHLS(t, s, tk2, err, path)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range finished {
		k := fmt.Sprintf("/v/seg%d.ts", i)
		if s.hits[k] != before[k] {
			t.Fatalf("part %d was finished but fetched again", i)
		}
	}
}

func TestHLSChangedPlaylistStartsOver(t *testing.T) {
	s1 := newHLSSrv(t, hlsOpt{n: 6})
	env, _, _ := twoLines()
	path := filepath.Join(t.TempDir(), "c.ts")
	// leave junk from a different stream
	os.MkdirAll(hlsDir(path), 0o755)
	os.WriteFile(hlsSegPath(hlsDir(path), 0), []byte("old junk"), 0o644)
	os.WriteFile(hlsStatePath(path), []byte(`{"v":1,"digest":"other","n":6}`), 0o644)
	tk, err := hlsRun(t, env, s1.URL+"/v/index.m3u8", path)
	checkHLS(t, s1, tk, err, path)
}

func TestHLSSignedLinksWithQuery(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{n: 5, token: true})
	env, _, _ := twoLines()
	path := filepath.Join(t.TempDir(), "q.ts")
	tk, err := hlsRun(t, env, s.URL+"/v/index.m3u8", path)
	checkHLS(t, s, tk, err, path)
}

func TestHLSParsing(t *testing.T) {
	a := hlsAttrs(`METHOD=AES-128,URI="https://k.example/a,b?x=1",IV=0x1F,FOO="q"`)
	if a["URI"] != "https://k.example/a,b?x=1" || a["IV"] != "0x1F" || a["METHOD"] != "AES-128" || a["FOO"] != "q" {
		t.Fatalf("%v", a)
	}
	body := "\ufeff#EXTM3U\r\n#EXT-X-MEDIA-SEQUENCE:7\r\n#EXT-X-KEY:METHOD=AES-128,URI=\"../k/key\",IV=0x10\r\n#EXT-X-BYTERANGE:100@50\r\n#EXTINF:1,\r\na.ts\r\n#EXT-X-BYTERANGE:200\r\n#EXTINF:1,t\r\na.ts\r\n#EXT-X-KEY:METHOD=NONE\r\n#EXTINF:1,\r\nhttps://cdn/x/b.ts\r\n#EXT-X-ENDLIST\r\n"
	_, md, err := hlsParse(body, "https://h.example/dir/sub/list.m3u8")
	if err != nil || md == nil || len(md.segs) != 3 {
		t.Fatalf("%v %v", md, err)
	}
	s0, s1, s2 := md.segs[0], md.segs[1], md.segs[2]
	if s0.uri != "https://h.example/dir/sub/a.ts" || s0.off != 50 || s0.len != 100 || s1.off != 150 || s1.len != 200 {
		t.Fatalf("byte ranges: %+v %+v", s0, s1)
	}
	if s0.keyURI != "https://h.example/dir/k/key" || len(s0.iv) != 16 || s0.iv[15] != 0x10 || s0.seq != 7 || s1.seq != 8 {
		t.Fatalf("key: %+v", s0)
	}
	if s2.keyURI != "" || s2.uri != "https://cdn/x/b.ts" || !md.endList || md.protectd {
		t.Fatalf("%+v", s2)
	}
	if _, _, err := hlsParse("<html>", "https://x/a"); err == nil {
		t.Fatal("html accepted")
	}
	if _, _, err := hlsParse("#EXTM3U\n#EXTINF:1,\nftp://x/a.ts\n", "https://x/a.m3u8"); err == nil {
		t.Fatal("non-web part accepted")
	}
	_, md, _ = hlsParse("#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"x\"\n#EXTINF:1,\na.ts\n#EXT-X-ENDLIST\n", "https://x/a.m3u8")
	if md == nil || !md.protectd {
		t.Fatal("sample-aes not flagged")
	}
	m, _, err := hlsParse("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nlo.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=9000,RESOLUTION=1x1\nhi.m3u8\n", "https://x/m.m3u8")
	if err != nil || hlsBest(m, 0).uri != "https://x/hi.m3u8" {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := hlsDecrypt(make([]byte, 15), make([]byte, 16), nil, 0); err == nil {
		t.Fatal("bad length accepted")
	}
	if _, err := hlsDecrypt(make([]byte, 32), make([]byte, 16), nil, 0); err == nil {
		t.Fatal("bad padding accepted")
	}
}

func TestHLSThroughTheManager(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{n: 8, enc: true})
	a, b := newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)
	m := newTestMgr(t, newSimEnv(a, b))
	id := addOne(t, m, s.URL+"/v/index.m3u8")
	v := waitState(t, m, id, "done", 40*time.Second)
	if !strings.HasSuffix(v.Name, ".ts") || strings.Contains(v.Name, "m3u8") {
		t.Fatalf("name %q", v.Name)
	}
	got, _ := os.ReadFile(v.Path)
	if !bytes.Equal(got, s.expected()) {
		t.Fatal("wrong content")
	}
	if filepath.Base(filepath.Dir(v.Path)) != "Videos" {
		t.Fatalf("category folder: %s", v.Path)
	}
	// probe describes the stream
	out, err := m.handle(dlReq{Action: "probe", dlAdd: dlAdd{URL: s.URL + "/v/index.m3u8"}})
	if err != nil || out["ok"] != true || out["hls"] == nil {
		t.Fatalf("%v %v", out, err)
	}
	h := out["hls"].(*hlsInfo)
	if h.Segments != 8 || h.Kind != "ts" || h.Seconds < 15 {
		t.Fatalf("%+v", h)
	}
	live := newHLSSrv(t, hlsOpt{live: true})
	out, _ = m.handle(dlReq{Action: "probe", dlAdd: dlAdd{URL: live.URL + "/v/index.m3u8"}})
	if out["ok"] != false || !strings.Contains(fmt.Sprint(out["error"]), "live") {
		t.Fatalf("%v", out)
	}
}

func TestHLSRemoveCleansEverything(t *testing.T) {
	s := newHLSSrv(t, hlsOpt{n: 40})
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0.3*MiB)))
	id := addOne(t, m, s.URL+"/v/index.m3u8")
	waitState(t, m, id, "downloading", 20*time.Second)
	time.Sleep(2 * time.Second)
	var v dlView
	for _, x := range m.views() {
		if x.ID == id {
			v = x
		}
	}
	if err := m.remove(id, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{hlsDir(v.Path), hlsStatePath(v.Path), dlPartPath(v.Path)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("left behind: %s", p)
		}
	}
}

// Real streams made by ffmpeg (skipped when ffmpeg is not installed): the joined file must play.
func TestHLSRealFFmpegStreams(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	probe, perr := exec.LookPath("ffprobe")
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "enc.key")
	os.WriteFile(keyFile, []byte("0123456789abcdef"), 0o644)
	os.WriteFile(filepath.Join(dir, "keyinfo"), []byte("enc.key\n"+keyFile+"\n00000000000000000000000000000001\n"), 0o644)
	gen := func(name string, extra ...string) {
		out := filepath.Join(dir, name)
		os.MkdirAll(out, 0o755)
		args := append([]string{"-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=14", "-f", "lavfi", "-i", "sine=frequency=440:duration=14",
			"-c:v", "libx264", "-preset", "ultrafast", "-g", "25", "-c:a", "aac", "-shortest", "-f", "hls", "-hls_time", "2", "-hls_playlist_type", "vod"}, extra...)
		args = append(args, "index.m3u8")
		cmd := exec.Command(ff, args...)
		cmd.Dir = out // ffmpeg writes the fMP4 init file next to where it runs, not next to the playlist, on some builds (Windows)
		if o, e := cmd.CombinedOutput(); e != nil {
			t.Skipf("ffmpeg cannot make %s here: %v %s", name, e, o)
		}
	}
	gen("plain")
	gen("aes", "-hls_key_info_file", filepath.Join(dir, "keyinfo"))
	os.WriteFile(filepath.Join(dir, "aes", "enc.key"), []byte("0123456789abcdef"), 0o644)
	gen("fmp4", "-hls_segment_type", "fmp4")
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer srv.Close()
	for _, name := range []string{"plain", "aes", "fmp4"} {
		env, _, _ := twoLines()
		ext := ".ts"
		if name == "fmp4" {
			ext = ".mp4"
		}
		path := filepath.Join(t.TempDir(), name+ext)
		tk, err := hlsRun(t, env, srv.URL+"/"+name+"/index.m3u8", path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if perr != nil {
			continue
		}
		o, e := exec.Command(probe, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", tk.FinalPath()).CombinedOutput()
		d, _ := strconv.ParseFloat(strings.TrimSpace(string(o)), 64)
		if e != nil || d < 12 || d > 16 {
			t.Fatalf("%s: ffprobe says duration %q err=%v", name, o, e)
		}
		if o, e := exec.Command(ff, "-v", "error", "-i", tk.FinalPath(), "-f", "null", "-").CombinedOutput(); e != nil || len(strings.TrimSpace(string(o))) > 0 {
			t.Fatalf("%s: the video does not decode cleanly: %s", name, o)
		}
	}
}

func TestStreamQualityCap(t *testing.T) {
	m := &hlsMaster{variants: []hlsVariant{
		{uri: "a", bandwidth: 800000, res: "640x360"}, {uri: "b", bandwidth: 2500000, res: "1280x720"}, {uri: "c", bandwidth: 6000000, res: "1920x1080"}}}
	for cap, want := range map[int]string{0: "c", 1080: "c", 720: "b", 500: "a", 100: "a"} {
		if got := hlsBest(m, cap).uri; got != want {
			t.Errorf("cap %d: got %s want %s", cap, got, want)
		}
	}
	if h := hlsHeights(m); len(h) != 3 || h[0] != 1080 || h[2] != 360 {
		t.Fatalf("%v", h)
	}
	// no sizes in the playlist: the cap changes nothing
	n := &hlsMaster{variants: []hlsVariant{{uri: "x", bandwidth: 1}, {uri: "y", bandwidth: 9}}}
	if hlsBest(n, 480).uri != "y" {
		t.Fatal("no sizes")
	}
}

func TestStreamQualityCapReachesThePlaylists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/m.m3u8", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=640x360\nlo.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720\nhi.m3u8\n")
	})
	for _, n := range []string{"lo", "hi"} {
		n := n
		mux.HandleFunc("/"+n+".m3u8", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\n%s0.ts\n#EXT-X-ENDLIST\n", n)
		})
	}
	mux.HandleFunc("/d.mpd", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<MPD type="static" mediaPresentationDuration="PT4S"><Period><AdaptationSet contentType="video">
<Representation id="s" bandwidth="500000" height="360" width="640"><BaseURL>s.mp4</BaseURL><SegmentBase/></Representation>
<Representation id="b" bandwidth="4000000" height="1080" width="1920"><BaseURL>b.mp4</BaseURL><SegmentBase/></Representation>
</AdaptationSet></Period></MPD>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := func(h int) context.Context { return context.WithValue(context.Background(), dlHeightKey{}, h) }
	for h, want := range map[int]string{0: "/hi0.ts", 360: "/lo0.ts"} {
		in, err := dlHLSInspect(ctx(h), http.DefaultClient, srv.URL+"/m.m3u8", nil)
		if err != nil || !strings.HasSuffix(in.media.segs[0].uri, want) || len(in.Heights) != 2 {
			t.Fatalf("hls %d: %v %+v", h, err, in)
		}
	}
	for h, want := range map[int]string{0: "1080", 480: "360"} {
		in, err := dlDASHInspect(ctx(h), http.DefaultClient, srv.URL+"/d.mpd", nil)
		if err != nil || !strings.Contains(in.mediaURL+fmt.Sprint(in.media.segs[0].uri), map[string]string{"1080": "b.mp4", "360": "s.mp4"}[want]) || len(in.Heights) != 2 {
			t.Fatalf("dash %d: %v %+v", h, err, in)
		}
	}
}
