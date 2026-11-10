package dm

// BitTorrent: one torrent download. It finds peers (trackers, DHT, peer exchange, magnet hints), connects to them
// through NetMerge's lines (each peer connection leaves through one line, weighted by what the line can carry), picks
// pieces rarest-first, checks every piece against its SHA-1 and writes it to the disk.
//
// Not included: incoming connections, encryption (MSE), uTP, seeding after the download is complete.

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const btBlock = 16 << 10

type btSpec struct {
	Meta       *btMeta
	Magnet     *btMagnet
	TorrentURL string // a link to a .torrent file
	Header     map[string]string
	Lines      []string
	MaxPeers   int
	Pick       func(name string) (string, error) // where the files go once the name is known
	OnInfo     func(m *btMeta, path string)
	SaveMeta   func(m *btMeta) // keeps a copy of the torrent description (so a magnet link resumes without peers)
	NoDHT      bool
	DHT        []string // replaces the built-in entry points (tests)
	Port       int
	Selected   []bool // files to download (nil = all)
	NoUpload   bool
	UploadRate float64 // bytes/s, 0 = no limit
	NoPrivate  bool    // added by a web page: peers and trackers inside the local network are not contacted
}

type btKey struct{ pc, off int }

type btPiece struct {
	idx      int
	buf      []byte
	blk      []uint8 // 0 free, 1 asked for, 2 received
	nGot     int
	claimers map[*btPeer]bool
	from     map[*btPeer]int
}

type btKnown struct {
	addr    string
	next    time.Time
	fails   int
	live    bool
	dialing bool
}

type btLineRun struct {
	name      string
	bytes     int64
	last      int64
	speed     float64
	conns     int
	dialing   int
	fails     int
	deadUntil time.Time
}

type btTask struct {
	spec   btSpec
	env    dlEnv
	bucket dlBucket
	upB    dlBucket // upload limit
	pid    [20]byte
	ih     [20]byte
	ctx    context.Context

	mu       sync.Mutex
	meta     *btMeta
	store    *btStore
	root     string
	ready    bool
	have     []bool
	want     []bool // pieces that belong to a selected file
	partial  []bool // pieces that also cover a file that was not chosen (those bytes are not kept, so such a piece is never uploaded)
	wantN    int
	haveN    int
	avail    []int
	active   map[int]*btPiece
	actB     int64
	peers    map[*btPeer]bool
	known    map[string]*btKnown
	banned   map[string]bool
	runs     map[string]*btLineRun
	order    []string
	note     string
	mf       *btMetaFetch
	upN      int
	dirty    bool
	trkSeed  int
	trkLeech int

	metaCh  chan struct{}
	readyCh chan struct{}
	doneCh  chan struct{}
	failCh  chan struct{}
	fatal   error
	once    sync.Once
	wg      sync.WaitGroup

	down, up   int64
	finalPath  string
	complete   bool
	startBytes int64
}

type btMetaFetch struct {
	size  int
	data  [][]byte
	state []uint8
	reqAt []time.Time
}

func newBtTask(spec btSpec, env dlEnv) *btTask {
	t := &btTask{spec: spec, env: env, active: map[int]*btPiece{}, peers: map[*btPeer]bool{}, known: map[string]*btKnown{},
		banned: map[string]bool{}, runs: map[string]*btLineRun{}, metaCh: make(chan struct{}), readyCh: make(chan struct{}),
		doneCh: make(chan struct{}), failCh: make(chan struct{})}
	t.upB.setRate(spec.UploadRate)
	copy(t.pid[:], "-NM3900-")
	rand.Read(t.pid[8:])
	if t.spec.Port == 0 {
		t.spec.Port = 6881
	}
	switch {
	case spec.Meta != nil:
		t.ih = spec.Meta.InfoHash
	case spec.Magnet != nil:
		t.ih = spec.Magnet.Hash
	}
	return t
}

func (t *btTask) fail(err error) {
	t.once.Do(func() { t.fatal = err; close(t.failCh) })
}

func (t *btTask) setNote(s string) { t.mu.Lock(); t.note = s; t.mu.Unlock() }

func (t *btTask) trackers() []string {
	if t.spec.Meta != nil && len(t.spec.Meta.Trackers) > 0 {
		return t.spec.Meta.Trackers
	}
	t.mu.Lock()
	m := t.meta
	t.mu.Unlock()
	if m != nil {
		return m.Trackers
	}
	if t.spec.Magnet != nil {
		return t.spec.Magnet.Trackers
	}
	return nil
}

func (t *btTask) isPrivate() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.meta != nil && t.meta.Private
}

// ---- lines -----------------------------------------------------------------------------------------------------------

func (t *btTask) liveLines() []dlLine {
	all := t.env.Lines()
	var out []dlLine
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, l := range all {
		if len(t.spec.Lines) > 0 {
			ok := false
			for _, n := range t.spec.Lines {
				if n == l.Name {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		if l.Alive != nil && !l.Alive() {
			continue
		}
		r := t.runs[l.Name]
		if r == nil {
			r = &btLineRun{name: l.Name}
			t.runs[l.Name] = r
			t.order = append(t.order, l.Name)
		}
		if now.Before(r.deadUntil) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// pickLine chooses the line that has the fewest peers for what it can carry.
func (t *btTask) pickLine(ls []dlLine) (dlLine, bool) {
	if len(ls) == 0 {
		return dlLine{}, false
	}
	var sum float64
	n := 0
	for _, l := range ls {
		if l.Rate > 0 {
			sum += l.Rate
			n++
		}
	}
	mean := 1.0
	if n > 0 {
		mean = sum / float64(n)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	best, bestScore := -1, math.MaxFloat64
	for i, l := range ls {
		w := l.Rate
		if w <= 0 {
			w = mean
		}
		r := t.runs[l.Name]
		score := float64(r.conns+r.dialing+1) / w
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	return ls[best], true
}

// ---- meta ------------------------------------------------------------------------------------------------------------

func (t *btTask) setMeta(m *btMeta) {
	t.mu.Lock()
	if t.meta != nil {
		t.mu.Unlock()
		return
	}
	t.meta = m
	n := m.pieces()
	t.have = make([]bool, n)
	t.avail = make([]int, n)
	t.want = make([]bool, n)
	for i := range t.want {
		t.want[i] = true
	}
	if t.spec.Selected != nil && len(t.spec.Selected) == len(m.Files) {
		for i := range t.want {
			t.want[i] = false
		}
		for fi, f := range m.Files {
			if !t.spec.Selected[fi] || f.Length == 0 {
				continue
			}
			a := int(f.off / m.PieceLen)
			b := int((f.off + f.Length - 1) / m.PieceLen)
			for p := a; p <= b && p < n; p++ {
				t.want[p] = true
			}
		}
	}
	for _, w := range t.want {
		if w {
			t.wantN++
		}
	}
	t.partial = make([]bool, n)
	if t.spec.Selected != nil && len(t.spec.Selected) == len(m.Files) {
		for fi, f := range m.Files {
			if t.spec.Selected[fi] || f.Length == 0 {
				continue
			}
			a := int(f.off / m.PieceLen)
			b := int((f.off + f.Length - 1) / m.PieceLen)
			for p := a; p <= b && p < n; p++ {
				t.partial[p] = true
			}
		}
	}
	t.ih = m.InfoHash
	close(t.metaCh)
	t.mu.Unlock()
	if t.spec.SaveMeta != nil {
		t.spec.SaveMeta(m)
	}
}

func (t *btTask) loadTorrentURL(ctx context.Context) (*btMeta, error) {
	ls := t.liveLines()
	if len(ls) == 0 {
		return nil, errors.New("no connection is available")
	}
	var last error
	for _, ln := range ls {
		tr := &http.Transport{DialContext: ln.Dial, DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second}
		cl := &http.Client{Transport: tr, Timeout: 40 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", t.spec.TorrentURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range t.spec.Header {
			req.Header.Set(k, v)
		}
		resp, err := cl.Do(req)
		if err != nil {
			last = err
			tr.CloseIdleConnections()
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		tr.CloseIdleConnections()
		if resp.StatusCode != 200 {
			last = dlHTTPErr{Code: resp.StatusCode}
			if resp.StatusCode == 404 || resp.StatusCode == 403 || resp.StatusCode == 410 {
				return nil, last
			}
			continue
		}
		if err != nil {
			last = err
			continue
		}
		return btParseTorrent(b)
	}
	return nil, last
}

// ---- running ---------------------------------------------------------------------------------------------------------

func (t *btTask) SetRate(r float64) { t.bucket.setRate(r) }

func (t *btTask) FinalPath() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finalPath
}

func (t *btTask) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	t.ctx = ctx
	defer func() {
		cancel()
		t.wg.Wait()
		if !t.completeNow() {
			go t.announceOnce("stopped") // tell the trackers we left (best effort)
		}
		t.mu.Lock()
		st, root, m, have := t.store, t.root, t.meta, append([]bool(nil), t.have...)
		t.mu.Unlock()
		if st != nil {
			st.sync()
			st.close()
			if t.fatal == nil && !t.completeNow() && m != nil && len(have) == m.pieces() {
				btSaveState(root, m, have, btStamps(st))
			}
		}
	}()

	switch {
	case t.spec.Meta != nil:
		t.setMeta(t.spec.Meta)
	case t.spec.TorrentURL != "":
		t.setNote("Getting the torrent file")
		m, err := t.loadTorrentURL(ctx)
		if err != nil {
			return err
		}
		t.ih = m.InfoHash
		t.setMeta(m)
	case t.spec.Magnet == nil:
		return errors.New("nothing to download")
	default:
		t.setNote("Looking for the torrent's description")
	}

	t.startWorkers()
	select {
	case <-t.metaCh:
	case <-t.failCh:
		return t.fatal
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := t.initStorage(ctx); err != nil {
		return err
	}
	t.setNote("")
	left := func() int64 { return t.leftBytes() }
	if left() == 0 {
		return t.finish()
	}
	select {
	case <-t.doneCh:
		return t.finish()
	case <-t.failCh:
		return t.fatal
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *btTask) leftBytes() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.meta == nil {
		return 1 << 30
	}
	var l int64
	for i, h := range t.have {
		if !h && t.want[i] {
			l += t.meta.pieceSize(i)
		}
	}
	return l
}

func (t *btTask) finish() error {
	t.mu.Lock()
	root := t.root
	t.mu.Unlock()
	_ = os.Remove(btStatePath(root))
	t.mu.Lock()
	t.complete = true
	t.finalPath = root
	t.mu.Unlock()
	t.markFiles(root)
	go t.announceOnce("completed") // best effort; the download is finished either way
	return nil
}

func (t *btTask) initStorage(ctx context.Context) error {
	t.mu.Lock()
	m := t.meta
	t.mu.Unlock()
	root, err := t.spec.Pick(m.Name)
	if err != nil {
		return err
	}
	if m.Total > 0 {
		sel := int64(0)
		for i, f := range m.Files {
			if t.spec.Selected == nil || len(t.spec.Selected) != len(m.Files) || t.spec.Selected[i] {
				sel += f.Length
			}
		}
		if free, err := dlFreeSpace(filepath.Dir(root)); err == nil && free > 0 && free < sel+(16<<20) {
			return errors.New("There is not enough space on the disk for this torrent.")
		}
	}
	if t.spec.OnInfo != nil {
		t.spec.OnInfo(m, root)
	}
	st := &btStore{meta: m, root: root}
	if t.spec.Selected != nil && len(t.spec.Selected) == len(m.Files) {
		st.skip = make([]bool, len(m.Files))
		for i, s := range t.spec.Selected {
			st.skip[i] = !s
		}
	}
	existed := false
	for i := range m.Files {
		if fi, err := os.Stat(st.pathOf(i)); err == nil && fi.Size() > 0 {
			existed = true
		}
	}
	if err := st.prepare(); err != nil {
		return err
	}
	have, saved := btLoadState(root, m)
	if have != nil {
		if err := t.repairFromState(ctx, st, have, saved); err != nil {
			st.close()
			return err
		}
	}
	if have != nil && !t.sampleOK(st, have) {
		have = nil
		existed = true
	}
	if have == nil && existed {
		t.setNote("Checking the files that are already there")
		have = make([]bool, m.pieces())
		buf := make([]byte, m.PieceLen)
		for i := range have {
			if ctx.Err() != nil {
				st.close()
				return ctx.Err()
			}
			have[i] = st.check(i, buf)
		}
	}
	t.mu.Lock()
	t.store, t.root = st, root
	if have != nil {
		for i, h := range have {
			if h {
				t.have[i] = true
				t.haveN++
			}
		}
	}
	t.ready = true
	close(t.readyCh)
	complete := true
	for i := range t.have {
		if t.want[i] && !t.have[i] {
			complete = false
		}
	}
	t.mu.Unlock()
	if complete {
		t.once2()
	}
	return nil
}

// repairFromState: the saved list says which pieces were finished, but files can change while NetMerge is closed.
// Every file whose size or time differs from what was saved (or that was deleted and made again) has all of its
// finished pieces hashed again; the ones that do not match are dropped from the list and fetched again.
func (t *btTask) repairFromState(ctx context.Context, st *btStore, have []bool, saved []btStamp) error {
	m := st.meta
	now := btStamps(st)
	need := make([]bool, len(have))
	any := false
	for i, f := range m.Files {
		if f.Length == 0 || (st.skip != nil && st.skip[i]) {
			continue
		}
		if saved != nil && saved[i] == now[i] {
			continue
		}
		first, last := int(f.off/m.PieceLen), int((f.off+f.Length-1)/m.PieceLen)
		for p := first; p <= last && p < len(need); p++ {
			need[p], any = true, true
		}
	}
	if !any {
		return nil
	}
	buf := make([]byte, m.PieceLen)
	dropped := 0
	for p := range need {
		if !need[p] || !have[p] {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// a piece that also covers a file that was not chosen cannot be hashed here; fetch it again to be safe
		if t.partial[p] || !st.check(p, buf) {
			have[p] = false
			dropped++
		}
	}
	if dropped > 0 {
		hLog("torrent: %d piece(s) were damaged or missing and will be fetched again", dropped)
		t.setNote("Some files changed while the torrent was stopped: fetching the damaged parts again")
	}
	return nil
}

func (t *btTask) once2() {
	select {
	case <-t.doneCh:
	default:
		close(t.doneCh)
	}
}

// ---- workers: announcing, DHT, connecting ---------------------------------------------------------------------------------

func (t *btTask) go_(name string, f func()) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				hLog("torrent: recovered from a crash in %s: %v", name, r)
			}
		}()
		f()
	}()
}

func (t *btTask) startWorkers() {
	if t.spec.Magnet != nil {
		t.addPeers(t.spec.Magnet.Peers)
	}
	t.go_("announce", t.announceLoop)
	if !t.spec.NoDHT {
		t.go_("dht", t.dhtLoop)
	}
	t.go_("connect", t.connectLoop)
	t.go_("stats", t.statsLoop)
}

func (t *btTask) addPeers(list []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, a := range list {
		if len(t.known) >= 4000 {
			break
		}
		if !btPeerAllowed(a, t.spec.NoPrivate) {
			continue
		}
		if t.known[a] == nil {
			t.known[a] = &btKnown{addr: a}
		}
	}
}

func (t *btTask) announceOnce(event string) {
	trs := t.trackers()
	if len(trs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	ls := t.env.Lines()
	if len(ls) == 0 {
		return
	}
	a := t.annArgs(event)
	for _, tr := range trs {
		wg.Add(1)
		go func(tr string) {
			defer wg.Done()
			defer func() { recover() }()
			_, _ = btAnnounce(ctx, ls[0], tr, a)
		}(tr)
	}
	wg.Wait()
}

func (t *btTask) annArgs(event string) btAnnArgs {
	return btAnnArgs{IH: t.ih, PeerID: t.pid, Left: t.leftBytes(), Down: atomic.LoadInt64(&t.down), Up: atomic.LoadInt64(&t.up), Event: event, Port: t.spec.Port}
}

func (t *btTask) announceLoop() {
	type trk struct {
		url   string
		next  time.Time
		fails int
		first bool
	}
	var list []*trk
	seen := map[string]bool{}
	for {
		for _, u := range t.trackers() {
			if !seen[u] && t.trackerAllowed(u) {
				seen[u] = true
				list = append(list, &trk{url: u, first: true})
			}
		}
		now := time.Now()
		for _, tr := range list {
			if now.Before(tr.next) {
				continue
			}
			ls := t.liveLines()
			ev := ""
			if tr.first {
				ev = "started"
			}
			var res *btAnn
			var err error
			ctx, cancel := context.WithTimeout(t.ctx, 30*time.Second)
			for _, ln := range ls {
				res, err = btAnnounce(ctx, ln, tr.url, t.annArgs(ev))
				if err == nil {
					break
				}
			}
			cancel()
			if t.ctx.Err() != nil {
				return
			}
			if err != nil || res == nil {
				tr.fails++
				d := time.Duration(30<<uint(min(tr.fails, 5))) * time.Second
				tr.next = time.Now().Add(d)
				continue
			}
			tr.fails, tr.first = 0, false
			t.addPeers(res.Peers)
			t.mu.Lock()
			if res.Seeds > t.trkSeed {
				t.trkSeed = res.Seeds
			}
			if res.Leech > t.trkLeech {
				t.trkLeech = res.Leech
			}
			np := len(t.peers)
			t.mu.Unlock()
			iv := res.Interval
			if iv < 60*time.Second {
				iv = 60 * time.Second
			}
			if iv > 30*time.Minute {
				iv = 30 * time.Minute
			}
			if np < 5 && iv > 5*time.Minute {
				iv = 5 * time.Minute
			}
			tr.next = time.Now().Add(iv)
		}
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (t *btTask) dhtLoop() {
	for {
		if t.isPrivate() {
			return
		}
		t.mu.Lock()
		np := len(t.peers)
		t.mu.Unlock()
		btDHTLookup(t.ctx, t.liveLines, t.ih, 25*time.Second, t.spec.DHT, t.addPeers)
		wait := 120 * time.Second
		if np < 5 {
			wait = 30 * time.Second
		}
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (t *btTask) connectLoop() {
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
		for i := 0; i < 12; i++ {
			if !t.dialOne() {
				break
			}
		}
	}
}

func (t *btTask) dialOne() bool {
	ls := t.liveLines()
	if len(ls) == 0 {
		return false
	}
	t.mu.Lock()
	if t.ready && t.haveN >= len(t.have) {
		t.mu.Unlock()
		return false
	}
	cur, dialing := len(t.peers), 0
	for _, k := range t.known {
		if k.dialing {
			dialing++
		}
	}
	if cur+dialing >= t.maxPeers(len(ls)) || dialing >= 12 {
		t.mu.Unlock()
		return false
	}
	var pick *btKnown
	now := time.Now()
	for _, k := range t.known {
		if k.live || k.dialing || now.Before(k.next) {
			continue
		}
		if host, _, _ := net.SplitHostPort(k.addr); t.banned[host] {
			continue
		}
		pick = k
		break
	}
	if pick == nil {
		t.mu.Unlock()
		return false
	}
	pick.dialing = true
	t.mu.Unlock()
	ln, _ := t.pickLine(ls)
	t.mu.Lock()
	t.runs[ln.Name].dialing++
	t.mu.Unlock()
	t.go_("peer", func() { t.runPeer(pick, ln) })
	return true
}

func (t *btTask) statsLoop() {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	saved := time.Now()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-tk.C:
		}
		t.mu.Lock()
		for _, r := range t.runs {
			b := atomic.LoadInt64(&r.bytes)
			d := float64(b - r.last)
			r.last = b
			r.speed = r.speed*0.6 + d*0.4
		}
		var save []bool
		var root string
		var m *btMeta
		if t.dirty && t.ready && time.Since(saved) > 5*time.Second {
			t.dirty = false
			save, root, m = append([]bool(nil), t.have...), t.root, t.meta
			saved = time.Now()
		}
		t.mu.Unlock()
		if save != nil {
			t.mu.Lock()
			st := t.store
			t.mu.Unlock()
			if st != nil {
				st.sync()
			}
			btSaveState(root, m, save, btStamps(st))
		}
	}
}

// ---- progress --------------------------------------------------------------------------------------------------------

func (t *btTask) Progress() dlProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	pr := dlProgress{Note: t.note}
	if t.meta == nil {
		if pr.Note == "" {
			pr.Note = "Looking for peers"
		}
		return pr
	}
	m := t.meta
	pr.Total = m.Total
	pr.Up = atomic.LoadInt64(&t.up)
	var done int64
	for i, h := range t.have {
		if h {
			done += m.pieceSize(i)
		}
	}
	for _, pc := range t.active {
		done += int64(pc.nGot) * btBlock
	}
	if done > m.Total {
		done = m.Total
	}
	pr.Done = done
	if t.wantN < len(t.have) {
		var tot, d int64
		for i := range t.have {
			if t.want[i] {
				tot += m.pieceSize(i)
				if t.have[i] {
					d += m.pieceSize(i)
				}
			}
		}
		pr.Total, pr.Done = tot, d
	}
	n := 80
	if n > len(t.have) {
		n = len(t.have)
	}
	pr.Map = make([]int, n)
	for i := 0; i < n; i++ {
		a, b := i*len(t.have)/n, (i+1)*len(t.have)/n
		c := 0
		for _, h := range t.have[a:b] {
			if h {
				c++
			}
		}
		pr.Map[i] = c * 100 / max(b-a, 1)
	}
	for _, name := range t.order {
		r := t.runs[name]
		st := dlLineStat{Name: name, Bytes: atomic.LoadInt64(&r.bytes), Workers: r.conns, Speed: r.speed}
		switch {
		case time.Now().Before(r.deadUntil):
			st.Status, st.Speed = "down", 0
		case r.conns > 0 && r.speed > 0:
			st.Status = "active"
		default:
			st.Status = "idle"
		}
		pr.Speed += st.Speed
		pr.Lines = append(pr.Lines, st)
	}
	for p := range t.peers {
		if p.handshook {
			pr.Peers++
			if p.haveN == len(t.have) && len(t.have) > 0 {
				pr.Seeds++
			}
		}
	}
	if t.trkSeed > pr.Seeds {
		pr.SwarmSeeds = t.trkSeed
	}
	if pr.Note == "" && pr.Peers == 0 && t.haveN < len(t.have) {
		pr.Note = "Looking for peers"
	}
	return pr
}

func btNew20(b []byte) string { return hex.EncodeToString(b) }

var _ = sha1.Sum
var _ = mrand.Intn
var _ = fmt.Sprint
var _ = btNew20

func (t *btTask) completeNow() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.complete }

var btAllowLoopback = false // tests talk to peers on the loopback

// btPeerAllowed refuses addresses that can never be a real peer (this PC, link-local, multicast) and, for torrents
// that a web page added, also addresses inside the local network.
func btPeerAllowed(addr string, noPrivate bool) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" || port == "0" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return !noPrivate || (host != "localhost" && !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".lan"))
	}
	switch {
	case ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return false
	case ip.IsLoopback():
		return btAllowLoopback
	case noPrivate && ip.IsPrivate():
		return false
	}
	return true
}

func (t *btTask) trackerAllowed(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Hostname() == "" {
		return false
	}
	port := p.Port()
	if port == "" {
		port = "80"
	}
	return btPeerAllowed(net.JoinHostPort(p.Hostname(), port), t.spec.NoPrivate)
}

// maxPeers: how many peer connections to keep (more lines, more peers), unless the caller fixed a number.
func (t *btTask) maxPeers(lines int) int {
	if t.spec.MaxPeers > 0 {
		return t.spec.MaxPeers
	}
	return min(150, 40*max(1, lines))
}

// sampleOK checks a few of the pieces the saved list says we have. A file that was changed, replaced or deleted while
// NetMerge was closed makes the saved list wrong; then everything is checked again.
func (t *btTask) sampleOK(st *btStore, have []bool) bool {
	var idx []int
	for i, h := range have {
		if h && !t.partial[i] {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return true
	}
	pick := map[int]bool{idx[0]: true, idx[len(idx)-1]: true}
	for len(pick) < min(8, len(idx)) {
		pick[idx[mrand.Intn(len(idx))]] = true
	}
	buf := make([]byte, st.meta.PieceLen)
	for i := range pick {
		if !st.check(i, buf) {
			return false
		}
	}
	return true
}

// markFiles puts the "downloaded from the internet" mark on the files (Windows asks before running them).
func (t *btTask) markFiles(root string) {
	t.mu.Lock()
	m := t.meta
	st := t.store
	t.mu.Unlock()
	if m == nil || st == nil {
		return
	}
	for i := range m.Files {
		if i >= 2000 {
			break
		}
		if st.skip != nil && st.skip[i] {
			continue
		}
		hMark(st.pathOf(i), "")
	}
}
