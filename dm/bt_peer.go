package dm

// BitTorrent: talking to one peer (BEP 3 wire protocol, BEP 10 extensions, ut_metadata and ut_pex).

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type btMsg struct {
	id   byte
	data []byte
}

type btPeer struct {
	t    *btTask
	k    *btKnown
	addr string
	ln   dlLine
	run  *btLineRun
	conn net.Conn

	out   chan []byte
	in    chan btMsg
	done  chan struct{}
	kill_ sync.Once

	handshook bool
	start     time.Time
	have      []bool
	haveN     int
	rawBits   []byte
	rawHaves  []int
	sentBits  bool
	choked    bool
	interest  bool
	remoteInt bool
	weChoke   bool
	pending   map[btKey]time.Time
	claims    []*btPiece
	dup       map[*btPiece]bool
	ext       map[string]int
	metaSize  int
	noMeta    bool
	lastRecv  time.Time
	recv      int64
	lastRecvB int64
	rate      float64
	strikes   int
	canExt    bool
}

func (p *btPeer) kill() {
	p.kill_.Do(func() {
		close(p.done)
		if p.conn != nil {
			p.conn.Close()
		}
	})
}

func (p *btPeer) send(b []byte) bool {
	select {
	case <-p.done:
		return false
	case p.out <- b:
		return true
	default:
		p.kill()
		return false
	}
}

func btFrame(id byte, payload ...[]byte) []byte {
	n := 1
	for _, x := range payload {
		n += len(x)
	}
	b := make([]byte, 4, 4+n)
	binary.BigEndian.PutUint32(b, uint32(n))
	b = append(b, id)
	for _, x := range payload {
		b = append(b, x...)
	}
	return b
}

func be32(v int) []byte { var b [4]byte; binary.BigEndian.PutUint32(b[:], uint32(v)); return b[:] }

func (t *btTask) runPeer(k *btKnown, ln dlLine) {
	p := &btPeer{t: t, k: k, addr: k.addr, ln: ln, out: make(chan []byte, 600), in: make(chan btMsg, 64), done: make(chan struct{}),
		choked: true, weChoke: true, pending: map[btKey]time.Time{}, dup: map[*btPiece]bool{}, ext: map[string]int{}, start: time.Now(), lastRecv: time.Now()}
	t.mu.Lock()
	p.run = t.runs[ln.Name]
	t.mu.Unlock()
	registered := false
	defer func() {
		p.kill()
		t.dropPeer(p, registered)
	}()
	dctx, cancel := context.WithTimeout(t.ctx, 8*time.Second)
	c, err := ln.Dial(dctx, "tcp", p.addr)
	cancel()
	if err != nil {
		t.mu.Lock()
		k.dialing = false
		k.fails++
		k.next = time.Now().Add(time.Duration(30<<uint(min(k.fails, 5))) * time.Second)
		if k.fails >= 4 {
			delete(t.known, p.addr)
		}
		p.run.dialing--
		p.run.fails++
		if p.run.fails >= 10 && p.run.conns == 0 {
			p.run.deadUntil = time.Now().Add(20 * time.Second)
			p.run.fails = 0
		}
		t.mu.Unlock()
		return
	}
	p.conn = c
	t.mu.Lock()
	k.dialing, k.live = false, true
	p.run.dialing--
	p.run.conns++
	p.run.fails = 0
	t.peers[p] = true
	registered = true
	t.mu.Unlock()
	if err := p.handshake(); err != nil {
		t.mu.Lock()
		k.fails++
		k.next = time.Now().Add(time.Duration(60<<uint(min(k.fails, 4))) * time.Second)
		t.mu.Unlock()
		return
	}
	t.mu.Lock()
	p.handshook = true
	k.fails = 0
	t.mu.Unlock()
	p.loop()
}

func (t *btTask) dropPeer(p *btPeer, registered bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p.k != nil {
		p.k.live, p.k.dialing = false, false
		wait := 45 * time.Second
		if p.recv > 0 { // it was giving us data: try it again soon
			wait = 3 * time.Second
		}
		p.k.next = time.Now().Add(wait)
	}
	if !registered {
		return
	}
	delete(t.peers, p)
	p.run.conns--
	for key := range p.pending {
		if pc := t.active[key.pc]; pc != nil && pc.blk[key.off/btBlock] == 1 {
			pc.blk[key.off/btBlock] = 0
		}
	}
	for _, pc := range p.claims {
		delete(pc.claimers, p)
	}
	for i, h := range p.have {
		if h && t.avail != nil {
			t.avail[i]--
		}
	}
	if !p.weChoke {
		t.upN--
	}
}

func (p *btPeer) handshake() error {
	hs := make([]byte, 68)
	hs[0] = 19
	copy(hs[1:], "BitTorrent protocol")
	hs[25] = 0x10 // extension protocol
	copy(hs[28:], p.t.ih[:])
	copy(hs[48:], p.t.pid[:])
	_ = p.conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := p.conn.Write(hs); err != nil {
		return err
	}
	r := make([]byte, 68)
	if _, err := io.ReadFull(p.conn, r); err != nil {
		return err
	}
	if r[0] != 19 || string(r[1:20]) != "BitTorrent protocol" || !bytes.Equal(r[28:48], p.t.ih[:]) {
		return errors.New("not the same torrent")
	}
	if bytes.Equal(r[48:68], p.t.pid[:]) {
		return errors.New("that is ourselves")
	}
	p.canExt = r[25]&0x10 != 0
	_ = p.conn.SetDeadline(time.Time{})
	return nil
}

func (p *btPeer) reader() {
	defer close(p.in)
	var hdr [4]byte
	for {
		_ = p.conn.SetReadDeadline(time.Now().Add(150 * time.Second))
		if _, err := io.ReadFull(p.conn, hdr[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint32(hdr[:]))
		if n == 0 {
			select {
			case p.in <- btMsg{id: 255}:
			case <-p.done:
				return
			}
			continue
		}
		if n > 2<<20 {
			return
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(p.conn, buf); err != nil {
			return
		}
		select {
		case p.in <- btMsg{id: buf[0], data: buf[1:]}:
		case <-p.done:
			return
		}
	}
}

func (p *btPeer) writer() {
	for {
		select {
		case <-p.done:
			return
		case b := <-p.out:
			_ = p.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
			if _, err := p.conn.Write(b); err != nil {
				p.kill()
				return
			}
		}
	}
}

func (p *btPeer) loop() {
	t := p.t
	go p.writer()
	go p.reader()
	if p.canExt {
		ext := map[string]any{"ut_metadata": 1, "ut_pex": 2}
		if t.isPrivate() { // a private torrent finds its peers at its own tracker only: no peer exchange, not even offered
			delete(ext, "ut_pex")
		}
		m := map[string]any{"m": ext, "v": "NetMerge", "reqq": 128}
		t.mu.Lock()
		if t.meta != nil {
			m["metadata_size"] = len(t.meta.Raw)
		}
		t.mu.Unlock()
		p.send(btFrame(20, []byte{0}, bencode(m)))
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-p.done:
			return
		case m, ok := <-p.in:
			if !ok {
				return
			}
			p.lastRecv = time.Now()
			if !p.handle(m) {
				return
			}
		case <-tick.C:
			if !p.tick() {
				return
			}
		}
	}
}

func (p *btPeer) tick() bool {
	t := p.t
	now := time.Now()
	d := float64(p.recv - p.lastRecvB)
	p.lastRecvB = p.recv
	p.rate = p.rate*0.7 + d*0.3
	if len(p.pending) > 0 && now.Sub(p.lastRecv) > 25*time.Second {
		return false
	}
	if p.choked && now.Sub(p.start) > 120*time.Second && p.handshook {
		t.mu.Lock()
		needed := t.ready && t.haveN < len(t.have)
		t.mu.Unlock()
		if needed {
			return false
		}
	}
	if now.Sub(p.lastRecv) > 140*time.Second {
		return false
	}
	t.mu.Lock()
	ready, meta := t.ready, t.meta
	if meta != nil && p.have == nil {
		p.initHaveLocked()
	}
	var bits []byte
	if ready && !p.sentBits {
		p.sentBits = true
		if t.haveN > 0 {
			bits = t.bitfieldLocked()
		}
	}
	t.mu.Unlock()
	if bits != nil {
		p.send(btFrame(5, bits))
	}
	if now.Unix()%60 == 0 {
		p.send([]byte{0, 0, 0, 0})
	}
	if meta == nil {
		p.fetchMeta()
	} else {
		p.updateInterest()
		p.fill()
	}
	return true
}

func (t *btTask) bitfieldLocked() []byte {
	b := make([]byte, (len(t.have)+7)/8)
	for i, h := range t.have {
		if h {
			b[i/8] |= 0x80 >> (i % 8)
		}
	}
	return b
}

// initHaveLocked turns what the peer told us before we knew the piece count into the piece list.
func (p *btPeer) initHaveLocked() {
	t := p.t
	n := len(t.have)
	p.have = make([]bool, n)
	if p.rawBits != nil && len(p.rawBits) == (n+7)/8 {
		for i := 0; i < n; i++ {
			if p.rawBits[i/8]&(0x80>>(i%8)) != 0 {
				p.set(i)
			}
		}
	}
	for _, i := range p.rawHaves {
		if i >= 0 && i < n {
			p.set(i)
		}
	}
	p.rawBits, p.rawHaves = nil, nil
}

func (p *btPeer) set(i int) {
	if !p.have[i] {
		p.have[i] = true
		p.haveN++
		p.t.avail[i]++
	}
}

func (p *btPeer) updateInterest() {
	t := p.t
	if p.interest || p.have == nil {
		return
	}
	t.mu.Lock()
	need := false
	for i, h := range p.have {
		if h && !t.have[i] && t.want[i] {
			need = true
			break
		}
	}
	t.mu.Unlock()
	if need {
		p.interest = true
		p.send(btFrame(2))
	}
}

func (p *btPeer) handle(m btMsg) bool {
	t := p.t
	d := m.data
	switch m.id {
	case 255: // keep-alive
	case 0:
		p.choked = true
		t.mu.Lock()
		for k := range p.pending {
			if pc := t.active[k.pc]; pc != nil && pc.blk[k.off/btBlock] == 1 {
				pc.blk[k.off/btBlock] = 0
			}
		}
		t.mu.Unlock()
		p.pending = map[btKey]time.Time{}
	case 1:
		p.choked = false
		p.fill()
	case 2:
		p.remoteInt = true
		t.mu.Lock()
		ok := !t.spec.NoUpload && t.upN < 6 && p.ln.Kind != "mobile" && p.weChoke
		if ok {
			p.weChoke = false
			t.upN++
		}
		t.mu.Unlock()
		if ok {
			p.send(btFrame(1))
		}
	case 3:
		p.remoteInt = false
	case 4:
		if len(d) != 4 {
			return false
		}
		i := int(binary.BigEndian.Uint32(d))
		t.mu.Lock()
		if p.have != nil {
			if i < len(p.have) {
				p.set(i)
			}
		} else if len(p.rawHaves) < 100000 {
			p.rawHaves = append(p.rawHaves, i)
		}
		t.mu.Unlock()
		p.updateInterest()
		p.fill()
	case 5:
		t.mu.Lock()
		if p.have != nil {
			if len(d) != (len(p.have)+7)/8 {
				t.mu.Unlock()
				return false
			}
			for i := range p.have {
				if d[i/8]&(0x80>>(i%8)) != 0 {
					p.set(i)
				}
			}
		} else {
			p.rawBits = append([]byte(nil), d...)
		}
		t.mu.Unlock()
		p.updateInterest()
		p.fill()
	case 6:
		p.onRequest(d)
	case 7:
		return p.onPiece(d)
	case 20:
		return p.onExt(d)
	}
	return true
}

// ---- downloading ------------------------------------------------------------------------------------------------------

func (p *btPeer) depth() int {
	d := int(p.rate*0.8/btBlock) + 10
	if d > 80 {
		d = 80
	}
	return d
}

func (p *btPeer) fill() {
	t := p.t
	if p.choked || p.have == nil {
		return
	}
	depth := p.depth()
	for len(p.pending) < depth {
		idx, off, ln, ok := t.nextBlock(p)
		if !ok {
			return
		}
		p.pending[btKey{idx, off}] = time.Now()
		if !p.send(btFrame(6, be32(idx), be32(off), be32(ln))) {
			return
		}
	}
}

// nextBlock decides what this peer should ask for next.
func (t *btTask) nextBlock(p *btPeer) (idx, off, ln int, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ready || t.meta == nil {
		return
	}
	pick := func(pc *btPiece, dup bool) bool {
		for b := range pc.blk {
			key := btKey{pc.idx, b * btBlock}
			if pc.blk[b] == 0 || (dup && pc.blk[b] == 1) {
				if _, mine := p.pending[key]; mine {
					continue
				}
				if pc.blk[b] == 0 {
					pc.blk[b] = 1
				}
				idx, off = pc.idx, b*btBlock
				ln = btBlock
				if rest := len(pc.buf) - off; rest < ln {
					ln = rest
				}
				return true
			}
		}
		return false
	}
	live := p.claims[:0]
	for _, pc := range p.claims {
		if t.active[pc.idx] == pc {
			live = append(live, pc)
		} else {
			delete(p.dup, pc)
		}
	}
	p.claims = live
	for _, pc := range p.claims {
		if pick(pc, p.dup[pc]) {
			return idx, off, ln, true
		}
	}
	best, bestAv, cnt := -1, 1<<30, 0
	for i := range t.have {
		if t.have[i] || !t.want[i] || !p.have[i] {
			continue
		}
		if a := t.active[i]; a != nil && len(a.claimers) > 0 {
			continue
		}
		av := t.avail[i]
		if av < bestAv {
			best, bestAv, cnt = i, av, 1
		} else if av == bestAv {
			cnt++
			if rand.Intn(cnt) == 0 {
				best = i
			}
		}
	}
	if best >= 0 {
		pc := t.active[best]
		if pc == nil {
			sz := int(t.meta.pieceSize(best))
			if t.actB > 256<<20 && len(t.active) > 0 {
				return
			}
			nb := (sz + btBlock - 1) / btBlock
			pc = &btPiece{idx: best, buf: make([]byte, sz), blk: make([]uint8, nb), claimers: map[*btPeer]bool{}, from: map[*btPeer]int{}}
			t.active[best] = pc
			t.actB += int64(sz)
		}
		pc.claimers[p] = true
		p.claims = append(p.claims, pc)
		if pick(pc, false) {
			return idx, off, ln, true
		}
		return
	}
	// nothing new is left for this peer: help with pieces other peers are still fetching (the end of the download)
	for i, pc := range t.active {
		if p.have[i] && !pc.claimers[p] && len(pc.claimers) < 3 && pc.nGot < len(pc.blk) {
			pc.claimers[p] = true
			p.claims = append(p.claims, pc)
			p.dup[pc] = true
			if pick(pc, true) {
				return idx, off, ln, true
			}
		}
	}
	return
}

func (p *btPeer) onPiece(d []byte) bool {
	t := p.t
	if len(d) < 9 {
		return false
	}
	idx, off, data := int(binary.BigEndian.Uint32(d)), int(binary.BigEndian.Uint32(d[4:])), d[8:]
	key := btKey{idx, off}
	if _, ok := p.pending[key]; !ok {
		return true
	}
	delete(p.pending, key)
	n := len(data)
	if err := t.env.Limit(t.ctx, p.ln.Name, n); err != nil {
		return false
	}
	if err := t.bucket.wait(t.ctx, n); err != nil {
		return false
	}
	atomic.AddInt64(&p.run.bytes, int64(n))
	atomic.AddInt64(&t.down, int64(n))
	p.recv += int64(n)
	t.mu.Lock()
	pc := t.active[idx]
	if pc == nil || off%btBlock != 0 || off+n > len(pc.buf) || n != min(btBlock, len(pc.buf)-off) || pc.blk[off/btBlock] == 2 {
		t.mu.Unlock()
		p.fill()
		return true
	}
	copy(pc.buf[off:], data)
	pc.blk[off/btBlock] = 2
	pc.nGot++
	pc.from[p]++
	complete := pc.nGot == len(pc.blk)
	if complete {
		delete(t.active, idx)
		t.actB -= int64(len(pc.buf))
	}
	t.mu.Unlock()
	if complete {
		t.finishPiece(pc, p)
	}
	p.fill()
	return true
}

func (t *btTask) finishPiece(pc *btPiece, from *btPeer) {
	t.mu.Lock()
	m, st := t.meta, t.store
	t.mu.Unlock()
	sum := sha1.Sum(pc.buf)
	if string(sum[:]) != string(m.hashOf(pc.idx)) {
		t.mu.Lock()
		if len(pc.from) == 1 {
			for q := range pc.from {
				q.strikes++
				if q.strikes >= 3 {
					if host, _, err := net.SplitHostPort(q.addr); err == nil {
						t.banned[host] = true
						hLog("torrent: banned %s after %d pieces that failed the hash check", host, q.strikes)
					}
					q.kill()
				}
			}
		}
		t.mu.Unlock()
		return
	}
	if err := st.writeAt(int64(pc.idx)*m.PieceLen, pc.buf); err != nil {
		t.fail(errors.New("cannot write to the disk: " + err.Error()))
		return
	}
	t.mu.Lock()
	if !t.have[pc.idx] {
		t.have[pc.idx] = true
		t.haveN++
		t.dirty = true
	}
	for q := range t.peers {
		if q.sentBits {
			q.send(btFrame(4, be32(pc.idx)))
		}
	}
	all := true
	for i := range t.have {
		if t.want[i] && !t.have[i] {
			all = false
			break
		}
	}
	t.mu.Unlock()
	if all {
		t.once2()
	}
}

// ---- uploading (a little, to be a fair peer while downloading) --------------------------------------------------------------

func (p *btPeer) onRequest(d []byte) {
	t := p.t
	if len(d) != 12 || p.weChoke {
		return
	}
	idx, off, ln := int(binary.BigEndian.Uint32(d)), int(binary.BigEndian.Uint32(d[4:])), int(binary.BigEndian.Uint32(d[8:]))
	t.mu.Lock()
	ok := t.ready && idx >= 0 && idx < len(t.have) && t.have[idx] && !t.partial[idx] && ln > 0 && ln <= 32<<10 && off >= 0 && int64(off+ln) <= t.meta.pieceSize(idx)
	st, m := t.store, t.meta
	t.mu.Unlock()
	if !ok {
		return
	}
	buf := make([]byte, ln)
	if st.readAt(int64(idx)*m.PieceLen+int64(off), buf) != nil {
		return
	}
	if err := t.upB.wait(t.ctx, ln); err != nil {
		return
	}
	if p.send(btFrame(7, be32(idx), be32(off), buf)) {
		atomic.AddInt64(&t.up, int64(ln))
	}
}

// ---- extensions: metadata and peer exchange ------------------------------------------------------------------------------

func (p *btPeer) onExt(d []byte) bool {
	t := p.t
	if len(d) < 2 {
		return true
	}
	id, body := int(d[0]), d[1:]
	switch id {
	case 0:
		v, err := bdecode(body)
		m, ok := v.(map[string]any)
		if err != nil || !ok {
			return true
		}
		if mm, ok := m["m"].(map[string]any); ok {
			for k, x := range mm {
				if n, ok := x.(int64); ok && n > 0 && n < 255 {
					p.ext[k] = int(n)
				}
			}
		}
		if n := btInt(m, "metadata_size"); n > 0 && n <= 16<<20 {
			p.metaSize = int(n)
		}
		t.mu.Lock()
		meta := t.meta
		t.mu.Unlock()
		if meta == nil {
			p.fetchMeta()
		}
	case 1: // ut_metadata
		v, n, err := bdecodePrefix(body)
		m, ok := v.(map[string]any)
		if err != nil || !ok {
			return true
		}
		piece := int(btInt(m, "piece"))
		switch btInt(m, "msg_type") {
		case 0:
			p.serveMeta(piece)
		case 1:
			p.gotMeta(piece, body[n:])
		case 2:
			p.noMeta = true
		}
	case 2: // ut_pex
		v, err := bdecode(body)
		m, ok := v.(map[string]any)
		if err != nil || !ok || t.isPrivate() {
			return true
		}
		if s, ok := m["added"].(string); ok {
			t.addPeers(btCompactPeers([]byte(s), false))
		}
	}
	return true
}

func (p *btPeer) serveMeta(piece int) {
	t := p.t
	id := p.ext["ut_metadata"]
	t.mu.Lock()
	meta := t.meta
	t.mu.Unlock()
	if id == 0 {
		return
	}
	if meta == nil || piece < 0 || piece*btBlock >= len(meta.Raw) {
		p.send(btFrame(20, []byte{byte(id)}, bencode(map[string]any{"msg_type": 2, "piece": piece})))
		return
	}
	a, b := piece*btBlock, min((piece+1)*btBlock, len(meta.Raw))
	p.send(btFrame(20, []byte{byte(id)}, bencode(map[string]any{"msg_type": 1, "piece": piece, "total_size": len(meta.Raw)}), meta.Raw[a:b]))
}

func (p *btPeer) fetchMeta() {
	t := p.t
	id := p.ext["ut_metadata"]
	if id == 0 || p.metaSize <= 0 || p.noMeta {
		return
	}
	t.mu.Lock()
	if t.meta != nil {
		t.mu.Unlock()
		return
	}
	if t.mf == nil {
		np := (p.metaSize + btBlock - 1) / btBlock
		t.mf = &btMetaFetch{size: p.metaSize, data: make([][]byte, np), state: make([]uint8, np), reqAt: make([]time.Time, np)}
	}
	mf := t.mf
	if mf.size != p.metaSize {
		t.mu.Unlock()
		return
	}
	var ask []int
	now := time.Now()
	for i := range mf.state {
		if len(ask) >= 3 {
			break
		}
		if mf.state[i] == 0 || (mf.state[i] == 1 && now.Sub(mf.reqAt[i]) > 8*time.Second) {
			mf.state[i], mf.reqAt[i] = 1, now
			ask = append(ask, i)
		}
	}
	t.mu.Unlock()
	for _, i := range ask {
		p.send(btFrame(20, []byte{byte(id)}, bencode(map[string]any{"msg_type": 0, "piece": i})))
	}
}

func (p *btPeer) gotMeta(piece int, data []byte) {
	t := p.t
	t.mu.Lock()
	mf := t.mf
	if t.meta != nil || mf == nil || piece < 0 || piece >= len(mf.state) || mf.state[piece] == 2 {
		t.mu.Unlock()
		return
	}
	want := min(btBlock, mf.size-piece*btBlock)
	if len(data) != want {
		t.mu.Unlock()
		return
	}
	mf.data[piece] = append([]byte(nil), data...)
	mf.state[piece] = 2
	all := true
	for _, s := range mf.state {
		if s != 2 {
			all = false
		}
	}
	var raw []byte
	if all {
		for _, d := range mf.data {
			raw = append(raw, d...)
		}
		t.mf = nil
	}
	t.mu.Unlock()
	if !all {
		p.fetchMeta()
		return
	}
	sum := sha1.Sum(raw)
	if sum != t.ih {
		return // somebody sent wrong data; the next peers start over
	}
	m, err := btParseInfo(raw)
	if err != nil {
		t.fail(errors.New("the torrent's description from the peers is not usable: " + err.Error()))
		return
	}
	if t.spec.Magnet != nil {
		for _, tr := range t.spec.Magnet.Trackers {
			m.Trackers = append(m.Trackers, tr)
		}
	}
	t.setMeta(m)
}

var _ = strings.ToLower
