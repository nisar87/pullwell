package dm

// Download engine: splits one file into small pieces and fetches them over several lines at once. Each line has its own
// small pool of connections (bound to that adapter), pieces are handed out on demand so fast lines take more, and a
// piece that fails on one line is simply fetched again (on any line). Everything unusual falls back to something
// simpler: one line, one connection, start again - the file must always end up complete and correct.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type dlLine struct {
	Name  string
	Kind  string
	Rate  float64 // bytes/s the line is known to carry (0 = unknown); only used to order lines
	Dial  func(ctx context.Context, network, addr string) (net.Conn, error)
	Alive func() bool // nil = always usable
}

type dlEnv interface {
	Lines() []dlLine
	Limit(ctx context.Context, line string, n int) error
	HostLocked(host string) bool
	LockHost(host string)
	Changed()
}

type dlSpec struct {
	URL        string
	Header     map[string]string
	Path       string   // final file
	Lines      []string // allowed lines, empty = all
	MaxPerLine int
	// Pick chooses the final path once the server has told us the file name (used when Path is empty).
	Overwrite bool // replace a file that has the final name
	MaxHeight int  // streams: the tallest picture wanted, in pixels (0 = the best there is)
	NoPrivate bool // links that lead into the local network are refused even if typed (links sent by a web page)
	Pick      func(dlInfo) (string, error)
	// OnInfo reports what the server said (size, name, final path).
	OnInfo func(dlInfo, string)
}

type dlInfo struct {
	URL      string `json:"url"`
	FinalURL string `json:"finalUrl"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"` // -1 = unknown
	Ranges   bool   `json:"ranges"`
	ETag     string `json:"etag"`
	LastMod  string `json:"lastMod"`
	Warning  string `json:"warning,omitempty"`
}

type dlHTTPErr struct {
	Code  int
	Retry time.Duration
}

func (e dlHTTPErr) Error() string {
	return fmt.Sprintf("the server answered %d %s", e.Code, http.StatusText(e.Code))
}

var errDlChanged = errors.New("the file on the server changed while downloading")

// dlNoLineGrace: how long a download waits with every line down before it reports that no connection is available.
var dlNoLineGrace = 25 * time.Second

func dlUA() string { return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) NetMerge/" + H.Version }

const (
	dlMinPiece  = 512 << 10
	dlMaxPiece  = 16 << 20
	dlStall     = 25 * time.Second
	dlMaxFails  = 8
	dlStartConn = 4
	dlStartBig  = 8
)

// ---- http plumbing -------------------------------------------------------------------------------------------------

var dlAllowedHeaders = map[string]string{"cookie": "Cookie", "referer": "Referer", "user-agent": "User-Agent", "authorization": "Authorization",
	"accept": "Accept", "accept-language": "Accept-Language", "origin": "Origin"}

func dlCleanHeader(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if c, ok := dlAllowedHeaders[strings.ToLower(k)]; ok && len(v) < 8192 && !strings.ContainsAny(v, "\r\n") {
			out[c] = v
		}
	}
	return out
}

func dlLineClient(line dlLine, allowPrivate bool) *http.Client {
	tr := &http.Transport{
		Proxy: nil, // downloads go straight out of the chosen line
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := line.Dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if !allowPrivate {
				if ta, ok := c.RemoteAddr().(*net.TCPAddr); ok && dlPrivateIP(ta.IP) {
					_ = c.Close()
					return nil, errors.New("refused: the link leads to an address inside your own network")
				}
			}
			return c, nil
		},
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{}, // HTTP/1.1: one request per connection
		DisableCompression:    true,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &http.Client{Transport: tr, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
			return errors.New("redirect to an unsupported address")
		}
		return nil
	}}
}

func dlRequest(ctx context.Context, link string, hdr map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", link, nil)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, errors.New("only http and https links can be downloaded")
	}
	req.Header.Set("User-Agent", dlUA())
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return req, nil
}

func dlParseRange(v string) (start, end, total int64, ok bool) {
	// "bytes 0-0/12345" or "bytes */12345"
	v = strings.TrimSpace(strings.TrimPrefix(v, "bytes"))
	slash := strings.IndexByte(v, '/')
	if slash < 0 {
		return
	}
	rng, tot := strings.TrimSpace(v[:slash]), strings.TrimSpace(v[slash+1:])
	total = -1
	if tot != "*" {
		t, err := strconv.ParseInt(tot, 10, 64)
		if err != nil {
			return
		}
		total = t
	}
	if rng == "*" {
		return -1, -1, total, true
	}
	dash := strings.IndexByte(rng, '-')
	if dash < 0 {
		return
	}
	s, e1 := strconv.ParseInt(rng[:dash], 10, 64)
	e, e2 := strconv.ParseInt(rng[dash+1:], 10, 64)
	if e1 != nil || e2 != nil {
		return
	}
	return s, e, total, true
}

// dlProbe asks the server about a link with a one byte ranged request (more reliable than HEAD).
func dlProbe(ctx context.Context, c *http.Client, link string, hdr map[string]string) (dlInfo, error) {
	info := dlInfo{URL: link, Size: -1}
	req, err := dlRequest(ctx, link, hdr)
	if err != nil {
		return info, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	info.FinalURL = resp.Request.URL.String()
	info.Type = strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	info.ETag = resp.Header.Get("ETag")
	info.LastMod = resp.Header.Get("Last-Modified")
	info.Name = dlPickName(resp.Header.Get("Content-Disposition"), info.FinalURL)
	switch resp.StatusCode {
	case 206:
		s, _, total, ok := dlParseRange(resp.Header.Get("Content-Range"))
		if ok && s == 0 && total > 0 {
			info.Size, info.Ranges = total, true
		} else if ok && total < 0 {
			info.Size = -1 // unknown total: stream it
		} else {
			return info, errors.New("the server sent an unusable range answer")
		}
	case 200:
		info.Size = resp.ContentLength
	case 416:
		if _, _, total, ok := dlParseRange(resp.Header.Get("Content-Range")); ok && total == 0 {
			info.Size = 0
		} else {
			return info, dlHTTPErr{Code: 416}
		}
	default:
		return info, dlHTTPErr{Code: resp.StatusCode, Retry: dlRetryAfter(resp)}
	}
	if info.Type == "text/html" || info.Type == "application/xhtml+xml" {
		low := strings.ToLower(info.Name)
		if !(strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm") || strings.HasSuffix(low, ".xhtml")) && (info.Size < 0 || info.Size < 4<<20) {
			info.Warning = "html"
		}
	}
	return info, nil
}

// dlProbeReferer asks the server like dlProbe; when the server refuses (403) a request that carries no Referer, it asks
// once more with the site's own address as the referer, as download managers do (many servers only answer requests that
// look as if they came from their own page). The header that worked is returned so the download keeps using it.
func dlProbeReferer(ctx context.Context, c *http.Client, link string, hdr map[string]string) (dlInfo, map[string]string, error) {
	info, err := dlProbe(ctx, c, link, hdr)
	var he dlHTTPErr
	if err == nil || !errors.As(err, &he) || he.Code != 403 {
		return info, hdr, err
	}
	for k := range hdr {
		if strings.EqualFold(k, "Referer") {
			return info, hdr, err // the person or the browser gave one: do not second-guess it
		}
	}
	u, perr := url.Parse(link)
	if perr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return info, hdr, err
	}
	h2 := make(map[string]string, len(hdr)+1)
	for k, v := range hdr {
		h2[k] = v
	}
	h2["Referer"] = u.Scheme + "://" + u.Host + "/"
	if info2, err2 := dlProbe(ctx, c, link, h2); err2 == nil {
		return info2, h2, nil
	}
	return info, hdr, err
}

func dlRetryAfter(r *http.Response) time.Duration {
	if v := r.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			if n > 60 {
				n = 60
			}
			return time.Duration(n) * time.Second
		}
	}
	return 0
}

// ---- state kept on disk --------------------------------------------------------------------------------------------

type dlState struct {
	V         int     `json:"v"`
	URL       string  `json:"url"`
	FinalURL  string  `json:"finalUrl"`
	Name      string  `json:"name"`
	Size      int64   `json:"size"`
	ETag      string  `json:"etag"`
	LastMod   string  `json:"lastMod"`
	PieceSize int64   `json:"pieceSize"`
	Have      []int64 `json:"have"`
}

func dlPartPath(final string) string  { return final + ".nmpart" }
func dlStatePath(final string) string { return final + ".nmdl" }

func dlWriteFileAtomic(path string, b []byte) error { return dlWriteFileAtomicMode(path, b, 0o644) }

func dlWriteFileAtomicMode(path string, b []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- the task ------------------------------------------------------------------------------------------------------

type dlPiece struct {
	start, end int64 // inclusive
	have       int64
	done       bool
	fails      int
	cuts       int // how often a slow connection was replaced on this piece
	atts       []*dlAtt
}

func (p *dlPiece) size() int64 { return p.end - p.start + 1 }

type dlAtt struct {
	line   string
	cancel context.CancelFunc
	bytes  int64 // atomic: bytes this attempt has received
	since  time.Time
	prev   int64
	speed  float64 // bytes/s (smoothed)
	slow   bool    // it was cancelled for being far slower than the other connections of its line
}

type dlRun struct {
	line     dlLine
	client   *http.Client
	ctx      context.Context
	cancel   context.CancelFunc
	target   int
	active   int
	max      int
	bytes    int64 // atomic: bytes received on this line
	prev     int64
	speed    float64
	lastSp   float64
	lastTgt  int
	rejected bool
	cool     time.Time
	okPieces int
	errs     int
	refusals int
	dead     bool
	refSp    float64 // decaying peak of what finished pieces got on this line (bytes/s per connection)
}

type dlLineStat struct {
	Name    string  `json:"name"`
	Bytes   int64   `json:"bytes"`
	Speed   float64 `json:"speed"` // bytes/s
	Workers int     `json:"workers"`
	Status  string  `json:"status"` // "active" | "idle" | "left out" | "down"
}

type dlProgress struct {
	Done       int64        `json:"done"`
	Total      int64        `json:"total"`
	Speed      float64      `json:"speed"`
	Lines      []dlLineStat `json:"lines"`
	Note       string       `json:"note"`
	Map        []int        `json:"map,omitempty"`
	Peers      int          `json:"peers,omitempty"` // torrents: connected peers
	Seeds      int          `json:"seeds,omitempty"`
	SwarmSeeds int          `json:"swarmSeeds,omitempty"` // seeds the tracker knows of
	Up         int64        `json:"up,omitempty"`         // torrents: bytes uploaded
}

type dlTask struct {
	spec    dlSpec
	env     dlEnv
	bucket  dlBucket // per-download speed limit
	info    dlInfo
	hdr     map[string]string
	private bool

	mu           sync.Mutex
	pieces       []*dlPiece
	pieceSize    int64
	hls          *hlsJob
	hlsPrev      int64 // bytes of the tracks of a stream that are already finished (the sound)
	probeClients []*http.Client
	runs         map[string]*dlRun
	order        []string
	file         *os.File
	fatal        error
	single       bool // use one line only
	note         string
	finalPath    string
	started      time.Time
	dirty        int32
	stream       bool
	lastRefuse   dlHTTPErr
	idleSince    time.Time
	streamN      int64
	ctx          context.Context
	cancelAll    context.CancelFunc
	wg           sync.WaitGroup
	doneCh       chan struct{}
	doneOnce     sync.Once
}

func newDlTask(spec dlSpec, env dlEnv) *dlTask {
	if spec.MaxPerLine <= 0 {
		spec.MaxPerLine = 8
	}
	t := &dlTask{spec: spec, env: env, hdr: dlCleanHeader(spec.Header), doneCh: make(chan struct{}), runs: map[string]*dlRun{}}
	t.private = dlLocalHost(dlHostOf(spec.URL)) && !spec.NoPrivate
	return t
}

func (t *dlTask) setNote(s string) {
	t.mu.Lock()
	t.note = s
	t.mu.Unlock()
}

func (t *dlTask) allowed(name string) bool {
	if len(t.spec.Lines) == 0 {
		return true
	}
	for _, n := range t.spec.Lines {
		if n == name {
			return true
		}
	}
	return false
}

func (t *dlTask) usableLines() []dlLine {
	var out []dlLine
	for _, l := range t.env.Lines() {
		if t.allowed(l.Name) && (l.Alive == nil || l.Alive()) {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rate > out[j].Rate })
	return out
}

func (t *dlTask) fail(err error) {
	t.mu.Lock()
	if t.fatal == nil {
		t.fatal = err
	}
	t.mu.Unlock()
	t.cancelAll()
}

func (t *dlTask) closeIdle() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.runs {
		if r.client != nil {
			r.client.CloseIdleConnections()
		}
	}
	for _, c := range t.probeClients {
		c.CloseIdleConnections()
	}
}

// Run downloads until the file is complete (nil), the context ends (ctx error), or something cannot be recovered.
func (t *dlTask) Run(ctx context.Context) error {
	t.ctx, t.cancelAll = context.WithCancel(ctx)
	defer t.cancelAll()
	defer t.closeIdle() // do not leave kept-alive connections behind
	t.started = time.Now()

	lines := t.usableLines()
	if len(lines) == 0 {
		return errors.New("no connection is available right now")
	}
	// 1. ask the server (try each line until one answers)
	var perr error
	for _, l := range lines {
		pctx, c := context.WithTimeout(t.ctx, 30*time.Second)
		pc := dlLineClient(l, t.private)
		t.mu.Lock()
		t.probeClients = append(t.probeClients, pc)
		t.mu.Unlock()
		var info dlInfo
		var err error
		if dlIsFTP(t.spec.URL) {
			info, err = dlProbeFTP(pctx, l, t.spec.URL, t.hdr, t.private)
		} else {
			var h2 map[string]string
			info, h2, err = dlProbeReferer(pctx, pc, t.spec.URL, t.hdr)
			if err == nil {
				t.hdr = h2
			}
		}
		c()
		if err == nil {
			t.info, perr = info, nil
			break
		}
		perr = err
		if t.ctx.Err() != nil {
			return t.ctx.Err()
		}
		var he dlHTTPErr
		if errors.As(err, &he) && (he.Code == 404 || he.Code == 410) {
			break // another line will not change that
		}
	}
	if perr != nil {
		return perr
	}
	if dlDASHLike(t.info) {
		if handled, err := t.runDASH(); handled {
			return err
		}
	}
	if dlHLSLike(t.info) {
		if handled, err := t.runHLS(); handled {
			return err
		}
	}
	if t.spec.Path == "" {
		if t.spec.Pick == nil {
			return errors.New("no place to save the file")
		}
		p, err := t.spec.Pick(t.info)
		if err != nil {
			return err
		}
		t.spec.Path = p
	}
	if t.spec.OnInfo != nil {
		t.spec.OnInfo(t.info, t.spec.Path)
	}
	if err := t.prepareFile(); err != nil {
		return err
	}
	defer func() {
		if t.file != nil {
			t.file.Close()
		}
	}()

	if t.info.Size == 0 {
		return t.finish()
	}
	t.single = t.env.HostLocked(dlHostOf(t.spec.URL))
	if !t.info.Ranges || t.info.Size < 0 {
		t.stream = true
		t.single = true
		t.setNote("This site does not allow splitting the file, so one connection is used")
		err := t.runStream()
		if err != nil {
			return err
		}
		return t.finish()
	}
	if t.single {
		t.setNote("This site only accepts one connection address (learned earlier), so one line is used")
	}

	// 2. work
	t.supervise()
	t.wg.Wait()
	t.checkRefused()
	t.saveState()
	t.mu.Lock()
	fatal := t.fatal
	t.mu.Unlock()
	if fatal != nil {
		return fatal
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !t.allDone() {
		return errors.New("download stopped before the file was complete")
	}
	return t.finish()
}

// prepareFile creates the partial file, or picks up an earlier one when the server file is unchanged.
func (t *dlTask) prepareFile() error {
	part := dlPartPath(t.spec.Path)
	var st dlState
	haveState := false
	if b, err := os.ReadFile(dlStatePath(t.spec.Path)); err == nil && json.Unmarshal(b, &st) == nil && st.V == 1 {
		fi, e := os.Stat(part)
		same := e == nil && st.Size == t.info.Size && fi.Size() == t.info.Size && t.info.Ranges &&
			st.PieceSize >= dlMinPiece && st.PieceSize%65536 == 0 && (st.Size+st.PieceSize-1)/st.PieceSize <= 5000
		if same && st.ETag != "" && t.info.ETag != "" && st.ETag != t.info.ETag {
			same = false
		}
		if same && (st.ETag == "" || t.info.ETag == "") && st.LastMod != "" && t.info.LastMod != "" && st.LastMod != t.info.LastMod {
			same = false
		}
		haveState = same
	}
	if t.info.Size > 0 {
		if free, err := dlFreeSpace(filepathDir(t.spec.Path)); err == nil {
			need := t.info.Size + 32<<20
			if haveState { // part of the file is already on the disk
				if fi, e := os.Stat(part); e == nil {
					need -= fi.Size()
				}
			}
			if free < need {
				return fmt.Errorf("not enough space on the disk: %d MB needed, %d MB free", t.info.Size>>20, free>>20)
			}
		}
	}
	flags := os.O_RDWR | os.O_CREATE
	if !haveState {
		flags |= os.O_TRUNC
		_ = os.Remove(dlStatePath(t.spec.Path))
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to the download folder: %v", err)
	}
	t.file = f
	if t.info.Size > 0 && t.info.Ranges {
		if !haveState {
			if err := f.Truncate(t.info.Size); err != nil {
				return fmt.Errorf("not enough space or no permission to create the file: %v", err)
			}
		}
		ps := dlPieceSize(t.info.Size)
		if haveState {
			ps = st.PieceSize
		}
		t.pieceSize = ps
		for s := int64(0); s < t.info.Size; s += ps {
			e := s + ps - 1
			if e >= t.info.Size {
				e = t.info.Size - 1
			}
			p := &dlPiece{start: s, end: e}
			t.pieces = append(t.pieces, p)
		}
		if haveState && len(st.Have) == len(t.pieces) {
			for i, h := range st.Have {
				if h > 0 && h <= t.pieces[i].size() {
					t.pieces[i].have = h
					t.pieces[i].done = h == t.pieces[i].size()
				}
			}
		}
	}
	return nil
}

func (t *dlTask) allDone() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range t.pieces {
		if !p.done {
			return false
		}
	}
	return true
}

func (t *dlTask) saveState() {
	if t.stream || t.file == nil || len(t.pieces) == 0 {
		return
	}
	t.mu.Lock()
	have := make([]int64, len(t.pieces))
	for i, p := range t.pieces {
		have[i] = p.have
	}
	t.mu.Unlock()
	_ = t.file.Sync() // data first, then the note that says it is there
	st := dlState{V: 1, URL: t.spec.URL, FinalURL: t.info.FinalURL, Name: t.info.Name, Size: t.info.Size, ETag: t.info.ETag,
		LastMod: t.info.LastMod, PieceSize: t.pieceSize, Have: have}
	b, _ := json.Marshal(st)
	_ = dlWriteFileAtomic(dlStatePath(t.spec.Path), b)
	atomic.StoreInt32(&t.dirty, 0)
}

// finish moves the partial file to its final name.
func (t *dlTask) finish() error {
	if t.file != nil {
		if err := t.file.Sync(); err != nil {
			return err
		}
		if fi, err := t.file.Stat(); err == nil && t.info.Size >= 0 && fi.Size() != t.info.Size {
			return fmt.Errorf("the file has the wrong size (%d instead of %d)", fi.Size(), t.info.Size)
		}
		t.file.Close()
		t.file = nil
	}
	final := t.spec.Path
	if _, err := os.Stat(final); err == nil && t.spec.Overwrite {
		_ = os.Remove(final)
	}
	if _, err := os.Stat(final); err == nil {
		final = dlUniquePath(filepathDir(final), filepathBase(final), func(p string) bool { _, e := os.Stat(p); return e == nil })
	}
	var rerr error
	for try := 0; try < 12; try++ { // a virus scanner may hold the new file for a moment
		if rerr = os.Rename(dlPartPath(t.spec.Path), final); rerr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond) // (t.ctx is already cancelled here: all pieces are done)
	}
	if rerr != nil {
		return fmt.Errorf("could not finish the file: %v", rerr)
	}
	_ = os.Remove(dlStatePath(t.spec.Path))
	hMark(final, t.spec.URL)
	t.mu.Lock()
	t.finalPath = final
	t.mu.Unlock()
	return nil
}

var dlFreeSpace = hFree // replaced in tests

// dlPieceSize: small pieces for small files, bigger ones for huge files so there are never more than a few thousand.
func dlPieceSize(size int64) int64 {
	ps := size / 48
	if min := (size + 4095) / 4096; ps < min {
		ps = min
	}
	ps = (ps + 65535) &^ 65535
	if ps < dlMinPiece {
		ps = dlMinPiece
	}
	if ps > dlMaxPiece && size/dlMaxPiece < 4096 {
		ps = dlMaxPiece
	}
	return ps
}

// ---- scheduling ----------------------------------------------------------------------------------------------------

func (t *dlTask) supervise() {
	t.syncLines()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				hLog("download manager: recovered from a crash in supervisor: %v", r)
			}
		}()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		n := 0
		for {
			select {
			case <-t.ctx.Done():
				return
			case <-t.doneCh:
				return
			case <-tick.C:
			}
			n++
			t.syncLines()
			t.measure()
			t.checkRefused()
			if t.noLines() {
				if t.idleSince.IsZero() {
					t.idleSince = time.Now()
				} else if time.Since(t.idleSince) > dlNoLineGrace {
					t.fail(errors.New("no connection is available right now"))
				}
			} else {
				t.idleSince = time.Time{}
			}
			t.replaceSlow()
			t.grow()
			if atomic.LoadInt32(&t.dirty) != 0 && n%2 == 0 {
				t.saveState()
			}
			if t.allDone() {
				t.doneOnce.Do(func() { close(t.doneCh) })
				return
			}
		}
	}()
	if t.allDone() {
		t.doneOnce.Do(func() { close(t.doneCh) })
		t.cancelAll()
	}
}

// syncLines adds lines that appeared, stops lines that went down, brings back lines that returned.
func (t *dlTask) syncLines() {
	all := t.env.Lines()
	t.mu.Lock()
	defer t.mu.Unlock()
	var usable []string
	seen := map[string]bool{}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Rate > all[j].Rate })
	for _, l := range all {
		if !t.allowed(l.Name) {
			continue
		}
		seen[l.Name] = true
		alive := l.Alive == nil || l.Alive()
		r := t.runs[l.Name]
		if r == nil {
			r = &dlRun{line: l, client: dlLineClient(l, t.private), max: t.spec.MaxPerLine}
			if dlIsFTP(t.spec.URL) && r.max > 4 { // FTP servers allow few logins at once
				r.max = 4
			}
			r.ctx, r.cancel = context.WithCancel(t.ctx)
			r.target = dlStartConn
			if t.info.Size >= 32<<20 && t.info.Ranges { // a big file: open as many connections at once as a download manager would
				r.target = dlStartBig
				if r.target > r.max {
					r.target = r.max
				}
				if per := 24 / len(all); per < r.target { // never more than about 24 connections at the start
					r.target = maxInt(per, dlStartConn)
				}
			}
			if r.target > r.max {
				r.target = r.max
			}
			t.runs[l.Name] = r
			t.order = append(t.order, l.Name)
		}
		if !alive && !r.dead {
			r.dead = true
			r.cancel()
		}
		if alive && r.dead {
			r.dead = false
			r.ctx, r.cancel = context.WithCancel(t.ctx)
			r.active = 0
			r.cool = time.Time{}
		}
		if alive && !r.rejected {
			usable = append(usable, l.Name)
		}
	}
	for n, r := range t.runs {
		if !seen[n] && !r.dead {
			r.dead = true
			r.cancel()
		}
	}
	// one-line mode: only the first usable line works, the others wait (and take over if it goes down)
	primary := ""
	if t.single && len(usable) > 0 {
		primary = usable[0]
	}
	for _, n := range t.order {
		r := t.runs[n]
		want := 0
		if !r.dead && !r.rejected && (primary == "" || n == primary) {
			want = r.target
		}
		if !t.single && !r.dead && !r.rejected {
			want = r.target
		}
		for r.active < want && !t.finishedLocked() {
			r.active++
			t.wg.Add(1)
			goSafe("worker", func() { t.worker(r) })
		}
	}
}

// noLines: no line is able to carry this download right now.
func (t *dlTask) noLines() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.runs {
		if !r.dead && !r.rejected {
			return false
		}
	}
	return true
}

func (t *dlTask) finishedLocked() bool {
	for _, p := range t.pieces {
		if !p.done {
			return false
		}
	}
	return true
}

func (t *dlTask) measure() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.runs {
		b := atomic.LoadInt64(&r.bytes)
		cur := float64(b - r.prev)
		r.prev = b
		if r.speed == 0 {
			r.speed = cur
		} else {
			r.speed = r.speed*0.5 + cur*0.5
		}
	}
}

// grow adds a connection to a line while it keeps getting faster; takes one away when more connections made it slower.
func (t *dlTask) grow() {
	t.mu.Lock()
	defer t.mu.Unlock()
	total := 0
	for _, r := range t.runs {
		total += r.target
	}
	for _, r := range t.runs {
		if r.dead || r.rejected {
			continue
		}
		sp := r.speed
		switch {
		case r.target < r.max && total < 24 && sp > 0 && (r.lastTgt == 0 || r.target <= r.lastTgt) && sp > r.lastSp*1.10:
			r.target++
			total++
			if r.target < r.max && total < 24 && sp > r.lastSp*1.5 { // clearly still climbing: two at a time
				r.target++
				total++
			}
		case r.target > 2 && r.lastTgt > 0 && r.target > r.lastTgt && sp < r.lastSp*0.85:
			r.target--
			total--
		}
		r.lastSp, r.lastTgt = sp, r.target
	}
}

// next gives a worker its piece: the first piece nobody has, else (near the end) a second attempt at a slow piece.
func (t *dlTask) next(r *dlRun) (*dlPiece, *dlAtt, context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r.dead || r.rejected || t.fatal != nil || time.Now().Before(r.cool) || r.ctx.Err() != nil {
		return nil, nil, nil
	}
	var pick *dlPiece
	for _, p := range t.pieces {
		if !p.done && len(p.atts) == 0 {
			pick = p
			break
		}
	}
	if pick == nil { // end game: help with the biggest piece still running on another line
		var best int64
		for _, p := range t.pieces {
			if p.done || len(p.atts) != 1 || p.atts[0].line == r.line.Name {
				continue
			}
			if rem := p.size() - p.have; rem > 256<<10 && rem > best {
				best, pick = rem, p
			}
		}
	}
	if pick == nil {
		return nil, nil, nil
	}
	ctx, cancel := context.WithCancel(r.ctx)
	at := &dlAtt{line: r.line.Name, cancel: cancel, since: time.Now()}
	pick.atts = append(pick.atts, at)
	return pick, at, ctx
}

func (t *dlTask) release(p *dlPiece, at *dlAtt) {
	t.mu.Lock()
	for i, a := range p.atts {
		if a == at {
			p.atts = append(p.atts[:i], p.atts[i+1:]...)
			break
		}
	}
	t.mu.Unlock()
	at.cancel()
}

func (t *dlTask) worker(r *dlRun) {
	defer t.wg.Done()
	defer func() { t.mu.Lock(); r.active--; t.mu.Unlock() }()
	back := 0
	for {
		if t.ctx.Err() != nil {
			return
		}
		t.mu.Lock()
		over := r.active > r.target || r.rejected || r.dead || t.finishedLocked() || t.fatal != nil
		primaryOnly := false
		if t.single {
			for _, n := range t.order {
				x := t.runs[n]
				if !x.dead && !x.rejected {
					primaryOnly = x != r
					break
				}
			}
		}
		t.mu.Unlock()
		if over || primaryOnly {
			return
		}
		p, at, ctx := t.next(r)
		if p == nil {
			select {
			case <-t.ctx.Done():
				return
			case <-r.ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			continue
		}
		err := t.fetch(ctx, r, p, at)
		lost := ctx.Err() != nil // asked to stop by the other attempt at this piece (checked before release cancels it)
		t.release(p, at)
		if err == nil {
			back = 0
			continue
		}
		if t.ctx.Err() != nil || r.ctx.Err() != nil {
			return
		}
		if lost { // a duplicate attempt lost the race
			continue
		}
		if errors.Is(err, errDlChanged) {
			t.fail(err)
			return
		}
		wait := time.Duration(0)
		var he dlHTTPErr
		if errors.As(err, &he) {
			switch he.Code {
			case 401, 403, 404, 410, 451:
				if t.handleReject(r, he) {
					return
				}
				t.mu.Lock()
				r.refusals++
				if r.refusals >= 3 && r.okPieces == 0 {
					r.cool = time.Now().Add(5 * time.Second)
				}
				t.mu.Unlock()
				select {
				case <-t.ctx.Done():
					return
				case <-time.After(400 * time.Millisecond):
				}
				continue
			case 416:
				t.fail(errDlChanged)
				return
			case 429, 503:
				t.mu.Lock()
				if r.target > 1 {
					r.target--
				}
				r.max = r.target
				t.mu.Unlock()
				wait = he.Retry
				if wait == 0 {
					wait = 2 * time.Second
				}
			}
		}
		t.mu.Lock()
		failed := t.bumpFail(p)
		r.errs++
		if r.errs >= 4 {
			r.cool = time.Now().Add(8 * time.Second)
			r.errs = 0
		}
		t.mu.Unlock()
		if failed >= dlMaxFails {
			t.fail(fmt.Errorf("part of the file could not be downloaded: %s", dlFmtErr(err)))
			return
		}
		if wait == 0 {
			back++
			wait = time.Duration(300<<uint(minInt(back, 5))) * time.Millisecond
		}
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (t *dlTask) bumpFail(p *dlPiece) int { // call with t.mu held
	p.fails++
	return p.fails
}

// handleReject: a line that is refused while another line was accepted is left out of this download (the server ties
// the link to one address). Returns true if the calling worker should stop.
func (t *dlTask) handleReject(r *dlRun, he dlHTTPErr) bool {
	t.mu.Lock()
	t.lastRefuse = he
	t.mu.Unlock()
	return t.checkRefused()
}

// checkRefused leaves out lines that are refused while others work, and gives up when every line is refused.
func (t *dlTask) checkRefused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	anyOK := false
	for _, x := range t.runs {
		if x.okPieces > 0 {
			anyOK = true
		}
	}
	stop := false
	if anyOK {
		for _, x := range t.runs {
			if x.okPieces == 0 && x.refusals >= 2 && !x.rejected && !x.dead { // refused more than once, while another line works
				x.rejected = true
				x.cancel()
				t.note = "A line was left out: this site only accepts the file from one address"
				goSafe("lock", func() { t.env.LockHost(dlHostOf(t.spec.URL)) })
				stop = true
			}
		}
		return stop
	}
	all, any := true, false
	for _, x := range t.runs {
		if x.dead {
			continue
		}
		any = true
		if x.refusals < 3 {
			all = false
		}
	}
	if any && all {
		goSafe("fail", func() { t.fail(t.lastRefuse) })
	}
	return false
}

// fetch downloads (the rest of) one piece on one line.
func (t *dlTask) fetch(ctx context.Context, r *dlRun, p *dlPiece, at *dlAtt) error {
	t.mu.Lock()
	from := p.start + p.have
	end := p.end
	final := t.info.FinalURL
	t.mu.Unlock()
	if from > end {
		t.markDone(p)
		return nil
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled int32
	wd := time.AfterFunc(dlStall, func() { atomic.StoreInt32(&stalled, 1); cancel() })
	defer wd.Stop()

	body, err := t.openBody(rctx, r, final, from, end, &stalled)
	if err != nil {
		return err
	}
	defer body.Close()

	// Bytes are read straight into a pooled buffer; a second goroutine writes full buffers to the disk while the
	// network read carries on into the next one, so a slow disk (or a virus scanner) never stalls the connection.
	w := newDlWriter(t, p, from)
	began := time.Now()
	want := end - from + 1
	var got int64
	var rerr error
	for got < want {
		b := w.cur()
		free := b[len(b):cap(b)]
		if int64(len(free)) > want-got {
			free = free[:want-got]
		}
		n, e := body.Read(free)
		if n > 0 {
			wd.Reset(dlStall)
			if err := t.limit(rctx, r, n); err != nil {
				w.add(n)
				_ = w.close()
				return err
			}
			atomic.AddInt64(&r.bytes, int64(n))
			atomic.AddInt64(&at.bytes, int64(n))
			got += int64(n)
			if err := w.add(n); err != nil {
				_ = w.close()
				t.fail(err)
				return err
			}
		}
		if e != nil {
			rerr = e
			break
		}
	}
	if err := w.close(); err != nil {
		t.fail(err)
		return err
	}
	if got < want {
		if atomic.LoadInt32(&stalled) == 1 {
			return errors.New("the connection stopped sending data")
		}
		if rerr == io.EOF || rerr == nil {
			return io.ErrUnexpectedEOF
		}
		return rerr
	}
	t.mu.Lock()
	r.okPieces++
	r.errs = 0
	if d := time.Since(began); d > 100*time.Millisecond {
		sp := float64(got) / d.Seconds()
		r.refSp = math.Max(sp, r.refSp*0.97)
	}
	t.mu.Unlock()
	t.markDone(p)
	return nil
}

// openBody asks the server for bytes from..end of the file and returns the stream of them.
func (t *dlTask) openBody(rctx context.Context, r *dlRun, final string, from, end int64, stalled *int32) (io.ReadCloser, error) {
	if dlIsFTP(final) {
		b, err := t.ftpOpenRange(rctx, r, from, end-from+1)
		if err != nil && atomic.LoadInt32(stalled) == 1 {
			return nil, errors.New("the connection stopped sending data")
		}
		return b, err
	}
	req, err := dlRequest(rctx, final, t.hdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, end))
	if strings.HasPrefix(t.info.ETag, `"`) {
		req.Header.Set("If-Range", t.info.ETag)
	} else if t.info.LastMod != "" && t.info.ETag == "" {
		req.Header.Set("If-Range", t.info.LastMod)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		if atomic.LoadInt32(stalled) == 1 {
			return nil, errors.New("the connection stopped sending data")
		}
		return nil, err
	}
	fail := func(err error) (io.ReadCloser, error) { _ = resp.Body.Close(); return nil, err }
	switch resp.StatusCode {
	case 206:
		s, _, total, ok := dlParseRange(resp.Header.Get("Content-Range"))
		if !ok || s != from {
			return fail(errors.New("the server answered with the wrong part of the file"))
		}
		if total > 0 && total != t.info.Size {
			return fail(errDlChanged)
		}
		if et := resp.Header.Get("ETag"); et != "" && t.info.ETag != "" && et != t.info.ETag {
			return fail(errDlChanged)
		}
	case 200:
		// asked for a range, got everything: the file changed (If-Range) or this address is treated differently
		if et := resp.Header.Get("ETag"); et != "" && t.info.ETag != "" && et != t.info.ETag {
			return fail(errDlChanged)
		}
		return fail(dlHTTPErr{Code: 403})
	default:
		return fail(dlHTTPErr{Code: resp.StatusCode, Retry: dlRetryAfter(resp)})
	}

	return resp.Body, nil
}

func (t *dlTask) limit(ctx context.Context, r *dlRun, n int) error {
	if err := t.env.Limit(ctx, r.line.Name, n); err != nil {
		return err
	}
	return t.bucket.wait(ctx, n)
}

func (t *dlTask) markDone(p *dlPiece) {
	t.mu.Lock()
	if p.done {
		t.mu.Unlock()
		return
	}
	p.done = true
	p.have = p.size()
	for _, a := range p.atts {
		a.cancel()
	}
	all := t.finishedLocked()
	t.mu.Unlock()
	atomic.StoreInt32(&t.dirty, 1)
	if all {
		t.doneOnce.Do(func() { close(t.doneCh) })
		t.cancelAll()
	}
}

// ---- single stream (no ranges) ---------------------------------------------------------------------------------------

func (t *dlTask) runStream() error {
	var last error
	for try := 0; try < 4; try++ {
		lines := t.usableLines()
		if len(lines) == 0 {
			return errors.New("no connection is available right now")
		}
		l := lines[try%len(lines)]
		run := &dlRun{line: l, client: dlLineClient(l, t.private), max: 1, target: 1}
		run.ctx = t.ctx
		t.mu.Lock()
		t.runs = map[string]*dlRun{l.Name: run}
		t.order = []string{l.Name}
		run.active = 1
		t.streamN = 0
		t.mu.Unlock()
		goSafe("speed", func() { t.speedLoop(run) })
		err := t.streamOnce(run)
		t.mu.Lock()
		run.active = 0
		t.mu.Unlock()
		if err == nil {
			return nil
		}
		if t.ctx.Err() != nil {
			return t.ctx.Err()
		}
		last = err
		var he dlHTTPErr
		if errors.As(err, &he) && (he.Code == 404 || he.Code == 410 || he.Code == 401) {
			return err
		}
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		case <-time.After(time.Duration(1+try) * time.Second):
		}
	}
	return last
}

func (t *dlTask) speedLoop(r *dlRun) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-tick.C:
			t.mu.Lock()
			if r.active == 0 {
				t.mu.Unlock()
				return
			}
			b := atomic.LoadInt64(&r.bytes)
			cur := float64(b - r.prev)
			r.prev = b
			r.speed = r.speed*0.5 + cur*0.5
			t.mu.Unlock()
		}
	}
}

func (t *dlTask) streamOnce(r *dlRun) error {
	if err := t.file.Truncate(0); err != nil {
		return err
	}
	rctx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	var stalled int32
	wd := time.AfterFunc(dlStall, func() { atomic.StoreInt32(&stalled, 1); cancel() })
	defer wd.Stop()
	var body io.ReadCloser
	if dlIsFTP(t.info.FinalURL) {
		b, err := t.ftpOpenRange(rctx, r, 0, -1)
		if err != nil {
			return err
		}
		body = b
	} else {
		req, err := dlRequest(rctx, t.info.FinalURL, t.hdr)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			return dlHTTPErr{Code: resp.StatusCode, Retry: dlRetryAfter(resp)}
		}
		body = resp.Body
	}
	defer body.Close()
	buf := make([]byte, 64<<10)
	var off int64
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			wd.Reset(dlStall)
			if err := t.limit(rctx, r, n); err != nil {
				return err
			}
			if _, err := t.file.WriteAt(buf[:n], off); err != nil {
				t.fail(fmt.Errorf("could not write to disk: %v", err))
				return err
			}
			off += int64(n)
			atomic.AddInt64(&r.bytes, int64(n))
			atomic.StoreInt64(&t.streamN, off)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if atomic.LoadInt32(&stalled) == 1 {
				return errors.New("the connection stopped sending data")
			}
			return rerr
		}
	}
	if t.info.Size >= 0 && off != t.info.Size {
		return io.ErrUnexpectedEOF
	}
	t.info.Size = off
	return nil
}

// ---- progress ------------------------------------------------------------------------------------------------------

func (t *dlTask) Progress() dlProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	pr := dlProgress{Total: t.info.Size, Note: t.note}
	if t.hls != nil {
		pr.Done, pr.Total = t.hls.progress()
		if pv := atomic.LoadInt64(&t.hlsPrev); pv > 0 {
			pr.Done += pv
			pr.Total += pv
		}
	} else if t.stream {
		pr.Done = atomic.LoadInt64(&t.streamN)
	} else {
		for _, p := range t.pieces {
			pr.Done += p.have
		}
		pr.Map = dlPieceMap(t.pieces, 80)
	}
	for _, n := range t.order {
		r := t.runs[n]
		st := dlLineStat{Name: n, Bytes: atomic.LoadInt64(&r.bytes), Workers: r.active, Speed: r.speed}
		switch {
		case r.dead:
			st.Status, st.Speed = "down", 0
		case r.rejected:
			st.Status, st.Speed = "left out", 0
		case r.active > 0 && r.speed > 0:
			st.Status = "active"
		default:
			st.Status = "idle"
		}
		pr.Speed += st.Speed
		pr.Lines = append(pr.Lines, st)
	}
	return pr
}

func (t *dlTask) FinalPath() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finalPath
}

// ---- writing to the disk without making the network wait ------------------------------------------------------------

const dlWriteBuf = 512 << 10

var dlBufs = sync.Pool{New: func() any { b := make([]byte, 0, dlWriteBuf); return &b }}

type dlWriter struct {
	t    *dlTask
	p    *dlPiece
	off  int64 // where the next buffer goes in the file
	b    []byte
	bp   *[]byte
	ch   chan *[]byte
	done chan struct{}
	err  atomic.Value // error
}

func newDlWriter(t *dlTask, p *dlPiece, from int64) *dlWriter {
	w := &dlWriter{t: t, p: p, off: from, ch: make(chan *[]byte, 2), done: make(chan struct{})}
	w.take()
	go w.run()
	return w
}

func (w *dlWriter) take() {
	w.bp = dlBufs.Get().(*[]byte)
	w.b = (*w.bp)[:0]
}

// cur is the buffer being filled; the caller reads into its free space and then calls add(n).
func (w *dlWriter) cur() []byte { return w.b }

func (w *dlWriter) add(n int) error {
	w.b = w.b[:len(w.b)+n]
	if e, _ := w.err.Load().(error); e != nil {
		return e
	}
	if len(w.b) >= cap(w.b)-4096 || (len(w.b) >= 64<<10 && len(w.ch) == 0) {
		w.flush()
	}
	return nil
}

// flush hands the filled buffer to the writer and takes a fresh one.
func (w *dlWriter) flush() {
	if len(w.b) == 0 {
		return
	}
	*w.bp = w.b
	w.ch <- w.bp
	w.take()
}

func (w *dlWriter) run() {
	defer close(w.done)
	for bp := range w.ch {
		b := *bp
		if e, _ := w.err.Load().(error); e == nil {
			if _, err := w.t.file.WriteAt(b, w.off); err != nil {
				w.err.Store(fmt.Errorf("could not write to disk: %v", err))
			} else {
				w.off += int64(len(b))
				w.t.mu.Lock()
				if h := w.off - w.p.start; h > w.p.have {
					w.p.have = h
				}
				w.t.mu.Unlock()
				atomic.StoreInt32(&w.t.dirty, 1)
			}
		}
		*bp = b[:0]
		dlBufs.Put(bp)
	}
}

// close writes what is left and waits for the writer. It returns the first write error.
func (w *dlWriter) close() error {
	w.flush()
	close(w.ch)
	<-w.done
	if w.bp != nil {
		*w.bp = w.b[:0]
		dlBufs.Put(w.bp)
		w.bp = nil
	}
	e, _ := w.err.Load().(error)
	return e
}

// ---- replacing a connection that is far slower than the others on its line -------------------------------------------

// replaceSlow cancels one attempt that has been running for a while at a small fraction of what its siblings on the
// same line get. Servers and CDNs often throttle single connections; the cancelled piece continues where it stopped,
// on a new connection that is usually faster.
func (t *dlTask) replaceSlow() {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	byLine := map[string][]*dlAtt{}
	owner := map[*dlAtt]*dlPiece{}
	for _, p := range t.pieces {
		if p.done {
			continue
		}
		for _, a := range p.atts {
			b := atomic.LoadInt64(&a.bytes)
			cur := float64(b - a.prev)
			a.prev = b
			a.speed = a.speed*0.5 + cur*0.5
			byLine[a.line] = append(byLine[a.line], a)
			owner[a] = p
		}
	}
	for ln, as := range byLine {
		r := t.runs[ln]
		if r == nil {
			continue
		}
		avg := r.refSp     // what a good connection of this line gets (from pieces that finished)
		if avg < 512<<10 { // the line itself is slow, or nothing has finished yet: nothing to compare against
			continue
		}
		cut := 0
		for _, a := range as {
			p := owner[a]
			if cut < (len(as)+1)/2 && now.Sub(a.since) > 2*time.Second && a.speed < avg/8 && p.size()-p.have > 512<<10 && p.cuts < 2 && !a.slow {
				p.cuts++
				a.slow = true
				a.cancel()
				cut++
			}
		}
	}
}
