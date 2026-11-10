package dm

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	mrand "math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- test torrents -----------------------------------------------------------------------------------------------------

type tFile struct {
	name string
	size int64
}

type tTorrent struct {
	file    []byte // the .torrent
	meta    *btMeta
	content []byte
	files   []tFile
}

var tPrivate bool // makeTorrent marks the torrent private

func makeTorrent(name string, files []tFile, pieceLen int, trackers ...string) *tTorrent {
	var content []byte
	for _, f := range files {
		b := make([]byte, f.size)
		simFill(b, int64(len(content))+int64(len(name))*7)
		content = append(content, b...)
	}
	var hashes []byte
	for off := 0; off < len(content); off += pieceLen {
		end := min(off+pieceLen, len(content))
		h := sha1.Sum(content[off:end])
		hashes = append(hashes, h[:]...)
	}
	info := map[string]any{"name": name, "piece length": pieceLen, "pieces": string(hashes)}
	if tPrivate {
		info["private"] = 1
	}
	if len(files) == 1 && files[0].name == "" {
		info["length"] = len(content)
	} else {
		var fl []any
		for _, f := range files {
			fl = append(fl, map[string]any{"length": f.size, "path": splitPath(f.name)})
		}
		info["files"] = fl
	}
	top := map[string]any{"info": info}
	if len(trackers) > 0 {
		top["announce"] = trackers[0]
		var l []any
		for _, t := range trackers {
			l = append(l, t)
		}
		top["announce-list"] = []any{l}
	}
	file := bencode(top)
	m, err := btParseTorrent(file)
	if err != nil {
		panic(err)
	}
	return &tTorrent{file: file, meta: m, content: content, files: files}
}

// ---- a seeder ----------------------------------------------------------------------------------------------------------

type seeder struct {
	ln       net.Listener
	tt       *tTorrent
	noMeta   bool    // does not offer the description
	corrupt  int32   // number of pieces to send wrongly once
	rate     float64 // bytes/s per connection
	conns    int32
	served   int64
	requests int32
	dropAt   int64 // drop the connection after this many bytes (once)
	dropped  int32
	maxConns int
}

func newSeeder(t *testing.T, tt *tTorrent) *seeder {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &seeder{ln: l, tt: tt}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	t.Cleanup(func() { l.Close() })
	return s
}

func (s *seeder) addr() string { return s.ln.Addr().String() }

func (s *seeder) serve(c net.Conn) {
	defer c.Close()
	if s.maxConns > 0 && int(atomic.AddInt32(&s.conns, 1)) > s.maxConns {
		return
	}
	hs := make([]byte, 68)
	if _, err := io.ReadFull(c, hs); err != nil || !bytes.Equal(hs[28:48], s.tt.meta.InfoHash[:]) {
		return
	}
	my := make([]byte, 68)
	my[0] = 19
	copy(my[1:], "BitTorrent protocol")
	my[25] = 0x10
	copy(my[28:], s.tt.meta.InfoHash[:])
	copy(my[48:], "-SEEDER-123456789012")
	c.Write(my)
	m := s.tt.meta
	n := m.pieces()
	bits := make([]byte, (n+7)/8)
	for i := 0; i < n; i++ {
		bits[i/8] |= 0x80 >> (i % 8)
	}
	em := map[string]any{"m": map[string]any{"ut_metadata": 3}}
	if !s.noMeta {
		em["metadata_size"] = len(m.Raw)
	}
	c.Write(btFrame(20, []byte{0}, bencode(em)))
	c.Write(btFrame(5, bits))
	var sent int64
	for {
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(c, hdr); err != nil {
			return
		}
		ln := binary.BigEndian.Uint32(hdr)
		if ln == 0 {
			continue
		}
		buf := make([]byte, ln)
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		switch buf[0] {
		case 2:
			c.Write(btFrame(1))
		case 6:
			atomic.AddInt32(&s.requests, 1)
			idx, off, l := int(binary.BigEndian.Uint32(buf[1:])), int(binary.BigEndian.Uint32(buf[5:])), int(binary.BigEndian.Uint32(buf[9:]))
			a := idx*int(m.PieceLen) + off
			data := append([]byte(nil), s.tt.content[a:a+l]...)
			if atomic.LoadInt32(&s.corrupt) > 0 && off == 0 && atomic.AddInt32(&s.corrupt, -1) >= 0 {
				data[0] ^= 0xff
			}
			if s.rate > 0 {
				time.Sleep(time.Duration(float64(l) / s.rate * float64(time.Second)))
			}
			c.Write(btFrame(7, be32(idx), be32(off), data))
			atomic.AddInt64(&s.served, int64(l))
			sent += int64(l)
			if s.dropAt > 0 && sent > s.dropAt && atomic.AddInt32(&s.dropped, 1) == 1 {
				return
			}
		case 20:
			if buf[1] == 3 {
				v, k, _ := bdecodePrefix(buf[2:])
				d, _ := v.(map[string]any)
				_ = k
				if btInt(d, "msg_type") == 0 && !s.noMeta {
					p := int(btInt(d, "piece"))
					a, b := p*btBlock, min((p+1)*btBlock, len(m.Raw))
					c.Write(btFrame(20, []byte{1}, bencode(map[string]any{"msg_type": 1, "piece": p, "total_size": len(m.Raw)}), m.Raw[a:b]))
				}
			}
		}
	}
}

// ---- trackers ----------------------------------------------------------------------------------------------------------

func compactPeers(addrs ...string) string {
	var b []byte
	for _, a := range addrs {
		h, p, _ := net.SplitHostPort(a)
		ip := net.ParseIP(h).To4()
		port := 0
		for _, ch := range p {
			port = port*10 + int(ch-'0')
		}
		b = append(b, ip...)
		b = append(b, byte(port>>8), byte(port))
	}
	return string(b)
}

func httpTracker(t *testing.T, ih [20]byte, addrs ...string) (*httptest.Server, *int32) {
	var hits int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Query().Get("info_hash") != string(ih[:]) {
			w.Write(bencode(map[string]any{"failure reason": "unknown torrent"}))
			return
		}
		w.Write(bencode(map[string]any{"interval": 60, "complete": len(addrs), "peers": compactPeers(addrs...)}))
	}))
	t.Cleanup(s.Close)
	return s, &hits
}

func udpTracker(t *testing.T, ih [20]byte, addrs ...string) string {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n >= 16 && binary.BigEndian.Uint64(buf) == 0x41727101980 && binary.BigEndian.Uint32(buf[8:]) == 0 {
				r := make([]byte, 16)
				copy(r[4:], buf[12:16])
				copy(r[8:], []byte("CONNID12"))
				pc.WriteTo(r, from)
			} else if n >= 98 && binary.BigEndian.Uint32(buf[8:]) == 1 && string(buf[:8]) == "CONNID12" && bytes.Equal(buf[16:36], ih[:]) {
				r := make([]byte, 20)
				binary.BigEndian.PutUint32(r, 1)
				copy(r[4:], buf[12:16])
				binary.BigEndian.PutUint32(r[8:], 60)
				binary.BigEndian.PutUint32(r[16:], uint32(len(addrs)))
				r = append(r, compactPeers(addrs...)...)
				pc.WriteTo(r, from)
			}
		}
	}()
	return "udp://" + pc.LocalAddr().String() + "/announce"
}

// ---- helpers -----------------------------------------------------------------------------------------------------------

func runBt(t *testing.T, env dlEnv, spec btSpec, root string, timeout time.Duration) (*btTask, error) {
	t.Helper()
	spec.NoDHT = true
	if spec.Pick == nil {
		spec.Pick = func(string) (string, error) { return root, nil }
	}
	tk := newBtTask(spec, env)
	ctx, cancel := ctxTimeout(timeout)
	defer cancel()
	return tk, tk.Run(ctx)
}

func checkContent(t *testing.T, tt *tTorrent, root string) {
	t.Helper()
	if len(tt.files) == 1 && tt.files[0].name == "" {
		got, err := os.ReadFile(root)
		if err != nil || !bytes.Equal(got, tt.content) {
			t.Fatalf("single file content wrong (%v)", err)
		}
		return
	}
	off := 0
	for _, f := range tt.files {
		got, err := os.ReadFile(filepath.Join(root, f.name))
		if err != nil || !bytes.Equal(got, tt.content[off:off+int(f.size)]) {
			t.Fatalf("file %s content wrong (%v)", f.name, err)
		}
		off += int(f.size)
	}
}

func fakeHost() { var _ sync.Mutex; var _ = strings.ToLower }

func TestBtBencodeAndMeta(t *testing.T) {
	v := map[string]any{"a": []any{1, "xy", map[string]any{"k": int64(-5)}}, "b": "zz"}
	got, err := bdecode(bencode(v))
	if err != nil || !bytes.Equal(bencode(got), bencode(v)) {
		t.Fatalf("round trip: %v", err)
	}
	for _, bad := range []string{"", "i12", "5:ab", "l", "d1:ae", "di1e1:ae", "i--1e", strings.Repeat("l", 100) + strings.Repeat("e", 100)} {
		if _, err := bdecode([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	tt := makeTorrent("Album", []tFile{{"a.txt", 100000}, {"sub.txt", 70000}}, 32768, "http://t/announce")
	if tt.meta.Total != 170000 || tt.meta.pieces() != 6 || !tt.meta.Multi || len(tt.meta.Trackers) != 1 {
		t.Fatalf("%+v", tt.meta)
	}
	if tt.meta.pieceSize(5) != 170000-5*32768 {
		t.Fatal("last piece")
	}
	// the info hash is of the bytes as they are in the file
	raw, _ := topInfoRaw(tt.file)
	if sha1.Sum(raw) != tt.meta.InfoHash {
		t.Fatal("info hash")
	}
	// a file name that climbs out of the folder is refused
	evil := bencode(map[string]any{"info": map[string]any{"name": "x", "piece length": 32768, "pieces": strings.Repeat("a", 20),
		"files": []any{map[string]any{"length": 10, "path": []any{"..", "evil"}}}}})
	if _, err := btParseTorrent(evil); err == nil {
		t.Fatal("accepted a path that leaves the folder")
	}
	// the sizes have to agree
	bad := bencode(map[string]any{"info": map[string]any{"name": "x", "piece length": 32768, "pieces": strings.Repeat("a", 40), "length": 10}})
	if _, err := btParseTorrent(bad); err == nil {
		t.Fatal("accepted mismatching sizes")
	}
	mg, err := btParseMagnet("magnet:?xt=urn:btih:" + strings.Repeat("ab", 20) + "&dn=Hello%20World&tr=udp%3A%2F%2Fx%3A1%2Fa&x.pe=1.2.3.4:5")
	if err != nil || mg.Name != "Hello World" || len(mg.Trackers) != 1 || len(mg.Peers) != 1 || mg.Hash[0] != 0xab {
		t.Fatalf("%+v %v", mg, err)
	}
	if _, err := btParseMagnet("magnet:?dn=x"); err == nil {
		t.Fatal("magnet without a hash")
	}
	if _, err := btParseMagnet("magnet:?xt=urn:btih:zz"); err == nil {
		t.Fatal("magnet with a bad hash")
	}
	b32, err := btParseMagnet("magnet:?xt=urn:btih:" + base32OfHash(mg.Hash))
	if err != nil || b32.Hash != mg.Hash {
		t.Fatalf("base32 hash: %v", err)
	}
}

func TestBtSingleFileHTTPTracker(t *testing.T) {
	tt := makeTorrent("one.bin", []tFile{{"", 3*MiB + 777}}, 256<<10)
	sd := newSeeder(t, tt)
	tr, hits := httpTracker(t, tt.meta.InfoHash, sd.addr())
	tt = makeTorrent("one.bin", tt.files, 256<<10, tr.URL+"/announce")
	sd.tt = tt
	tr2, hits2 := httpTracker(t, tt.meta.InfoHash, sd.addr())
	_ = tr2
	_ = hits
	tt = makeTorrent("one.bin", tt.files, 256<<10, tr2.URL+"/announce")
	sd.tt = tt
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "one.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk, err := runBt(t, env, btSpec{Meta: meta}, root, 40*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
	if tk.FinalPath() != root || atomic.LoadInt32(hits2) == 0 {
		t.Fatalf("final=%s hits=%d", tk.FinalPath(), atomic.LoadInt32(hits2))
	}
	if _, err := os.Stat(btStatePath(root)); err == nil {
		t.Fatal("state file left behind")
	}
}

func TestBtMultiFileUDPTrackerTwoLines(t *testing.T) {
	files := []tFile{{"a/one.bin", 2*MiB + 5}, {"b.bin", 0}, {"two.bin", MiB + 123}}
	tt := makeTorrent("Pack", files, 128<<10)
	sd := newSeeder(t, tt)
	sd2 := newSeeder(t, tt)
	trk := udpTracker(t, tt.meta.InfoHash, sd.addr(), sd2.addr())
	tt2 := makeTorrent("Pack", files, 128<<10, trk)
	sd.tt, sd2.tt = tt2, tt2
	sd.rate, sd2.rate = 1.5*MiB, 1.5*MiB
	meta, _ := btParseTorrent(tt2.file)
	root := filepath.Join(t.TempDir(), "Pack")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0))
	tk, err := runBt(t, env, btSpec{Meta: meta}, root, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt2, root)
	pr := tk.Progress()
	got := 0
	for _, l := range pr.Lines {
		if l.Bytes > 0 {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("both lines should carry data: %+v", pr.Lines)
	}
}

func TestBtMagnetGetsDescriptionFromPeers(t *testing.T) {
	tt := makeTorrent("Movie", []tFile{{"m.mkv", 1*MiB + 99}, {"n.srt", 5000}}, 64<<10)
	sd := newSeeder(t, tt)
	mg, err := btParseMagnet(btMagnetLink(tt.meta.InfoHash, "", nil) + "&x.pe=" + sd.addr())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "Movie")
	var saved atomic.Bool
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk, err := runBt(t, env, btSpec{Magnet: mg, SaveMeta: func(m *btMeta) { saved.Store(true) }}, root, 40*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
	if !saved.Load() || tk.meta.Name != "Movie" {
		t.Fatal("description not kept")
	}
}

func TestBtBadPieceIsFetchedAgain(t *testing.T) {
	tt := makeTorrent("bad.bin", []tFile{{"", 2 * MiB}}, 128<<10)
	sd := newSeeder(t, tt)
	sd.corrupt = 2
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "bad.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers([]string{sd.addr()})
	ctx, cancel := ctxTimeout(40 * time.Second)
	defer cancel()
	if err := tk.Run(ctx); err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
}

func TestBtResumeAfterStop(t *testing.T) {
	tt := makeTorrent("res.bin", []tFile{{"", 6 * MiB}}, 128<<10)
	sd := newSeeder(t, tt)
	sd.rate = 2 * MiB
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "res.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	mk := func() *btTask {
		tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
		tk.addPeers([]string{sd.addr()})
		return tk
	}
	tk := mk()
	ctx, cancel := ctxTimeout(30 * time.Second)
	go func() {
		for tk.Progress().Done < 2*MiB {
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
	}()
	if err := tk.Run(ctx); err == nil {
		t.Fatal("should have been stopped")
	}
	if _, err := os.Stat(btStatePath(root)); err != nil {
		t.Fatalf("no state saved: %v", err)
	}
	before := atomic.LoadInt64(&sd.served)
	tk2 := mk()
	ctx2, cancel2 := ctxTimeout(40 * time.Second)
	defer cancel2()
	if err := tk2.Run(ctx2); err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
	if again := atomic.LoadInt64(&sd.served) - before; again > 5*MiB {
		t.Fatalf("downloaded %d bytes again; resume should skip finished pieces", again)
	}
}

func TestBtRecoversWhenPeerDrops(t *testing.T) {
	tt := makeTorrent("drop.bin", []tFile{{"", 3 * MiB}}, 128<<10)
	sd := newSeeder(t, tt)
	sd.dropAt = 700 << 10
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "drop.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers([]string{sd.addr()})
	ctx, cancel := ctxTimeout(60 * time.Second)
	defer cancel()
	if err := tk.Run(ctx); err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
}

func TestBtOnlySelectedFiles(t *testing.T) {
	files := []tFile{{"keep.bin", MiB}, {"skip.bin", 2 * MiB}, {"keep2.bin", 300000}}
	tt := makeTorrent("Sel", files, 64<<10)
	sd := newSeeder(t, tt)
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "Sel")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Selected: []bool{true, false, true}, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers([]string{sd.addr()})
	ctx, cancel := ctxTimeout(40 * time.Second)
	defer cancel()
	if err := tk.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skip.bin")); err == nil {
		t.Fatal("a file that was not chosen was created")
	}
	k1, _ := os.ReadFile(filepath.Join(root, "keep.bin"))
	if !bytes.Equal(k1, tt.content[:MiB]) {
		t.Fatal("keep.bin")
	}
	k2, _ := os.ReadFile(filepath.Join(root, "keep2.bin"))
	if !bytes.Equal(k2, tt.content[3*MiB:]) {
		t.Fatal("keep2.bin")
	}
	if got := atomic.LoadInt64(&sd.served); got > int64(MiB+300000+2*64<<10+64<<10) {
		t.Fatalf("downloaded %d bytes, the skipped file should not be fetched", got)
	}
}

func TestBtManagerMagnetAndTorrentFile(t *testing.T) {
	tt := makeTorrent("Mgr", []tFile{{"x.bin", 2 * MiB}, {"y.txt", 1000}}, 64<<10)
	sd := newSeeder(t, tt)
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	m := newTestMgr(t, env)
	link := btMagnetLink(tt.meta.InfoHash, "Mgr", nil) + "&x.pe=" + sd.addr()
	id := addOne(t, m, link)
	v := waitState(t, m, id, "done", 40*time.Second)
	if v.Kind != "torrent" || v.Category != "Torrents" || filepath.Base(v.Path) != "Mgr" || v.NFiles != 2 || v.Size != 2*MiB+1000 {
		t.Fatalf("%+v", v)
	}
	checkContent(t, tt, v.Path)
	// same hash again is a duplicate
	if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: link}}); err != nil {
		t.Fatal(err)
	}
	// a .torrent file with the seeder as its tracker-less peer: the file itself carries no peers, so use a tracker
	tr, _ := httpTracker(t, tt.meta.InfoHash, sd.addr())
	tt2 := makeTorrent("Other", []tFile{{"z.bin", 500000}}, 64<<10, tr.URL+"/a")
	sd.tt = tt2
	tr2, _ := httpTracker(t, tt2.meta.InfoHash, sd.addr())
	tt2 = makeTorrent("Other", []tFile{{"z.bin", 500000}}, 64<<10, tr2.URL+"/a")
	sd.tt = tt2
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{Torrent: b64(tt2.file), Force: true}})
	if err != nil {
		t.Fatal(err)
	}
	id2 := out["added"].([]*dlItem)[0].ID
	v2 := waitState(t, m, id2, "done", 40*time.Second)
	checkContent(t, tt2, v2.Path)
	// removing deletes the copy of the description
	if _, err := os.Stat(btCopyPath(btHashOfLink(btMagnetLink(tt2.meta.InfoHash, "", nil)))); err != nil {
		t.Fatalf("no kept description: %v", err)
	}
	if _, err := m.handle(dlReq{Action: "remove", ID: id2, DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v2.Path); err == nil {
		t.Fatal("files not deleted")
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func base32OfHash(h [20]byte) string { return base32.StdEncoding.EncodeToString(h[:]) }

func splitPath(p string) []any {
	var out []any
	for _, x := range strings.Split(p, "/") {
		out = append(out, x)
	}
	return out
}

// a node of the DHT that knows one peer, and another that only points to the first
func TestBtDHTFindsPeers(t *testing.T) {
	ih := [20]byte{1, 2, 3}
	start := func(reply func(tx string) map[string]any) string {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		go func() {
			buf := make([]byte, 2048)
			for {
				n, from, err := pc.ReadFrom(buf)
				if err != nil {
					return
				}
				v, _ := bdecode(buf[:n])
				q, _ := v.(map[string]any)
				if btStr(q, "q") != "get_peers" {
					continue
				}
				tx := btStr(q, "t")
				pc.WriteTo(bencode(map[string]any{"t": tx, "y": "r", "r": reply(tx)}), from)
			}
		}()
		return pc.LocalAddr().String()
	}
	leaf := start(func(string) map[string]any {
		return map[string]any{"id": strings.Repeat("x", 20), "token": "t", "values": []any{compactPeers("10.1.2.3:6881", "10.1.2.4:51413")}}
	})
	h, p, _ := net.SplitHostPort(leaf)
	var node []byte
	node = append(node, []byte(strings.Repeat("n", 20))...)
	node = append(node, []byte(compactPeers(net.JoinHostPort(h, p)))...)
	root := start(func(string) map[string]any {
		return map[string]any{"id": strings.Repeat("r", 20), "token": "t", "nodes": string(node)}
	})
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	var mu sync.Mutex
	var found []string
	ctx, cancel := ctxTimeout(10 * time.Second)
	defer cancel()
	btDHTLookupBoot(ctx, func() []dlLine { return env.Lines() }, ih, 5*time.Second, []string{root}, func(ps []string) {
		mu.Lock()
		found = append(found, ps...)
		mu.Unlock()
	})
	mu.Lock()
	defer mu.Unlock()
	if len(found) < 2 || found[0] != "10.1.2.3:6881" {
		t.Fatalf("peers: %v", found)
	}
}

func TestBtPeerFilter(t *testing.T) {
	old := btAllowLoopback
	btAllowLoopback = false
	defer func() { btAllowLoopback = old }()
	for a, want := range map[string]bool{"127.0.0.1:9196": false, "0.0.0.0:80": false, "169.254.1.1:80": false, "8.8.8.8:6881": true, "8.8.8.8:0": false,
		"[2001:db8::1]:6881": true, "nope": false, "192.168.1.5:6881": true, "peer.example.com:6881": true} {
		if got := btPeerAllowed(a, false); got != want {
			t.Errorf("%s: got %v", a, got)
		}
	}
	for a, want := range map[string]bool{"192.168.1.5:6881": false, "10.0.0.2:1": false, "localhost:80": false, "8.8.8.8:6881": true} {
		if got := btPeerAllowed(a, true); got != want {
			t.Errorf("web %s: got %v", a, got)
		}
	}
}

// the lines of the real product are read through the manager's own settings (which take its lock): starting a torrent
// must not ask for them while the lock is held
func TestBtStartDoesNotDeadlockOnLines(t *testing.T) {
	tt := makeTorrent("Lock", []tFile{{"", 300000}}, 64<<10)
	sd := newSeeder(t, tt)
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	m := newTestMgr(t, env)
	m.lines = func() []dlLine { m.config(); return env.Lines() }
	id := addOne(t, m, btMagnetLink(tt.meta.InfoHash, "Lock", nil)+"&x.pe="+sd.addr())
	done := make(chan struct{})
	go func() { waitState(t, m, id, "done", 30*time.Second); close(done) }()
	select {
	case <-done:
	case <-time.After(40 * time.Second):
		t.Fatal("stuck")
	}
}

func TestBtLeavesNoGoroutinesBehind(t *testing.T) {
	tt := makeTorrent("leak.bin", []tFile{{"", MiB}}, 64<<10)
	sd := newSeeder(t, tt)
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0))
	time.Sleep(200 * time.Millisecond)
	before := runtime.NumGoroutine()
	for i := 0; i < 3; i++ {
		root := filepath.Join(t.TempDir(), "leak.bin")
		tk := newBtTask(btSpec{Meta: tt.meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
		tk.addPeers([]string{sd.addr()})
		ctx, cancel := ctxTimeout(30 * time.Second)
		if err := tk.Run(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
	var after int
	for i := 0; i < 40; i++ {
		after = runtime.NumGoroutine()
		if after <= before+6 { // the seeder's own connections are still winding down
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	t.Fatalf("goroutines: %d before, %d after\n%s", before, after, buf[:n])
}

func FuzzBtParse(f *testing.F) {
	tt := makeTorrent("f", []tFile{{"a", 100000}, {"b/c", 5}}, 32768, "http://t/a")
	f.Add(tt.file)
	f.Add([]byte("d4:infod6:lengthi5e4:name1:x12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaaee"))
	f.Fuzz(func(t *testing.T, b []byte) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			bdecode(b)
			btParseTorrent(b)
			btParseMagnet(string(b))
			btCompactPeers(b, false)
			btCompactPeers(b, true)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("too slow on %d bytes", len(b))
		}
	})
}

// a peer that sends nonsense must not hurt the download, which another peer finishes
func TestBtHostilePeerIsHarmless(t *testing.T) {
	tt := makeTorrent("hostile.bin", []tFile{{"", MiB}}, 64<<10)
	good := newSeeder(t, tt)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				hs := make([]byte, 68)
				io.ReadFull(c, hs)
				my := make([]byte, 68)
				my[0] = 19
				copy(my[1:], "BitTorrent protocol")
				my[25] = 0x10
				copy(my[28:], tt.meta.InfoHash[:])
				copy(my[48:], "-HOSTILE12345678901-")
				c.Write(my)
				c.Write(btFrame(5, []byte{1, 2, 3}))                     // a bitfield of the wrong size
				c.Write(btFrame(4, be32(1<<30)))                         // a piece that does not exist
				c.Write(btFrame(7, be32(0), be32(0), []byte("junk")))    // data nobody asked for
				c.Write(btFrame(20, []byte{0}, []byte("not bencode")))   // a broken extension message
				c.Write(btFrame(20, []byte{1}, []byte("d3:fooi1ee")))    // metadata nobody asked for
				c.Write(btFrame(6, be32(1<<20), be32(0), be32(1<<31-1))) // an absurd request
				c.Write([]byte{0x7f, 0xff, 0xff, 0xff})                  // a frame of 2 GB
				time.Sleep(time.Second)
			}(c)
		}
	}()
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "hostile.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers([]string{l.Addr().String(), good.addr()})
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	if err := tk.Run(ctx); err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
}

func TestBtNeverReusesAnExistingFolder(t *testing.T) {
	tt := makeTorrent("Mine", []tFile{{"a.bin", 100000}, {"b/c.bin", 50000}}, 32<<10)
	sd := newSeeder(t, tt)
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	m := newTestMgr(t, env)
	m.cfg.OnExists = "overwrite"
	m.cfg.UseCategories = false
	// the person already has a folder with that name and a file in it
	mine := filepath.Join(m.cfg.Dir, "Mine")
	os.MkdirAll(mine, 0o755)
	keep := filepath.Join(mine, "precious.txt")
	os.WriteFile(keep, []byte("do not touch"), 0o644)
	id := addOne(t, m, btMagnetLink(tt.meta.InfoHash, "Mine", nil)+"&x.pe="+sd.addr())
	v := waitState(t, m, id, "done", 30*time.Second)
	if v.Path == mine {
		t.Fatalf("the torrent used the existing folder: %s", v.Path)
	}
	checkContent(t, tt, v.Path)
	if _, err := m.handle(dlReq{Action: "remove", ID: id, DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "do not touch" {
		t.Fatal("the person's own file was touched")
	}
	if _, err := os.Stat(v.Path); err == nil {
		t.Fatal("the torrent's folder was not cleaned up")
	}
}

func TestBtResumeNoticesChangedFiles(t *testing.T) {
	tt := makeTorrent("chg.bin", []tFile{{"", 3 * MiB}}, 128<<10)
	sd := newSeeder(t, tt)
	sd.rate = 3 * MiB
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "chg.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	mk := func() *btTask {
		tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
		tk.addPeers([]string{sd.addr()})
		return tk
	}
	tk := mk()
	ctx, cancel := ctxTimeout(30 * time.Second)
	go func() {
		for tk.Progress().Done < MiB {
			time.Sleep(30 * time.Millisecond)
		}
		cancel()
	}()
	tk.Run(ctx)
	// somebody damages the start and the end of the file while NetMerge is closed
	f, _ := os.OpenFile(root, os.O_RDWR, 0)
	f.WriteAt(bytes.Repeat([]byte{0xAA}, 4096), 0)
	f.Close()
	tk2 := mk()
	ctx2, cancel2 := ctxTimeout(40 * time.Second)
	defer cancel2()
	if err := tk2.Run(ctx2); err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
}

func TestBtTellsTrackerWhenStopped(t *testing.T) {
	tt := makeTorrent("stop.bin", []tFile{{"", 4 * MiB}}, 128<<10)
	sd := newSeeder(t, tt)
	sd.rate = 1 * MiB
	events := make(chan string, 20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events <- r.URL.Query().Get("event")
		w.Write(bencode(map[string]any{"interval": 60, "peers": compactPeers(sd.addr())}))
	}))
	defer srv.Close()
	tt2 := makeTorrent("stop.bin", tt.files, 128<<10, srv.URL+"/a")
	sd.tt = tt2
	meta, _ := btParseTorrent(tt2.file)
	root := filepath.Join(t.TempDir(), "stop.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
	ctx, cancel := ctxTimeout(30 * time.Second)
	go func() {
		for tk.Progress().Done < 300000 {
			time.Sleep(30 * time.Millisecond)
		}
		cancel()
	}()
	tk.Run(ctx)
	deadline := time.After(8 * time.Second)
	seen := map[string]bool{}
	for !seen["stopped"] {
		select {
		case e := <-events:
			seen[e] = true
		case <-deadline:
			t.Fatalf("events seen: %v", seen)
		}
	}
	if !seen["started"] {
		t.Fatal("no start event")
	}
}

func TestBtTorrentsCanBeSwitchedOff(t *testing.T) {
	m := newTestMgr(t, newSimEnv())
	m.cfg.NoTorrents = true
	for _, l := range []string{btMagnetLink([20]byte{1}, "x", nil), "http://example.com/a.torrent"} {
		if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: l}}); err == nil || !strings.Contains(err.Error(), "switched off") {
			t.Fatalf("%s: %v", l, err)
		}
	}
	// ordinary links still work
	if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: "http://example.com/a.zip", Start: new(bool)}}); err != nil {
		t.Fatal(err)
	}
}

// a peer that wants data from us; the first half of the torrent is already on the disk
func leecherTest(t *testing.T, noUpload bool, limit float64) (served int, uploaded int64) {
	tt := makeTorrent("up.bin", []tFile{{"", 2 * MiB}}, 64<<10)
	root := filepath.Join(t.TempDir(), "up.bin")
	half := make([]byte, 2*MiB)
	copy(half, tt.content[:MiB])
	os.WriteFile(root, half, 0o644)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	got := make(chan int, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			got <- -1
			return
		}
		defer c.Close()
		hs := make([]byte, 68)
		io.ReadFull(c, hs)
		my := make([]byte, 68)
		my[0] = 19
		copy(my[1:], "BitTorrent protocol")
		copy(my[28:], tt.meta.InfoHash[:])
		copy(my[48:], "-LEECHER12345678901-")
		c.Write(my)
		c.Write(btFrame(2))
		unchoked := false
		c.SetReadDeadline(time.Now().Add(4 * time.Second))
		for !unchoked {
			hdr := make([]byte, 4)
			if _, err := io.ReadFull(c, hdr); err != nil {
				got <- 0
				return
			}
			n := binary.BigEndian.Uint32(hdr)
			buf := make([]byte, n)
			io.ReadFull(c, buf)
			if n > 0 && buf[0] == 1 {
				unchoked = true
			}
		}
		c.Write(btFrame(6, be32(0), be32(0), be32(16384)))
		hdr := make([]byte, 4)
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		for {
			if _, err := io.ReadFull(c, hdr); err != nil {
				got <- 0
				return
			}
			n := binary.BigEndian.Uint32(hdr)
			buf := make([]byte, n)
			io.ReadFull(c, buf)
			if n > 9 && buf[0] == 7 {
				if bytes.Equal(buf[9:], tt.content[:16384]) {
					got <- 1
				} else {
					got <- -1
				}
				return
			}
		}
	}()
	meta, _ := btParseTorrent(tt.file)
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, NoUpload: noUpload, UploadRate: limit, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers([]string{l.Addr().String()})
	ctx, cancel := ctxTimeout(9 * time.Second)
	done := make(chan struct{})
	go func() { tk.Run(ctx); close(done) }()
	served = <-got
	time.Sleep(200 * time.Millisecond)
	up := tk.Progress().Up
	cancel()
	<-done // the task must be finished before the temp folder is removed
	return served, up
}

func TestBtUploadsWhileDownloadingWhenAllowed(t *testing.T) {
	if s, up := leecherTest(t, false, 0); s != 1 || up < 16384 {
		t.Fatalf("served=%d up=%d", s, up)
	}
	if s, _ := leecherTest(t, true, 0); s != 0 {
		t.Fatalf("uploaded although uploads are off: %d", s)
	}
	if s, up := leecherTest(t, false, 1<<20); s != 1 || up < 16384 {
		t.Fatalf("with a limit: served=%d up=%d", s, up)
	}
}

func TestBtPreviewThenAddWithChosenFiles(t *testing.T) {
	tt := makeTorrent("Pick", []tFile{{"keep.bin", 300000}, {"skip.bin", 300000}}, 64<<10)
	sd := newSeeder(t, tt)
	tr, _ := httpTracker(t, tt.meta.InfoHash, sd.addr())
	tt = makeTorrent("Pick", []tFile{{"keep.bin", 300000}, {"skip.bin", 300000}}, 64<<10, tr.URL+"/a")
	sd.tt = tt
	tr, _ = httpTracker(t, tt.meta.InfoHash, sd.addr())
	tt = makeTorrent("Pick", []tFile{{"keep.bin", 300000}, {"skip.bin", 300000}}, 64<<10, tr.URL+"/a")
	sd.tt = tt
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	pv, err := m.handle(dlReq{Action: "torrentInfo", dlAdd: dlAdd{Torrent: b64(tt.file)}})
	if err != nil {
		t.Fatal(err)
	}
	if fs := pv["files"].([]dlTFile); len(fs) != 2 || fs[1].Name != "skip.bin" || pv["name"] != "Pick" || pv["duplicate"] != false {
		t.Fatalf("%+v", pv)
	}
	if _, err := m.handle(dlReq{Action: "torrentInfo", dlAdd: dlAdd{Torrent: b64([]byte("junk"))}}); err == nil {
		t.Fatal("junk accepted")
	}
	if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{Torrent: b64(tt.file), Select: []bool{false, false}}}); err == nil {
		t.Fatal("no files chosen was accepted")
	}
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{Torrent: b64(tt.file), Select: []bool{true, false}}})
	if err != nil {
		t.Fatal(err)
	}
	v := waitState(t, m, out["added"].([]*dlItem)[0].ID, "done", 40*time.Second)
	if _, err := os.Stat(filepath.Join(v.Path, "keep.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v.Path, "skip.bin")); err == nil {
		t.Fatal("the unchosen file was downloaded")
	}
	if pv2, _ := m.handle(dlReq{Action: "torrentInfo", dlAdd: dlAdd{Torrent: b64(tt.file)}}); pv2["duplicate"] != true {
		t.Fatal("duplicate not noticed")
	}
}

func TestBtRefusesTorrentsWhoseFilesCollide(t *testing.T) {
	mk := func(paths ...[]string) error {
		var fl []any
		for _, p := range paths {
			var pl []any
			for _, x := range p {
				pl = append(pl, x)
			}
			fl = append(fl, map[string]any{"length": int64(100), "path": pl})
		}
		info := bencode(map[string]any{"name": "T", "piece length": int64(16384), "pieces": string(make([]byte, 20)), "files": fl})
		_, err := btParseInfo(info)
		return err
	}
	if err := mk([]string{"a.bin"}, []string{"b.bin"}); err != nil {
		t.Fatalf("good torrent refused: %v", err)
	}
	if err := mk([]string{"a.bin"}, []string{"sub", "b.bin"}, []string{"sub", "c.bin"}); err != nil {
		t.Fatalf("good torrent with folders refused: %v", err)
	}
	for name, p := range map[string][][]string{
		"same path twice":       {{"same.bin"}, {"same.bin"}},
		"file and folder":       {{"a"}, {"a", "b"}},
		"folder then file":      {{"a", "b"}, {"a"}},
		"capital letters":       {{"Same.bin"}, {"same.BIN"}},
		"cleaned names collide": {{"x:y"}, {"x?y"}},
	} {
		if err := mk(p...); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestBrowserDuplicateLeavesAWorkingDownload(t *testing.T) {
	m := newTestMgr(t, newSimEnv())
	link := "http://example.com/big.zip"
	first, err := m.add(dlAdd{URL: link, Start: new(bool)})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.pause(first.ID); err != nil {
		t.Fatal(err)
	}
	r, err := m.addFromBrowser(link, "", "", "", "")
	if err != nil || r["duplicate"] != true || r["restarted"] != true {
		t.Fatalf("a paused copy must be restarted: %v %v", r, err)
	}
	// a download that failed earlier is restarted too, not left failed or duplicated
	if err := m.pause(first.ID); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.find(first.ID).State = "error"
	m.find(first.ID).Err = "the server said no"
	m.mu.Unlock()
	count := func() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.items) }
	before := count()
	r, err = m.addFromBrowser(link, "", "", "", "")
	if err != nil || r["duplicate"] != true || r["restarted"] != true {
		t.Fatalf("a failed copy must be restarted: %v %v", r, err)
	}
	if count() != before {
		t.Fatalf("a failed copy must not be duplicated (%d rows, had %d)", count(), before)
	}
	m.mu.Lock()
	m.find(first.ID).State = "done"
	m.mu.Unlock()
	r, err = m.addFromBrowser(link, "", "", "", "")
	if err != nil || r["id"] == nil {
		t.Fatalf("a finished copy must give a new download: %v %v", r, err)
	}
}

// Damage in the middle of a torrent while it is closed: a changed byte, and a file that was deleted. The saved list
// of finished pieces must not be trusted for those; every damaged piece has to be fetched again.
func TestBtResumeRepairsMiddleDamageAndDeletedFile(t *testing.T) {
	for trial := 0; trial < 6; trial++ {
		tt := makeTorrent("mid", []tFile{{"one.bin", 2 * MiB}, {"sub/two.bin", 300000}, {"three.bin", 2 * MiB}}, 16<<10)
		sd := newSeeder(t, tt)
		sd.rate = 3 * MiB
		meta, _ := btParseTorrent(tt.file)
		root := filepath.Join(t.TempDir(), "mid")
		env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
		mk := func() *btTask {
			tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
			tk.addPeers([]string{sd.addr()})
			return tk
		}
		tk := mk()
		ctx, cancel := ctxTimeout(30 * time.Second)
		go func() {
			for tk.Progress().Done < 3*MiB {
				time.Sleep(20 * time.Millisecond)
			}
			cancel()
		}()
		tk.Run(ctx)
		f, _ := os.OpenFile(filepath.Join(root, "one.bin"), os.O_RDWR, 0)
		f.WriteAt([]byte{0x55}, 1*MiB+12345)
		f.Close()
		os.Remove(filepath.Join(root, "sub", "two.bin"))
		tk2 := mk()
		ctx2, cancel2 := ctxTimeout(40 * time.Second)
		err := tk2.Run(ctx2)
		cancel2()
		if err != nil {
			t.Fatal(err)
		}
		checkContent(t, tt, root)
	}
}

// A stress test for resuming: stop at a random point, damage the files in a random way (flip a byte anywhere, delete a
// file, cut a file short, make one longer, wipe a stretch with zeros, or do nothing), resume, and the result must be
// exactly the torrent. Run many rounds with a fixed seed so a failure can be repeated.
func TestBtResumeChaos(t *testing.T) {
	rng := mrand.New(mrand.NewSource(20261109))
	for round := 0; round < 14; round++ {
		tt := makeTorrent("chaos", []tFile{{"a.bin", 700000}, {"d/b.bin", 250000}, {"d/e/c.bin", 900000}, {"z.bin", 1}}, 16<<10)
		sd := newSeeder(t, tt)
		sd.rate = 4 * MiB
		meta, _ := btParseTorrent(tt.file)
		root := filepath.Join(t.TempDir(), "chaos")
		env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
		mk := func() *btTask {
			tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
			tk.addPeers([]string{sd.addr()})
			return tk
		}
		stopAt := int64(100000 + rng.Intn(1700000))
		tk := mk()
		ctx, cancel := ctxTimeout(30 * time.Second)
		go func() {
			for tk.Progress().Done < stopAt {
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
		}()
		tk.Run(ctx)
		cancel()
		names := []string{"a.bin", "d/b.bin", "d/e/c.bin", "z.bin"}
		what := rng.Intn(7)
		p := filepath.Join(root, filepath.FromSlash(names[rng.Intn(3)]))
		switch what {
		case 0: // flip a byte
			if f, err := os.OpenFile(p, os.O_RDWR, 0); err == nil {
				st, _ := f.Stat()
				f.WriteAt([]byte{0x7E}, rng.Int63n(max(st.Size(), 1)))
				f.Close()
			}
		case 1:
			os.Remove(p)
		case 2:
			if st, err := os.Stat(p); err == nil {
				os.Truncate(p, rng.Int63n(max(st.Size(), 1)))
			}
		case 3:
			if f, err := os.OpenFile(p, os.O_RDWR, 0); err == nil {
				st, _ := f.Stat()
				f.WriteAt([]byte("extra"), st.Size()+int64(rng.Intn(500)))
				f.Close()
			}
		case 4: // zeros over a stretch
			if f, err := os.OpenFile(p, os.O_RDWR, 0); err == nil {
				st, _ := f.Stat()
				n := int64(1 + rng.Intn(60000))
				f.WriteAt(make([]byte, n), rng.Int63n(max(st.Size()-n, 1)))
				f.Close()
			}
		case 5: // remove the saved list as well
			os.Remove(btStatePath(root))
			os.Remove(p)
		}
		tk2 := mk()
		ctx2, cancel2 := ctxTimeout(40 * time.Second)
		err := tk2.Run(ctx2)
		cancel2()
		if err != nil {
			t.Fatalf("round %d (damage %d, stopped at %d): %v", round, what, stopAt, err)
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("round %d (damage %d, stopped at %d): %v", round, what, stopAt, r)
				}
			}()
			off := 0
			for _, f := range tt.files {
				got, err := os.ReadFile(filepath.Join(root, f.name))
				if err != nil || !bytes.Equal(got, tt.content[off:off+int(f.size)]) {
					t.Fatalf("round %d (damage %d, stopped at %d): file %s is wrong (%v)", round, what, stopAt, f.name, err)
				}
				off += int(f.size)
			}
		}()
	}
}

// A peer that sends three pieces that fail the hash check is banned (by address); a good peer on another address
// still finishes the torrent, and the good peer is never banned.
func TestBtBansAPeerAfterThreeBadPieces(t *testing.T) {
	tt := makeTorrent("ban.bin", []tFile{{"", 3 * MiB}}, 64<<10)
	bad := newSeeder(t, tt)
	atomic.StoreInt32(&bad.corrupt, 100000) // every piece it sends is wrong
	good := newSeederAt(t, tt, "127.0.0.2:0")
	good.rate = 2 * MiB
	meta, _ := btParseTorrent(tt.file)
	root := filepath.Join(t.TempDir(), "ban.bin")
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers([]string{bad.addr()})
	ctx, cancel := ctxTimeout(40 * time.Second)
	defer cancel()
	go func() { time.Sleep(500 * time.Millisecond); tk.addPeers([]string{good.addr()}) }()
	if err := tk.Run(ctx); err != nil {
		t.Fatal(err)
	}
	checkContent(t, tt, root)
	tk.mu.Lock()
	defer tk.mu.Unlock()
	if !tk.banned["127.0.0.1"] {
		t.Fatal("the peer that sent bad pieces was not banned")
	}
	if tk.banned["127.0.0.2"] {
		t.Fatal("the good peer was banned")
	}
}

func newSeederAt(t *testing.T, tt *tTorrent, addr string) *seeder {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skip("cannot listen on " + addr + ": " + err.Error())
	}
	s := &seeder{ln: l, tt: tt}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	t.Cleanup(func() { l.Close() })
	return s
}

// T16: the upload limit really limits. A leecher asks for every block of the half of the file that is on the disk;
// with a limit of 200 KiB/s it must receive about that much per second, not the whole megabyte at once.
func TestBtUploadLimitHolds(t *testing.T) {
	const limit = 200 << 10
	tt := makeTorrent("lim.bin", []tFile{{"", 4 * MiB}}, 64<<10)
	root := filepath.Join(t.TempDir(), "lim.bin")
	data := make([]byte, 4*MiB)
	copy(data, tt.content[:2*MiB])
	os.WriteFile(root, data, 0o644)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	type res struct {
		bytes int
		secs  float64
	}
	out := make(chan res, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			out <- res{}
			return
		}
		defer c.Close()
		hs := make([]byte, 68)
		io.ReadFull(c, hs)
		my := make([]byte, 68)
		my[0] = 19
		copy(my[1:], "BitTorrent protocol")
		copy(my[28:], tt.meta.InfoHash[:])
		copy(my[48:], "-LEECHER12345678901-")
		c.Write(my)
		c.Write(btFrame(2))
		c.SetReadDeadline(time.Now().Add(6 * time.Second))
		readFrame := func() []byte {
			hdr := make([]byte, 4)
			if _, err := io.ReadFull(c, hdr); err != nil {
				return nil
			}
			b := make([]byte, binary.BigEndian.Uint32(hdr))
			io.ReadFull(c, b)
			return b
		}
		for {
			b := readFrame()
			if b == nil {
				out <- res{}
				return
			}
			if len(b) > 0 && b[0] == 1 {
				break
			}
		}
		for blk := 0; blk < 96; blk++ { // the first 1.5 MiB, one 16 KiB block at a time, all asked for at once
			c.Write(btFrame(6, be32(blk*16384/(64<<10)), be32(blk*16384%(64<<10)), be32(16384)))
		}
		start := time.Now()
		got := 0
		c.SetReadDeadline(time.Now().Add(2500 * time.Millisecond))
		for {
			b := readFrame()
			if b == nil {
				break
			}
			if len(b) > 9 && b[0] == 7 {
				got += len(b) - 9
			}
		}
		out <- res{got, time.Since(start).Seconds()}
	}()
	meta, _ := btParseTorrent(tt.file)
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
	tk := newBtTask(btSpec{Meta: meta, NoDHT: true, UploadRate: limit, Pick: func(string) (string, error) { return root, nil }}, env)
	tk.addPeers([]string{l.Addr().String()})
	ctx, cancel := ctxTimeout(12 * time.Second)
	done := make(chan struct{})
	go func() { tk.Run(ctx); close(done) }()
	r := <-out
	cancel()
	<-done
	allowed := int(float64(limit)*r.secs*1.5) + 96<<10 // a little room for the first burst
	if r.bytes == 0 {
		t.Fatal("nothing was uploaded")
	}
	if r.bytes > allowed {
		t.Fatalf("uploaded %d bytes in %.1fs with a limit of %d bytes/s (at most about %d expected)", r.bytes, r.secs, limit, allowed)
	}
	if r.bytes < int(float64(limit)*r.secs*0.3) {
		t.Fatalf("uploaded only %d bytes in %.1fs; the limit %d starves the peer", r.bytes, r.secs, limit)
	}
}

// T19: take a file off a torrent that is running. The file must be gone from the disk (not left half filled at its full
// size), the folder it was in goes with it when it is empty, and the rest finishes with the right content.
func TestBtDeselectingARunningTorrentDropsTheFile(t *testing.T) {
	tt := makeTorrent("Drop", []tFile{{"keep.bin", 600000}, {"sub/skip.bin", 600000}, {"last.bin", 600000}}, 64<<10)
	sd := newSeeder(t, tt)
	sd.rate = 600 << 10
	tr, _ := httpTracker(t, tt.meta.InfoHash, sd.addr())
	tt = makeTorrent("Drop", []tFile{{"keep.bin", 600000}, {"sub/skip.bin", 600000}, {"last.bin", 600000}}, 64<<10, tr.URL+"/a")
	sd.tt = tt
	tr, _ = httpTracker(t, tt.meta.InfoHash, sd.addr())
	tt = makeTorrent("Drop", []tFile{{"keep.bin", 600000}, {"sub/skip.bin", 600000}, {"last.bin", 600000}}, 64<<10, tr.URL+"/a")
	sd.tt = tt
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{Torrent: b64(tt.file)}})
	if err != nil {
		t.Fatal(err)
	}
	id := out["added"].([]*dlItem)[0].ID
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		v := m.find(id)
		m.mu.Lock()
		d := v.Done
		m.mu.Unlock()
		if d > 300000 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := m.setSelect(id, []bool{true, false, true}); err != nil {
		t.Fatal(err)
	}
	v := waitState(t, m, id, "done", 40*time.Second)
	if _, err := os.Stat(filepath.Join(v.Path, "sub", "skip.bin")); err == nil {
		t.Fatal("the file that was taken off is still on the disk")
	}
	if _, err := os.Stat(filepath.Join(v.Path, "sub")); err == nil {
		t.Fatal("the empty folder was left behind")
	}
	for _, f := range []struct {
		name string
		off  int
	}{{"keep.bin", 0}, {"last.bin", 1200000}} {
		got, err := os.ReadFile(filepath.Join(v.Path, f.name))
		if err != nil || !bytes.Equal(got, tt.content[f.off:f.off+600000]) {
			t.Fatalf("%s is wrong (%v)", f.name, err)
		}
	}
}

// T17: with torrents switched off every way in is closed, the file preview included; ordinary links still work.
func TestBtSwitchedOffClosesEveryWayIn(t *testing.T) {
	tt := makeTorrent("Off", []tFile{{"a.bin", 100000}}, 32<<10)
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	c := m.config()
	c.NoTorrents = true
	if err := m.setConfig(c); err != nil {
		t.Fatal(err)
	}
	const msg = "Torrents are switched off in the download settings."
	for name, req := range map[string]dlReq{
		"add .torrent": {Action: "add", dlAdd: dlAdd{Torrent: b64(tt.file)}},
		"preview":      {Action: "torrentInfo", dlAdd: dlAdd{Torrent: b64(tt.file)}},
		"add magnet":   {Action: "add", dlAdd: dlAdd{URL: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}},
	} {
		if _, err := m.handle(req); err == nil || err.Error() != msg {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// T20: a private torrent never offers peer exchange to a peer, and ignores the peers one sends; an ordinary torrent does offer it.
func TestBtPrivateTorrentOffersNoPeerExchange(t *testing.T) {
	offers := func(private bool) bool {
		tPrivate = private
		tt := makeTorrent("p.bin", []tFile{{"", 1 * MiB}}, 64<<10)
		tPrivate = false
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		defer l.Close()
		res := make(chan bool, 1)
		go func() {
			c, err := l.Accept()
			if err != nil {
				res <- false
				return
			}
			defer c.Close()
			hs := make([]byte, 68)
			io.ReadFull(c, hs)
			my := make([]byte, 68)
			my[0] = 19
			copy(my[1:], "BitTorrent protocol")
			my[25] = 0x10
			copy(my[28:], tt.meta.InfoHash[:])
			copy(my[48:], "-PEXCHECK1234567890-")
			c.Write(my)
			c.SetReadDeadline(time.Now().Add(4 * time.Second))
			for {
				hdr := make([]byte, 4)
				if _, err := io.ReadFull(c, hdr); err != nil {
					res <- false
					return
				}
				b := make([]byte, binary.BigEndian.Uint32(hdr))
				io.ReadFull(c, b)
				if len(b) > 2 && b[0] == 20 && b[1] == 0 {
					res <- bytes.Contains(b, []byte("ut_pex"))
					return
				}
			}
		}()
		meta, _ := btParseTorrent(tt.file)
		env := newSimEnv(newSimLine("A", "127.0.0.1", 0))
		root := filepath.Join(t.TempDir(), "p.bin")
		tk := newBtTask(btSpec{Meta: meta, NoDHT: true, Pick: func(string) (string, error) { return root, nil }}, env)
		tk.addPeers([]string{l.Addr().String()})
		ctx, cancel := ctxTimeout(6 * time.Second)
		done := make(chan struct{})
		go func() { tk.Run(ctx); close(done) }()
		r := <-res
		cancel()
		<-done
		return r
	}
	if !offers(false) {
		t.Fatal("an ordinary torrent should offer peer exchange")
	}
	if offers(true) {
		t.Fatal("a private torrent offered peer exchange")
	}
}

// E2: a link from the browser that leads into the local network is refused before the browser's own download is
// cancelled: by address, by a local-looking name, and by an ordinary name that resolves to an inside address.
func TestBrowserLinkIntoTheLocalNetworkIsRefusedUpFront(t *testing.T) {
	old := dlLookupHost
	defer func() { dlLookupHost = old }()
	dlLookupHost = func(ctx context.Context, host string) ([]net.IP, error) {
		switch host {
		case "files.corp.example":
			return []net.IP{net.ParseIP("10.1.2.3")}, nil
		case "mixed.example":
			return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("192.168.0.9")}, nil
		case "cdn.example":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return nil, errors.New("no such host")
	}
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	for _, bad := range []string{"http://192.168.1.5/a.zip", "http://127.0.0.1/a.zip", "http://nas/a.zip", "http://printer.local/a.zip",
		"http://files.corp.example/a.zip", "http://mixed.example/a.zip"} {
		if _, err := m.addFromBrowser(bad, "", "", "", ""); err == nil || !strings.Contains(err.Error(), "inside your own network") {
			t.Errorf("%s was not refused up front: %v", bad, err)
		}
	}
	m.mu.Lock()
	n := len(m.items)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("a refused link left %d rows behind", n)
	}
	for _, ok := range []string{"http://cdn.example/a.zip", "http://unknown.example/a.zip"} {
		if _, err := m.addFromBrowser(ok, "", "", "", ""); err != nil {
			t.Errorf("%s was refused: %v", ok, err)
		}
	}
}
