package dm

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- a configurable file server that misbehaves on request ---------------------------------------------------------

type simSrv struct {
	*httptest.Server
	mu          sync.Mutex
	size        int64
	noRange     bool
	perConn     float64 // bytes/s per connection (0 = unlimited)
	lockIP      bool    // the first address that asks owns the link
	owner       string
	maxPerIP    int // simultaneous connections per address, extra get 429
	live        map[string]int
	dropEvery   int64 // cut a response after this many bytes (0 = never)
	changeAfter int32 // after this many requests the ETag changes
	reqs        int32
	chunked     bool
	etag        string
	html        bool
	fails404    bool
	seen        map[string]int64 // bytes sent per client address
	ranged      int32
	failFirst   int32
	slowEvery   int32 // every Nth request is served at slowRate (a throttled connection)
	slowRate    float64
	needReferer bool // a request without a Referer is refused with 403 (anti-hotlinking)
}

func simByte(i int64) byte { return byte((i*131 + (i>>8)*17 + (i >> 16)) ^ 0x5a) }

func simFill(b []byte, off int64) {
	for i := range b {
		b[i] = simByte(off + int64(i))
	}
}

func simHash(size int64) string {
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for off := int64(0); off < size; off += int64(len(buf)) {
		n := int64(len(buf))
		if off+n > size {
			n = size - off
		}
		simFill(buf[:n], off)
		h.Write(buf[:n])
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func fileHash(t *testing.T, p string) string {
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func newSim(size int64) *simSrv {
	s := &simSrv{size: size, live: map[string]int{}, seen: map[string]int64{}, etag: `"v1"`}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *simSrv) handle(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	n := atomic.AddInt32(&s.reqs, 1)
	if s.needReferer && r.Header.Get("Referer") == "" {
		http.Error(w, "forbidden", 403)
		return
	}
	if s.failFirst > 0 && n <= s.failFirst {
		http.Error(w, "oops", 500)
		return
	}
	s.mu.Lock()
	if s.fails404 {
		s.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/redir" {
		s.mu.Unlock()
		http.Redirect(w, r, "/file.bin", 302)
		return
	}
	if s.lockIP {
		if s.owner == "" {
			s.owner = ip
		}
		if s.owner != ip {
			s.mu.Unlock()
			http.Error(w, "locked", 403)
			return
		}
	}
	if s.maxPerIP > 0 && s.live[ip] >= s.maxPerIP {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		http.Error(w, "busy", 429)
		return
	}
	s.live[ip]++
	etag := s.etag
	drop := s.dropEvery
	if s.changeAfter > 0 && n > s.changeAfter {
		etag = `"v2"`
	}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.live[ip]--; s.mu.Unlock() }()

	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
	if s.html {
		h.Set("Content-Type", "text/html")
		io.WriteString(w, "<html>please log in</html>")
		return
	}
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", `attachment; filename="sim file.bin"`)
	start, end := int64(0), s.size-1
	status := 200
	if rg := r.Header.Get("Range"); rg != "" && !s.noRange {
		if ir := r.Header.Get("If-Range"); ir == "" || ir == etag {
			spec := strings.TrimPrefix(rg, "bytes=")
			parts := strings.SplitN(spec, "-", 2)
			a, _ := strconv.ParseInt(parts[0], 10, 64)
			b := s.size - 1
			if len(parts) > 1 && parts[1] != "" {
				b, _ = strconv.ParseInt(parts[1], 10, 64)
			}
			if a >= s.size {
				h.Set("Content-Range", fmt.Sprintf("bytes */%d", s.size))
				w.WriteHeader(416)
				return
			}
			if b >= s.size {
				b = s.size - 1
			}
			start, end, status = a, b, 206
			atomic.AddInt32(&s.ranged, 1)
			h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, s.size))
		}
	}
	if !s.noRange {
		h.Set("Accept-Ranges", "bytes")
	}
	if !s.chunked {
		h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	}
	w.WriteHeader(status)
	buf := make([]byte, 16<<10)
	var sent int64
	for off := start; off <= end; {
		nn := int64(len(buf))
		if off+nn > end+1 {
			nn = end + 1 - off
		}
		simFill(buf[:nn], off)
		if _, err := w.Write(buf[:nn]); err != nil {
			return
		}
		off += nn
		sent += nn
		s.mu.Lock()
		s.seen[ip] += nn
		s.mu.Unlock()
		if drop > 0 && sent >= drop && off <= end {
			if hj, ok := w.(http.Hijacker); ok {
				c, _, _ := hj.Hijack()
				c.Close()
			}
			return
		}
		if s.perConn > 0 {
			time.Sleep(time.Duration(float64(nn) / s.perConn * float64(time.Second)))
		}
		if s.slowEvery > 0 && n%s.slowEvery == 0 {
			time.Sleep(time.Duration(float64(nn) / s.slowRate * float64(time.Second)))
		}
	}
}

func (s *simSrv) bytesFrom(ip string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[ip]
}

// ---- lines on the loopback: different source addresses, each with its own speed cap ------------------------------

type shapedConn struct {
	net.Conn
	b *dlBucket
}

func (c shapedConn) Read(p []byte) (int, error) {
	if len(p) > 8192 {
		p = p[:8192]
	}
	n, err := c.Conn.Read(p)
	if n > 0 {
		_ = c.b.wait(context.Background(), n)
	}
	return n, err
}

type simLine struct {
	name  string
	ip    string
	bk    *dlBucket
	alive atomic.Bool
	rate  float64
}

func newSimLine(name, ip string, capBps float64) *simLine {
	l := &simLine{name: name, ip: ip, bk: &dlBucket{}, rate: capBps}
	l.bk.setRate(capBps)
	l.alive.Store(true)
	return l
}

func (l *simLine) line() dlLine {
	d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(l.ip)}, Timeout: 5 * time.Second}
	return dlLine{Name: l.name, Kind: "ethernet", Rate: l.rate,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(network, "udp") { // the sources are TCP addresses; UDP just goes out from the machine
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
			}
			c, err := d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if l.bk.getRate() > 0 {
				return shapedConn{c, l.bk}, nil
			}
			return c, nil
		},
		Alive: func() bool { return l.alive.Load() }}
}

type simEnv struct {
	mu     sync.Mutex
	lines  []*simLine
	locked map[string]bool
	limit  func(line string, n int) error
}

func newSimEnv(ls ...*simLine) *simEnv { return &simEnv{lines: ls, locked: map[string]bool{}} }

func (e *simEnv) Lines() []dlLine {
	var out []dlLine
	for _, l := range e.lines {
		out = append(out, l.line())
	}
	return out
}
func (e *simEnv) Limit(ctx context.Context, line string, n int) error { return ctx.Err() }
func (e *simEnv) HostLocked(h string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.locked[h]
}
func (e *simEnv) LockHost(h string) { e.mu.Lock(); e.locked[h] = true; e.mu.Unlock() }
func (e *simEnv) Changed()          {}

func runDl(t *testing.T, env dlEnv, link string, spec dlSpec) (*dlTask, time.Duration, error) {
	t.Helper()
	if spec.Path == "" {
		spec.Path = filepath.Join(t.TempDir(), "out.bin")
	}
	spec.URL = link
	tk := newDlTask(spec, env)
	st := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	err := tk.Run(ctx)
	return tk, time.Since(st), err
}

func mustOK(t *testing.T, tk *dlTask, err error, size int64) {
	t.Helper()
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if got := fileHash(t, tk.FinalPath()); got != simHash(size) {
		t.Fatalf("file content is wrong")
	}
}

const MiB = 1 << 20

func (s *simSrv) set(f func()) { s.mu.Lock(); f(); s.mu.Unlock() }

func init() { enabledFlag.set(true); btAllowLoopback = true } // the tests use the manager as if it were switched on

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
