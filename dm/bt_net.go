package dm

// BitTorrent: finding peers through trackers (HTTP and UDP) and the DHT. Every request goes out through one of
// NetMerge's lines, so tracker and DHT traffic leave the same way the download does.

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type btAnn struct {
	Peers    []string
	Interval time.Duration
	Seeds    int
	Leech    int
}

func btCompactPeers(b []byte, v6 bool) []string {
	sz := 6
	if v6 {
		sz = 18
	}
	var out []string
	for i := 0; i+sz <= len(b); i += sz {
		var ip net.IP
		if v6 {
			ip = net.IP(b[i : i+16])
		} else {
			ip = net.IP(b[i : i+4])
		}
		port := int(binary.BigEndian.Uint16(b[i+sz-2:]))
		if port == 0 || ip.IsUnspecified() {
			continue
		}
		out = append(out, net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	}
	return out
}

type btAnnArgs struct {
	IH       [20]byte
	PeerID   [20]byte
	Left     int64
	Down, Up int64
	Event    string // "started" | "completed" | "stopped" | ""
	Port     int
}

func btAnnounce(ctx context.Context, ln dlLine, tracker string, a btAnnArgs) (*btAnn, error) {
	u, err := url.Parse(tracker)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return btHTTPAnnounce(ctx, ln, tracker, a)
	case "udp":
		return btUDPAnnounce(ctx, ln, u.Host, a)
	}
	return nil, errors.New("unsupported tracker")
}

func btHTTPAnnounce(ctx context.Context, ln dlLine, tracker string, a btAnnArgs) (*btAnn, error) {
	sep := "?"
	if strings.Contains(tracker, "?") {
		sep = "&"
	}
	q := fmt.Sprintf("%s%sinfo_hash=%s&peer_id=%s&port=%d&uploaded=%d&downloaded=%d&left=%d&compact=1&numwant=80",
		tracker, sep, url.QueryEscape(string(a.IH[:])), url.QueryEscape(string(a.PeerID[:])), a.Port, a.Up, a.Down, a.Left)
	if a.Event != "" {
		q += "&event=" + a.Event
	}
	tr := &http.Transport{DialContext: ln.Dial, DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", q, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NetMerge/"+H.Version)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tracker answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	v, err := bdecode(body)
	m, ok := v.(map[string]any)
	if err != nil || !ok {
		return nil, errors.New("tracker sent an unreadable answer")
	}
	if f := btStr(m, "failure reason"); f != "" {
		return nil, errors.New("tracker: " + f)
	}
	out := &btAnn{Interval: time.Duration(btInt(m, "interval")) * time.Second, Seeds: int(btInt(m, "complete")), Leech: int(btInt(m, "incomplete"))}
	switch p := m["peers"].(type) {
	case string:
		out.Peers = btCompactPeers([]byte(p), false)
	case []any:
		for _, e := range p {
			if d, ok := e.(map[string]any); ok {
				ip, port := btStr(d, "ip"), btInt(d, "port")
				if ip != "" && port > 0 && port < 65536 {
					out.Peers = append(out.Peers, net.JoinHostPort(ip, strconv.FormatInt(port, 10)))
				}
			}
		}
	}
	if p6, ok := m["peers6"].(string); ok {
		out.Peers = append(out.Peers, btCompactPeers([]byte(p6), true)...)
	}
	return out, nil
}

// btUDPAnnounce speaks BEP 15.
func btUDPAnnounce(ctx context.Context, ln dlLine, host string, a btAnnArgs) (*btAnn, error) {
	if _, _, err := net.SplitHostPort(host); err != nil {
		return nil, errors.New("tracker address has no port")
	}
	dctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	c, err := ln.Dial(dctx, "udp", host)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	go func() { <-dctx.Done(); c.Close() }()
	rt := func(req []byte, minLen int, action uint32, tx uint32) ([]byte, error) {
		buf := make([]byte, 4096)
		var last error
		for try := 0; try < 3; try++ {
			if _, err := c.Write(req); err != nil {
				return nil, err
			}
			_ = c.SetReadDeadline(time.Now().Add(time.Duration(4+try*3) * time.Second))
			n, err := c.Read(buf)
			if err != nil {
				last = err
				if dctx.Err() != nil {
					return nil, dctx.Err()
				}
				continue
			}
			if n >= 8 && binary.BigEndian.Uint32(buf[4:8]) == tx {
				if binary.BigEndian.Uint32(buf[0:4]) == 3 {
					return nil, errors.New("tracker: " + string(buf[8:n]))
				}
				if binary.BigEndian.Uint32(buf[0:4]) == action && n >= minLen {
					return buf[:n], nil
				}
			}
		}
		if last == nil {
			last = errors.New("tracker did not answer")
		}
		return nil, last
	}
	tx := func() uint32 { var b [4]byte; rand.Read(b[:]); return binary.BigEndian.Uint32(b[:]) }
	t1 := tx()
	req := make([]byte, 16)
	binary.BigEndian.PutUint64(req, 0x41727101980)
	binary.BigEndian.PutUint32(req[8:], 0)
	binary.BigEndian.PutUint32(req[12:], t1)
	resp, err := rt(req, 16, 0, t1)
	if err != nil {
		return nil, err
	}
	conn := resp[8:16]
	t2 := tx()
	ev := map[string]uint32{"": 0, "completed": 1, "started": 2, "stopped": 3}[a.Event]
	r2 := make([]byte, 98)
	copy(r2, conn)
	binary.BigEndian.PutUint32(r2[8:], 1)
	binary.BigEndian.PutUint32(r2[12:], t2)
	copy(r2[16:], a.IH[:])
	copy(r2[36:], a.PeerID[:])
	binary.BigEndian.PutUint64(r2[56:], uint64(a.Down))
	binary.BigEndian.PutUint64(r2[64:], uint64(a.Left))
	binary.BigEndian.PutUint64(r2[72:], uint64(a.Up))
	binary.BigEndian.PutUint32(r2[80:], ev)
	binary.BigEndian.PutUint32(r2[88:], 0xffffffff) // num_want = default
	binary.BigEndian.PutUint16(r2[96:], uint16(a.Port))
	resp, err = rt(r2, 20, 1, t2)
	if err != nil {
		return nil, err
	}
	out := &btAnn{Interval: time.Duration(binary.BigEndian.Uint32(resp[8:])) * time.Second,
		Leech: int(binary.BigEndian.Uint32(resp[12:])), Seeds: int(binary.BigEndian.Uint32(resp[16:]))}
	out.Peers = btCompactPeers(resp[20:], false)
	return out, nil
}

// ---- DHT (BEP 5): only enough to ask "who has this torrent?" -----------------------------------------------------------

var btDHTBootstrap = []string{"router.bittorrent.com:6881", "dht.transmissionbt.com:6881", "router.utorrent.com:6881", "dht.libtorrent.org:25401"}

type btNode struct {
	id    [20]byte
	addr  string
	asked bool
	boot  bool
}

func btXor(a, b [20]byte) (d [20]byte) {
	for i := range a {
		d[i] = a[i] ^ b[i]
	}
	return
}

func btDHTAsk(ctx context.Context, ln dlLine, addr string, msg []byte, txid string) (map[string]any, error) {
	c, err := ln.Dial(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if _, err := c.Write(msg); err != nil {
		return nil, err
	}
	buf := make([]byte, 8192)
	end := time.Now().Add(4 * time.Second)
	for {
		_ = c.SetReadDeadline(end)
		n, err := c.Read(buf)
		if err != nil {
			return nil, err
		}
		v, derr := bdecode(buf[:n])
		m, ok := v.(map[string]any)
		if derr != nil || !ok || btStr(m, "t") != txid {
			continue
		}
		if btStr(m, "y") != "r" {
			return nil, errors.New("dht error")
		}
		r, _ := m["r"].(map[string]any)
		if r == nil {
			return nil, errors.New("dht empty")
		}
		return r, nil
	}
}

// btDHTLookup walks towards the torrent's hash for at most `dur` and reports peers as it finds them.
func btDHTLookup(ctx context.Context, lines func() []dlLine, ih [20]byte, dur time.Duration, boot []string, onPeers func([]string)) {
	if boot == nil {
		boot = btDHTBootstrap
	}
	btDHTLookupBoot(ctx, lines, ih, dur, boot, onPeers)
}

func btDHTLookupBoot(ctx context.Context, lines func() []dlLine, ih [20]byte, dur time.Duration, boot []string, onPeers func([]string)) {
	ctx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()
	var myID [20]byte
	rand.Read(myID[:])
	var mu sync.Mutex
	nodes := map[string]*btNode{}
	for _, b := range boot {
		nodes[b] = &btNode{addr: b, boot: true}
	}
	queries := 0
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	next := func() *btNode {
		mu.Lock()
		defer mu.Unlock()
		var list []*btNode
		for _, n := range nodes {
			if !n.asked {
				list = append(list, n)
			}
		}
		if len(list) == 0 {
			return nil
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].boot != list[j].boot {
				return list[i].boot
			}
			a, b := btXor(list[i].id, ih), btXor(list[j].id, ih)
			return string(a[:]) < string(b[:])
		})
		list[0].asked = true
		queries++
		return list[0]
	}
	idle := 0
	rr := 0
	for ctx.Err() == nil {
		mu.Lock()
		q := queries
		mu.Unlock()
		if q >= 120 {
			break
		}
		n := next()
		if n == nil {
			idle++
			if idle > 30 {
				break
			}
			select {
			case <-ctx.Done():
			case <-time.After(150 * time.Millisecond):
			}
			continue
		}
		idle = 0
		ls := lines()
		if len(ls) == 0 {
			break
		}
		ln := ls[rr%len(ls)]
		rr++
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		tx := string([]byte{byte(rr), byte(rr >> 8)})
		wg.Add(1)
		go func(n *btNode, ln dlLine) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() { recover() }()
			msg := bencode(map[string]any{"t": tx, "y": "q", "q": "get_peers", "a": map[string]any{"id": string(myID[:]), "info_hash": string(ih[:])}})
			r, err := btDHTAsk(ctx, ln, n.addr, msg, tx)
			if err != nil {
				return
			}
			if vals, ok := r["values"].([]any); ok {
				var all []string
				for _, v := range vals {
					if s, ok := v.(string); ok {
						all = append(all, btCompactPeers([]byte(s), false)...)
					}
				}
				if len(all) > 0 {
					onPeers(all)
				}
			}
			if s, ok := r["nodes"].(string); ok {
				mu.Lock()
				for i := 0; i+26 <= len(s) && len(nodes) < 2000; i += 26 {
					var id [20]byte
					copy(id[:], s[i:i+20])
					addr := btCompactPeers([]byte(s[i+20:i+26]), false)
					if len(addr) == 1 && nodes[addr[0]] == nil {
						nodes[addr[0]] = &btNode{id: id, addr: addr[0]}
					}
				}
				mu.Unlock()
			}
		}(n, ln)
	}
	wg.Wait()
}
