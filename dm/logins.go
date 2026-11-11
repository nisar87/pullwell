package dm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ---- saved logins: a user name and password kept per website --------------------------------------------------------
//
// A login saved for example.com is used for every download from example.com and its subdomains that comes without a
// login of its own, and is only ever sent to that site (see authScopeKey). The password is stored sealed (on Windows
// with the current user's key, see secret_windows.go) in data/logins.json, and is never given back to the window.

type dlLoginRec struct {
	Host   string `json:"host"`
	User   string `json:"user"`
	Secret string `json:"secret"`
}

type dlLoginView struct {
	Host string `json:"host"`
	User string `json:"user"`
}

type dlLogins struct {
	mu   sync.Mutex
	path string
	recs []dlLoginRec
	done bool
}

func loginsFile() string { return filepath.Join(hRoot(), "data", "logins.json") }

// authScopeKey is a header that is never sent: it names the site a login belongs to, so the engine does not give it
// to a mirror on another site.
const authScopeKey = "X-Auth-Scope"

func (s *dlLogins) load() {
	if p := loginsFile(); s.done && s.path == p {
		return
	}
	s.done, s.recs = true, nil
	s.path = loginsFile()
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var d struct {
		Logins []dlLoginRec `json:"logins"`
	}
	if json.Unmarshal(b, &d) != nil {
		hLog("logins.json could not be read - starting with no saved logins")
		_ = os.Rename(s.path, s.path+".bad")
		return
	}
	s.recs = d.Logins
}

func (s *dlLogins) save() error {
	b, _ := json.MarshalIndent(map[string]any{"logins": s.recs}, "", " ")
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.path+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(s.path+".tmp", s.path)
}

// dlLoginHost reads a site name from what the person typed: "example.com", "https://www.example.com/files" or "Example.com:8080".
func dlLoginHost(in string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(in))
	if strings.Contains(h, "://") {
		u, err := url.Parse(h)
		if err != nil {
			return "", errors.New("that is not a website address")
		}
		h = u.Hostname()
	} else {
		if i := strings.IndexAny(h, "/?#"); i >= 0 {
			h = h[:i]
		}
		if i := strings.LastIndexByte(h, '@'); i >= 0 {
			h = h[i+1:]
		}
		if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
			h = h[:i]
		}
	}
	h = strings.Trim(h, ".[]")
	if h == "" || len(h) > 253 || strings.ContainsAny(h, " \t\r\n\\\"'<>") {
		return "", errors.New("type the website, for example example.com")
	}
	return h, nil
}

func (s *dlLogins) set(host, user, pass string) error {
	h, err := dlLoginHost(host)
	if err != nil {
		return err
	}
	if user == "" || len(user) > 200 || strings.ContainsAny(user, "\r\n:") {
		return errors.New("type the user name (it cannot contain a colon)")
	}
	if len(pass) > 500 || strings.ContainsAny(pass, "\r\n") {
		return errors.New("that password is not usable")
	}
	sealed, err := dlSeal([]byte(pass))
	if err != nil {
		return errors.New("the password could not be stored safely: " + err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	rec := dlLoginRec{Host: h, User: user, Secret: base64.StdEncoding.EncodeToString(sealed)}
	found := false
	for i := range s.recs {
		if s.recs[i].Host == h {
			s.recs[i], found = rec, true
		}
	}
	if !found {
		if len(s.recs) >= 500 {
			return errors.New("too many saved logins (500 at most)")
		}
		s.recs = append(s.recs, rec)
	}
	return s.save()
}

func (s *dlLogins) remove(host string) error {
	h, err := dlLoginHost(host)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	out := s.recs[:0:0]
	for _, r := range s.recs {
		if r.Host != h {
			out = append(out, r)
		}
	}
	s.recs = out
	return s.save()
}

func (s *dlLogins) list() []dlLoginView {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	out := []dlLoginView{}
	for _, r := range s.recs {
		out = append(out, dlLoginView{r.Host, r.User})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// hostCovers says whether a login saved for site covers host: the site itself or any of its subdomains.
func dlHostCovers(site, host string) bool {
	host = strings.ToLower(host)
	return host == site || strings.HasSuffix(host, "."+site)
}

// auth returns the Authorization value and the site it belongs to for a link, or "" when nothing is saved for it.
func (s *dlLogins) auth(link string) (header, site string) {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	best := -1
	for i, r := range s.recs {
		if dlHostCovers(r.Host, u.Hostname()) && (best < 0 || len(r.Host) > len(s.recs[best].Host)) {
			best = i // the most specific site wins
		}
	}
	if best < 0 {
		return "", ""
	}
	raw, err := base64.StdEncoding.DecodeString(s.recs[best].Secret)
	if err != nil {
		return "", ""
	}
	pass, err := dlUnseal(raw)
	if err != nil {
		hLog("a saved login for " + s.recs[best].Host + " could not be opened (saved by another Windows user?)")
		return "", ""
	}
	return dlBasicAuth(s.recs[best].User, string(pass)), s.recs[best].Host
}

var savedLogins dlLogins

// dlWithSavedLogin gives hdr the saved login for link, unless the link or the header already carries one.
func dlWithSavedLogin(link string, hdr map[string]string) map[string]string {
	for k := range hdr {
		if strings.EqualFold(k, "Authorization") {
			return hdr
		}
	}
	auth, site := savedLogins.auth(link)
	if auth == "" {
		return hdr
	}
	out := make(map[string]string, len(hdr)+2)
	for k, v := range hdr {
		out[k] = v
	}
	out["Authorization"], out[authScopeKey] = auth, site
	return out
}
