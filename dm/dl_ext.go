package dm

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// The browser extension talks to the program on the same local port as the window, under /ext/. Only extension pages may
// call it (a normal website is refused by its Origin), the Host header must be local, and every call except "ping"
// and "pair" needs a private token that the extension got by typing a one-time code shown in the program.

type extBrowser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hash     string `json:"hash"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"lastSeen"`
}

type extStore struct {
	mu       sync.Mutex
	path     string
	Browsers []extBrowser `json:"browsers"`
	MinMB    int          `json:"minMB"`
	Off      bool         `json:"off"`
	NoAsk    bool         `json:"noAsk"` // false (the default): ask before a download from the browser starts
	code     string
	codeExp  time.Time
	tries    int
	failures []time.Time
}

var ext = &extStore{MinMB: 5}

func extFile() string { return filepath.Join(hRoot(), "data", "extension.json") }

func (s *extStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = extFile()
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.MinMB < 0 || s.MinMB > 100000 {
		s.MinMB = 5
	}
}

func (s *extStore) saveLocked() {
	if s.path == "" {
		s.path = extFile()
	}
	b, _ := json.MarshalIndent(s, "", " ")
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	_ = dlWriteFileAtomicMode(s.path, b, 0o600)
}

func extHash(tok string) string { h := sha256.Sum256([]byte(tok)); return hex.EncodeToString(h[:]) }

// newCode makes a one-time code that is valid for two minutes (a new one replaces the old one).
func (s *extStore) newCode() (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := randomHex(5) // 10 hex letters, read out as groups of 5
	s.code, s.codeExp, s.tries = d, time.Now().Add(2*time.Minute), 0
	return d, s.codeExp
}

var errExtCode = errors.New("that code is wrong or has expired")

func (s *extStore) pair(code, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code = strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(code))
	if s.code == "" || time.Now().After(s.codeExp) {
		s.code = ""
		return "", errExtCode
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(s.code)) != 1 {
		s.tries++
		if s.tries >= 5 {
			s.code = "" // too many guesses: the code is dead
		}
		return "", errExtCode
	}
	s.code = ""
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 40 {
		name = "Browser"
	}
	if len(s.Browsers) >= 8 {
		return "", errors.New("too many browsers are paired; remove one first")
	}
	tok := randomHex(32)
	s.Browsers = append(s.Browsers, extBrowser{ID: randomHex(4), Name: name, Hash: extHash(tok), Created: time.Now().Unix()})
	s.saveLocked()
	return tok, nil
}

func (s *extStore) check(tok string) bool {
	if tok == "" {
		return false
	}
	h := extHash(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	// too many wrong tokens in a minute: slow down guessing
	keep := s.failures[:0]
	for _, t := range s.failures {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	s.failures = keep
	if len(s.failures) >= 20 {
		return false
	}
	for i := range s.Browsers {
		if subtle.ConstantTimeCompare([]byte(h), []byte(s.Browsers[i].Hash)) == 1 {
			s.Browsers[i].LastSeen = now.Unix()
			return true
		}
	}
	s.failures = append(s.failures, now)
	return false
}

func (s *extStore) revoke(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []extBrowser
	for _, b := range s.Browsers {
		if b.ID != id {
			keep = append(keep, b)
		}
	}
	s.Browsers = keep
	s.saveLocked()
}

func (s *extStore) setConfig(minMB int, off, noAsk bool) error {
	if minMB < 0 || minMB > 100000 {
		return errors.New("the minimum size must be between 0 and 100000 MB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MinMB, s.Off, s.NoAsk = minMB, off, noAsk
	s.saveLocked()
	return nil
}

func (s *extStore) view() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	bs := []map[string]any{}
	for _, b := range s.Browsers {
		bs = append(bs, map[string]any{"id": b.ID, "name": b.Name, "created": b.Created, "lastSeen": b.LastSeen})
	}
	return map[string]any{"browsers": bs, "minMB": s.MinMB, "enabled": !s.Off, "ask": !s.NoAsk, "asked": askedTotal.Load()}
}

func extOrigin(o string) bool {
	return strings.HasPrefix(o, "chrome-extension://") || strings.HasPrefix(o, "moz-extension://") || strings.HasPrefix(o, "safari-web-extension://")
}

func extHandler(auth bool, h http.HandlerFunc) http.HandlerFunc {
	return safeHandler(func(w http.ResponseWriter, r *http.Request) {
		if !Enabled() { // the manager is off: the browser extension sees a plain "not found"
			http.NotFound(w, r)
			return
		}
		o := r.Header.Get("Origin")
		if o != "" && !extOrigin(o) {
			writeErr(w, 403, "forbidden")
			return
		}
		if o != "" {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		if auth {
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ext.check(tok) {
				writeErr(w, 401, "not paired")
				return
			}
		}
		h(w, r)
	})
}

func registerExtRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ext/ping", extHandler(false, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"app": strings.ToLower(hName()), "version": H.Version})
	}))
	mux.HandleFunc("/ext/pair", extHandler(false, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "POST only")
			return
		}
		var in struct{ Code, Name string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		tok, err := ext.pair(in.Code, in.Name)
		if err != nil {
			writeErr(w, 403, err.Error())
			return
		}
		writeJSON(w, map[string]any{"token": tok})
	}))
	mux.HandleFunc("/ext/status", extHandler(true, func(w http.ResponseWriter, r *http.Request) {
		act, sp := dlMgr.summary()
		v := ext.view()
		writeJSON(w, map[string]any{"version": H.Version, "enabled": v["enabled"], "minMB": v["minMB"], "active": act, "speed": sp, "lines": len(dlMgr.lines())})
	}))
	mux.HandleFunc("/ext/unpair", extHandler(true, func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		h := extHash(tok)
		ext.mu.Lock()
		var id string
		for _, b := range ext.Browsers {
			if b.Hash == h {
				id = b.ID
			}
		}
		ext.mu.Unlock()
		if id != "" {
			ext.revoke(id)
		}
		writeJSON(w, map[string]any{"ok": true})
	}))
	mux.HandleFunc("/ext/add", extHandler(true, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "POST only")
			return
		}
		var in struct {
			URL       string `json:"url"`
			Referrer  string `json:"referrer"`
			Cookie    string `json:"cookie"`
			UserAgent string `json:"userAgent"`
			Filename  string `json:"filename"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in) != nil {
			writeErr(w, 400, "bad request")
			return
		}
		res, err := dlMgr.addFromBrowser(in.URL, in.Referrer, in.Cookie, in.UserAgent, in.Filename)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, res)
	}))
}

// addFromBrowser adds a link the browser was about to download. Local-network targets are refused.
func (m *dlManager) addFromBrowser(link, referrer, cookie, ua, name string) (map[string]any, error) {
	h := map[string]string{}
	if referrer != "" {
		h["Referer"] = referrer
	}
	if cookie != "" {
		h["Cookie"] = cookie
	}
	if ua != "" {
		h["User-Agent"] = ua
	}
	// Refuse a local-network target now, while the browser still has its own download: the extension cancels that
	// download as soon as we answer ok, and a link on an ordinary-looking name can resolve to an inside address.
	if err := dlRefuseLocalTarget(link); err != nil {
		return nil, err
	}
	// A site that refuses us even after the retries (a login wall, bot protection) would leave the person with a
	// dead "Download file info" box and a cancelled browser download. Ask the site once now, briefly; when it refuses,
	// say no, and the extension lets the browser carry on with its own download (it has the person's sign-in).
	if refused := m.refusedForUs(link, h); refused != "" {
		return nil, errors.New(refused)
	}
	if ext.asks() {
		// Like IDM: the person sees the "Download file info" box first (a wrong click costs nothing). The browser's own
		// download is cancelled once we answer ok, so the link waits here until the window takes it.
		queueAsk(extAsk{URL: link, Name: name, Header: h})
		if H.ShowWindow != nil {
			go H.ShowWindow()
		}
		return map[string]any{"ok": true, "asked": true}, nil
	}
	a := dlAdd{URL: link, Header: h, Name: name, FromWeb: true}
	it, err := m.add(a)
	if errors.Is(err, errDlDuplicate) {
		// The browser download is cancelled once we answer ok, so the person must be left with something that works:
		// a running or waiting copy is enough; a stopped or failed one is restarted; a finished one (the person asked
		// for the file again) becomes a new download, named by the file-exists rule.
		switch st := m.restartDuplicate(link); st {
		case "running":
			return map[string]any{"ok": true, "duplicate": true}, nil
		case "restarted":
			return map[string]any{"ok": true, "duplicate": true, "restarted": true}, nil
		default:
			a.Force = true
			it, err = m.add(a)
		}
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": it.ID}, nil
}

// refusedForUs returns a message when the site answers 401/403 to every way we ask; "" otherwise (including
// when the check itself could not run: no line, timeout, other errors - those are handled by the normal flow).
func (m *dlManager) refusedForUs(link string, h map[string]string) string {
	if dlIsFTP(link) || len(m.lines()) == 0 {
		return ""
	}
	l := m.lines()[0]
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pc := dlLineClient(l, dlLocalHost(dlHostOf(link)))
	defer pc.CloseIdleConnections()
	_, _, err := dlProbeReferer(ctx, pc, link, h)
	var he dlHTTPErr
	if errors.As(err, &he) && (he.Code == 401 || he.Code == 403) {
		msg, _ := dlFriendly(err)
		hLog("browser download %s refused by the site: %s", dlHostOf(link), msg)
		return msg
	}
	return ""
}

// restartDuplicate looks at the existing download of a link: "running" (queued or downloading), "restarted" (it was paused or
// had failed and is started again) or "finished"/"" (nothing to continue).
func (m *dlManager) restartDuplicate(link string) string {
	m.mu.Lock()
	var id, state string
	for _, o := range m.items {
		if o.URL == link {
			id, state = o.ID, o.State
		}
	}
	m.mu.Unlock()
	switch state {
	case "done":
		return "finished"
	case "paused", "error":
		if m.resume(id) == nil {
			return "restarted"
		}
	}
	return "running"
}

// dlLookupHost is the name lookup used to check a link from the browser (replaced in tests).
var dlLookupHost = func(ctx context.Context, host string) ([]net.IP, error) {
	as, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	var out []net.IP
	for _, a := range as {
		out = append(out, a.IP)
	}
	return out, err
}

// dlRefuseLocalTarget: an http(s) link from a web page must not lead into the local network, whether it names an inside
// address or a name that resolves to one. A name that cannot be looked up is left to the download itself to report.
func dlRefuseLocalTarget(link string) error {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ftp") || u.Hostname() == "" {
		return nil
	}
	host := u.Hostname()
	if dlLocalHost(host) {
		return errors.New("the link leads to an address inside your own network")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ips, err := dlLookupHost(ctx, host)
	if err != nil {
		return nil
	}
	for _, ip := range ips {
		if dlPrivateIP(ip) {
			return errors.New("the link leads to an address inside your own network")
		}
	}
	return nil
}

// extAsk is a browser download waiting for the person's yes: the window shows the "Download file info" box for it.
type extAsk struct {
	URL    string            `json:"url"`
	Name   string            `json:"name"`
	Header map[string]string `json:"header"`
}

// askedTotal counts the browser downloads that were held for the box (shown in the extension view, for tests).
var askedTotal atomic.Int64

var (
	askMu sync.Mutex
	askQ  []extAsk
)

func (s *extStore) asks() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.NoAsk
}

func queueAsk(a extAsk) {
	askedTotal.Add(1)
	askMu.Lock()
	defer askMu.Unlock()
	for _, o := range askQ {
		if o.URL == a.URL {
			return
		}
	}
	if len(askQ) < 50 {
		askQ = append(askQ, a)
	}
}

// takeAsk hands out the oldest waiting browser download, once.
func takeAsk() *extAsk {
	askMu.Lock()
	defer askMu.Unlock()
	if len(askQ) == 0 {
		return nil
	}
	a := askQ[0]
	askQ = askQ[1:]
	return &a
}
