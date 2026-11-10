package main

// The pullwell:// link (design section 6.4). Toast buttons call the program back through it, but any web page can
// also try to open such a link, so it is treated as untrusted input: only five verbs exist, every action except
// "show" needs a one-time random token that the program itself put in a toast, and "open" never runs a program file.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	linkMaxLen     = 512
	linkTokenLife  = 24 * time.Hour
	linkMaxTokens  = 1000
	linkTokenBytes = 16
)

// appLink is a parsed pullwell:// link. Nothing in it is trusted yet.
type appLink struct {
	Verb   string // show, open, reveal, list, cancel-shutdown
	ItemID string
	Token  string
}

var linkVerbs = map[string]bool{"show": true, "open": true, "reveal": true, "list": true, "cancel-shutdown": true}

var errBadLink = errors.New("not a valid link")

// parseAppLink reads a link like pullwell://open?item=d1762741234-7&t=<32 hex>. Anything unexpected is an error.
func parseAppLink(raw string) (appLink, error) {
	if len(raw) > linkMaxLen {
		return appLink{}, errBadLink
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, appScheme) {
		return appLink{}, errBadLink
	}
	verb := u.Host
	if verb == "" { // pullwell:open?... (no slashes)
		verb = u.Opaque
	}
	verb = strings.ToLower(strings.Trim(verb+u.Path, "/")) // Windows sometimes adds a slash: pullwell://open/?item=
	if !linkVerbs[verb] {
		return appLink{}, errBadLink
	}
	q := u.Query()
	l := appLink{Verb: verb, ItemID: q.Get("item"), Token: q.Get("t")}
	if l.ItemID != "" && !validLinkPart(l.ItemID, 64) {
		return appLink{}, errBadLink
	}
	if l.Token != "" && (len(l.Token) != 2*linkTokenBytes || !validHex(l.Token)) {
		return appLink{}, errBadLink
	}
	return l, nil
}

func validLinkPart(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

// linkTokens remembers the tokens put into toasts. Memory only: a restart forgets them, which is safe (the link then
// only shows the window).
type linkTokens struct {
	mu   sync.Mutex
	now  func() time.Time
	list []linkGrant
}

type linkGrant struct {
	token   string
	itemID  string
	verbs   map[string]bool
	expires time.Time
}

func newLinkTokens(now func() time.Time) *linkTokens {
	if now == nil {
		now = time.Now
	}
	return &linkTokens{now: now}
}

// issue makes a new random token allowing the given verbs, for one item (itemID may be empty for "list" and
// "cancel-shutdown").
func (s *linkTokens) issue(itemID string, verbs ...string) (string, error) {
	b := make([]byte, linkTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	g := linkGrant{token: hex.EncodeToString(b), itemID: itemID, verbs: map[string]bool{}, expires: s.now().Add(linkTokenLife)}
	for _, v := range verbs {
		g.verbs[v] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExpiredLocked()
	if len(s.list) >= linkMaxTokens {
		s.list = s.list[1:] // oldest first
	}
	s.list = append(s.list, g)
	return g.token, nil
}

// allows says whether this link's token was issued for this verb and item and has not expired.
func (s *linkTokens) allows(l appLink) bool {
	if l.Token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExpiredLocked()
	for _, g := range s.list {
		if subtle.ConstantTimeCompare([]byte(g.token), []byte(l.Token)) == 1 {
			return g.verbs[l.Verb] && g.itemID == l.ItemID
		}
	}
	return false
}

func (s *linkTokens) dropExpiredLocked() {
	now := s.now()
	keep := s.list[:0]
	for _, g := range s.list {
		if now.Before(g.expires) {
			keep = append(keep, g)
		}
	}
	s.list = keep
}

// linkDecision is what the program does for a link.
type linkDecision struct {
	Do     string // "show", "open", "reveal", "list", "cancel-shutdown"
	ItemID string // item to select or act on; "" = none
	Why    string // for the log when the request was reduced to "show"
}

// decideLink turns a link into an action. fileName returns the downloaded file's name for an item ("" if the item
// does not exist or has no file).
func decideLink(l appLink, tokens *linkTokens, fileName func(itemID string) string) linkDecision {
	show := func(item, why string) linkDecision { return linkDecision{Do: "show", ItemID: item, Why: why} }
	if l.Verb == "show" { // harmless: brings up the window and selects the item if it exists
		return show(l.ItemID, "")
	}
	if !tokens.allows(l) {
		return show("", "link without a valid token: only the window is shown")
	}
	switch l.Verb {
	case "list", "cancel-shutdown":
		return linkDecision{Do: l.Verb}
	}
	name := fileName(l.ItemID)
	if name == "" {
		return show("", "the download no longer exists")
	}
	if l.Verb == "open" && isProgramFile(name) {
		return show(l.ItemID, "program files are never opened from a link; open it from the window")
	}
	return linkDecision{Do: l.Verb, ItemID: l.ItemID}
}

// programExts are file types Windows runs as programs or scripts when opened.
var programExts = map[string]bool{
	".exe": true, ".msi": true, ".msix": true, ".msixbundle": true, ".appx": true, ".appxbundle": true,
	".bat": true, ".cmd": true, ".com": true, ".scr": true, ".pif": true, ".cpl": true,
	".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true,
	".hta": true, ".lnk": true, ".url": true, ".reg": true, ".jar": true, ".msp": true, ".appref-ms": true,
	".application": true, ".chm": true, ".scf": true, ".inf": true,
}

func isProgramFile(name string) bool {
	// Windows ignores trailing dots and spaces, so "setup.exe. " is still a program.
	return programExts[strings.ToLower(filepath.Ext(strings.TrimRight(name, ". ")))]
}
