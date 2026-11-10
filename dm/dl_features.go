package dm

// IDM-style features around the download list: named queues with their own hours, folders by site and type,
// what to do when a file exists, links with a user name, mirrors, a changed link, a per-download log,
// link patterns, a page link grabber, export / import and a command to run after a download.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- queues ----------------------------------------------------------------------------------------------------------

// dlQueue is a named group of downloads with its own hours. A download with no queue belongs to the main queue,
// which follows the general settings.
type dlQueue struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	MaxActive int      `json:"maxActive"` // at the same time from this queue (0 = only the general limit)
	Window    dlWindow `json:"window"`    // run only between these hours
	Days      []int    `json:"days"`      // days it runs: 0 = Sunday ... 6 = Saturday (empty = every day)
	Paused    bool     `json:"paused"`
}

type dlSiteRule struct {
	Site string `json:"site"` // example.com also matches www.example.com
	Dir  string `json:"dir"`
}

type dlLogLine struct {
	T int64  `json:"t"`
	S string `json:"s"`
}

var dlCategories = []string{"Programs", "Archives", "Videos", "Music", "Documents", "Images", "Other"}

var dlDayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func (c *dlConfig) queue(id string) *dlQueue {
	if id == "" {
		return nil
	}
	for i := range c.Queues {
		if c.Queues[i].ID == id {
			return &c.Queues[i]
		}
	}
	return nil
}

// queueBlock says why the queue of e may not run right now ("" = it may). Call with m.mu held.
func (m *dlManager) queueBlock(e *dlEntry, now time.Time) string {
	q := m.cfg.queue(e.Queue)
	if q == nil {
		return ""
	}
	if q.Paused {
		return "Queue \"" + q.Name + "\" is paused"
	}
	if len(q.Days) > 0 {
		ok := false
		for _, d := range q.Days {
			if d == int(now.Weekday()) {
				ok = true
			}
		}
		if !ok {
			return "Queue \"" + q.Name + "\" does not run on " + dlDayNames[now.Weekday()]
		}
	}
	if q.Window.Enabled && !inWindow(now, q.Window.From, q.Window.To) {
		return "Waiting for the hours of queue \"" + q.Name + "\" (" + q.Window.From + " to " + q.Window.To + ")"
	}
	return ""
}

func dlFixQueues(in []dlQueue) []dlQueue {
	var out []dlQueue
	seen := map[string]bool{}
	for _, q := range in {
		q.Name = strings.TrimSpace(q.Name)
		if len(q.Name) > 40 {
			q.Name = q.Name[:40]
		}
		if q.Name == "" || len(out) >= 12 {
			continue
		}
		if q.ID == "" || seen[q.ID] || len(q.ID) > 24 {
			q.ID = "q" + randomHex(4)
		}
		seen[q.ID] = true
		if q.MaxActive < 0 || q.MaxActive > 10 {
			q.MaxActive = 0
		}
		if _, e := time.Parse("15:04", q.Window.From); e != nil {
			q.Window.Enabled, q.Window.From = false, "22:00"
		}
		if _, e := time.Parse("15:04", q.Window.To); e != nil {
			q.Window.Enabled, q.Window.To = false, "07:00"
		}
		var days []int
		have := map[int]bool{}
		for _, d := range q.Days {
			if d >= 0 && d <= 6 && !have[d] {
				have[d] = true
				days = append(days, d)
			}
		}
		sort.Ints(days)
		q.Days = days
		out = append(out, q)
	}
	return out
}

func dlFixRules(in []dlSiteRule) []dlSiteRule {
	var out []dlSiteRule
	for _, r := range in {
		s := strings.ToLower(strings.TrimSpace(r.Site))
		s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		s, _, _ = strings.Cut(s, "/")
		s = strings.TrimPrefix(s, "www.")
		if s == "" || strings.TrimSpace(r.Dir) == "" || len(out) >= 30 {
			continue
		}
		out = append(out, dlSiteRule{Site: s, Dir: strings.TrimSpace(r.Dir)})
	}
	return out
}

// dlRuleDir finds the folder for a site (the first matching rule), or "".
func (c *dlConfig) ruleDir(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	for _, r := range c.SiteRules {
		if h == r.Site || strings.HasSuffix(h, "."+r.Site) {
			return r.Dir
		}
	}
	return ""
}

// ---- the log of one download -----------------------------------------------------------------------------------------

// elog adds a line to the log of one download. Call with m.mu held.
func (m *dlManager) elog(e *dlEntry, format string, a ...any) {
	e.Log = append(e.Log, dlLogLine{T: time.Now().Unix(), S: fmt.Sprintf(format, a...)})
	if len(e.Log) > 60 {
		e.Log = append([]dlLogLine{}, e.Log[len(e.Log)-60:]...)
	}
}

// ---- mirrors and a changed link ------------------------------------------------------------------------------------------

// rotateMirror switches to the next address of the same file. Call with m.mu held. It reports whether it did.
func (m *dlManager) rotateMirror(e *dlEntry, why string) bool {
	if len(e.Mirrors) == 0 || e.mirrorTries >= 2*(len(e.Mirrors)+1) {
		return false
	}
	next := e.Mirrors[0]
	e.Mirrors = append(append([]string{}, e.Mirrors[1:]...), e.URL)
	m.elog(e, "%s: switching to the mirror %s", why, dlHostOf(next))
	e.URL = next
	e.mirrorTries++
	e.Retries, e.RetryAt, e.Err, e.Detail = 0, 0, "", ""
	e.State, e.Note = "queued", "Trying another address"
	return true
}

func dlCleanMirrors(in []string, self string) ([]string, error) {
	var out []string
	seen := map[string]bool{self: true}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		l, err := dlCleanURL(s)
		if err != nil {
			return nil, err
		}
		l, _ = dlSplitAuth(l)
		if !seen[l] && len(out) < 8 {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out, nil
}

func (m *dlManager) setMirrors(id string, urls []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.find(id)
	if e == nil {
		return errors.New("download not found")
	}
	ms, err := dlCleanMirrors(urls, e.URL)
	if err != nil {
		return err
	}
	e.Mirrors = ms
	m.elog(e, "Mirrors: %d", len(ms))
	m.dirty = true
	return nil
}

// relink gives a download a new address (the old one expired) and keeps what was already downloaded.
func (m *dlManager) relink(id, link string, hdr map[string]string) error {
	l, err := dlCleanURL(link)
	if err != nil {
		return err
	}
	l, auth := dlSplitAuth(l)
	if auth != "" {
		h := map[string]string{}
		for k, v := range hdr {
			h[k] = v
		}
		h["Authorization"] = auth
		hdr = h
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.find(id)
	if e == nil {
		return errors.New("download not found")
	}
	if e.State == "done" {
		return errors.New("this download is already finished")
	}
	if e.running {
		return errors.New("pause the download first, then change its link")
	}
	m.elog(e, "Link changed to %s", dlHostOf(l))
	e.URL = l
	if hdr != nil {
		e.Header = dlCleanHeader(hdr)
	}
	e.Retries, e.RetryAt, e.Err, e.Detail = 0, 0, "", ""
	if e.State == "error" {
		e.State = "queued"
	}
	m.dirty = true
	return nil
}

func (m *dlManager) edit(id string, f func(e *dlEntry) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.find(id)
	if e == nil {
		return errors.New("download not found")
	}
	if err := f(e); err != nil {
		return err
	}
	m.dirty = true
	return nil
}

func (m *dlManager) setQueue(id, queue string) error {
	return m.edit(id, func(e *dlEntry) error {
		if queue != "" && m.cfg.queue(queue) == nil {
			return errors.New("that queue does not exist")
		}
		e.Queue = queue
		m.elog(e, "Moved to queue %q", queueName(&m.cfg, queue))
		return nil
	})
}

func queueName(c *dlConfig, id string) string {
	if q := c.queue(id); q != nil {
		return q.Name
	}
	return "Main queue"
}

func (m *dlManager) setConns(id string, n int) error {
	return m.edit(id, func(e *dlEntry) error {
		if n < 0 || n > 16 {
			return errors.New("1 to 16 connections per line (0 = automatic)")
		}
		e.Conns = n
		m.elog(e, "Connections per line: %d", n)
		return nil
	})
}

// basicAuth makes the header for a link that needs a user name and password.
func dlBasicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// ---- link patterns: https://site/img[001-120].jpg ----------------------------------------------------------------------

var dlPatRe = regexp.MustCompile(`\[([0-9]+)-([0-9]+)(?::([0-9]+))?\]|\[([a-zA-Z])-([a-zA-Z])\]`)

// dlExpand turns a link with [1-20], [001-120], [1-100:5] or [a-f] into the list of links (at most 500).
func dlExpand(link string) ([]string, error) {
	out := []string{link}
	for guard := 0; guard < 4; guard++ {
		var next []string
		changed := false
		for _, s := range out {
			loc := dlPatRe.FindStringSubmatchIndex(s)
			if loc == nil {
				next = append(next, s)
				continue
			}
			changed = true
			vals, err := dlPatValues(s, loc)
			if err != nil {
				return nil, err
			}
			for _, v := range vals {
				next = append(next, s[:loc[0]]+v+s[loc[1]:])
				if len(next) > 500 {
					return nil, errors.New("the pattern makes more than 500 links")
				}
			}
		}
		out = next
		if !changed {
			break
		}
	}
	return out, nil
}

func dlPatValues(s string, loc []int) ([]string, error) {
	g := func(i int) string {
		if loc[2*i] < 0 {
			return ""
		}
		return s[loc[2*i]:loc[2*i+1]]
	}
	var vals []string
	if g(4) != "" { // letters
		a, b := g(4)[0], g(5)[0]
		if a > b {
			a, b = b, a
		}
		for c := a; c <= b; c++ {
			vals = append(vals, string(rune(c)))
		}
		return vals, nil
	}
	bad := errors.New("the pattern " + s[loc[0]:loc[1]] + " is not valid (or makes more than 500 links)")
	lo, e1 := strconv.ParseInt(g(1), 10, 64)
	hi, e2 := strconv.ParseInt(g(2), 10, 64)
	step := int64(1)
	var e3 error
	if g(3) != "" {
		step, e3 = strconv.ParseInt(g(3), 10, 64)
	}
	// numbers too big to parse are refused (they used to become the largest number, and counting up from there wrapped around forever)
	if e1 != nil || e2 != nil || e3 != nil || step < 1 || hi < lo || (hi-lo)/step >= 500 {
		return nil, bad
	}
	width := 0
	if len(g(1)) > 1 && g(1)[0] == '0' {
		width = len(g(1))
	}
	n := (hi - lo) / step // at most 499: counting by number of steps cannot overflow
	for i := int64(0); i <= n; i++ {
		vals = append(vals, fmt.Sprintf("%0*d", width, lo+i*step))
	}
	return vals, nil
}

// ---- export and import -----------------------------------------------------------------------------------------------

type dlExportItem struct {
	URL      string   `json:"url"`
	Name     string   `json:"name,omitempty"`
	Dir      string   `json:"dir,omitempty"`
	Queue    string   `json:"queue,omitempty"`
	Mirrors  []string `json:"mirrors,omitempty"`
	Hash     string   `json:"hash,omitempty"`
	Finished bool     `json:"finished,omitempty"`
}

type dlExportFile struct {
	App     string         `json:"app"`
	Version int            `json:"version"`
	Queues  []dlQueue      `json:"queues,omitempty"`
	Items   []dlExportItem `json:"items"`
}

// exportList is the whole list as a file the person can keep or move to another PC (no passwords or cookies).
func (m *dlManager) exportList() dlExportFile {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := dlExportFile{App: "netmerge-downloads", Version: 1, Queues: append([]dlQueue{}, m.cfg.Queues...)}
	for _, e := range m.items {
		it := dlExportItem{URL: e.URL, Name: e.Name, Queue: e.Queue, Mirrors: e.Mirrors, Hash: e.Hash, Finished: e.State == "done"}
		if e.Dir != "" {
			it.Dir = e.Dir
		}
		f.Items = append(f.Items, it)
	}
	return f
}

// parseImport reads either the export file or a plain list of links (one per line; lines starting with # are skipped).
func dlParseImport(text string) ([]dlExportItem, []dlQueue, error) {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\xef\xbb\xbf"))
	if text == "" {
		return nil, nil, errors.New("the file is empty")
	}
	if strings.HasPrefix(text, "{") {
		var f dlExportFile
		if err := json.Unmarshal([]byte(text), &f); err != nil || f.App != "netmerge-downloads" {
			return nil, nil, errors.New("that is not a NetMerge download list")
		}
		return f.Items, f.Queues, nil
	}
	var items []dlExportItem
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		if l := strings.ToLower(ln); strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "ftp://") || strings.HasPrefix(l, "magnet:?") {
			items = append(items, dlExportItem{URL: ln})
		}
	}
	if len(items) == 0 {
		return nil, nil, errors.New("no links found in that file")
	}
	return items, nil, nil
}

// ---- the page link grabber -------------------------------------------------------------------------------------------

type dlGrabbed struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Ext  string `json:"ext"`
}

var dlLinkRe = regexp.MustCompile(`(?is)<(?:a|source|img|video|audio|link|embed)\b[^>]*?\b(?:href|src)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

var dlNotFiles = map[string]bool{"": true, "html": true, "htm": true, "php": true, "asp": true, "aspx": true, "jsp": true, "css": true, "js": true, "json": true, "xml": true, "ico": true, "map": true}

// dlGrabLinks finds the links on a page. types are extensions without a dot ("zip", "mp4"); empty means every link
// that looks like a file.
func dlGrabLinks(base string, body []byte, types []string) []dlGrabbed {
	bu, err := url.Parse(base)
	if err != nil {
		return nil
	}
	want := map[string]bool{}
	for _, t := range types {
		t = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t), "."))
		if t != "" {
			want[t] = true
		}
	}
	seen := map[string]bool{}
	var out []dlGrabbed
	for _, m := range dlLinkRe.FindAllStringSubmatch(string(body), -1) {
		raw := strings.TrimSpace(m[1] + m[2] + m[3])
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(strings.ToLower(raw), "javascript:") || strings.HasPrefix(strings.ToLower(raw), "mailto:") || strings.HasPrefix(raw, "data:") {
			continue
		}
		raw = strings.ReplaceAll(raw, "&amp;", "&")
		if strings.HasPrefix(strings.ToLower(raw), "magnet:?") {
			if len(want) > 0 && !want["magnet"] {
				continue
			}
			if seen[raw] || len(raw) > 4000 {
				continue
			}
			seen[raw] = true
			name := "magnet link"
			if mu, err := url.Parse(raw); err == nil && mu.Query().Get("dn") != "" {
				name = mu.Query().Get("dn")
			}
			out = append(out, dlGrabbed{URL: raw, Name: name, Ext: "magnet"})
			continue
		}
		u, err := bu.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		u.Fragment = ""
		name := path.Base(u.Path)
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
		if len(want) > 0 {
			if !want[ext] {
				continue
			}
		} else if dlNotFiles[ext] || (len(ext) > 6 && ext != "torrent") {
			continue
		}
		l := u.String()
		if seen[l] {
			continue
		}
		seen[l] = true
		if name == "" || name == "/" || name == "." {
			name = u.Hostname()
		}
		out = append(out, dlGrabbed{URL: l, Name: name, Ext: ext})
		if len(out) >= 500 {
			break
		}
	}
	return out
}

func (m *dlManager) grab(page string, types []string) (map[string]any, error) {
	link, err := dlCleanURL(page)
	if err != nil {
		return nil, err
	}
	lines := m.lines()
	if len(lines) == 0 {
		return nil, errors.New("no internet connection found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req, err := dlRequest(ctx, link, nil)
	if err != nil {
		return nil, err
	}
	resp, err := dlLineClient(lines[0], true).Do(req)
	if err != nil {
		msg, _ := dlFriendly(err)
		return nil, errors.New(msg)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, dlHTTPErr{Code: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 3<<20))
	if err != nil {
		return nil, err
	}
	final := link
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	list := dlGrabLinks(final, b, types)
	return map[string]any{"links": list, "page": final}, nil
}

// ---- run a command after a download ------------------------------------------------------------------------------------

// dlSplitCommand splits "tool.exe --scan "%f"" into words (double quotes group words); %f becomes the file.
func dlSplitCommand(cmd, file string) []string {
	var out []string
	var cur strings.Builder
	in, has := false, false
	for _, r := range cmd {
		switch {
		case r == '"':
			in, has = !in, true
		case (r == ' ' || r == '\t') && !in:
			if has || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if has || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	for i := range out {
		out[i] = strings.ReplaceAll(out[i], "%f", file)
	}
	return out
}

func (m *dlManager) runAfter(e *dlEntry, cmd string) {
	if cmd == "" || H.Run == nil || e.Path == "" {
		return
	}
	args := dlSplitCommand(cmd, e.Path)
	if len(args) == 0 {
		return
	}
	id := e.ID
	goSafe("after", func() {
		err := H.Run(args)
		m.mu.Lock()
		defer m.mu.Unlock()
		if x := m.find(id); x != nil {
			if err != nil {
				m.elog(x, "The command after the download failed: %v", err)
			} else {
				m.elog(x, "The command after the download ran")
			}
			m.dirty = true
		}
	})
}

// dlPieceMap sums the progress of the pieces into at most n cells (0..100 each) for the segment bar.
func dlPieceMap(ps []*dlPiece, n int) []int {
	if len(ps) == 0 {
		return nil
	}
	if n > len(ps) {
		n = len(ps)
	}
	out := make([]int, n)
	for i := 0; i < n; i++ {
		a, b := i*len(ps)/n, (i+1)*len(ps)/n
		var have, size int64
		for _, p := range ps[a:b] {
			have += p.have
			size += p.size()
		}
		if size > 0 {
			out[i] = int(have * 100 / size)
		}
	}
	return out
}

var _ = os.Stat
var _ = filepath.Join

// importItems adds the entries of an export file (queues are created when missing, by name).
func (m *dlManager) importItems(items []dlExportItem, queues []dlQueue) (map[string]any, error) {
	if len(items) > 2000 {
		return nil, errors.New("too many entries in that file (2000 at most)")
	}
	qmap := map[string]string{} // id in the file -> id here
	m.mu.Lock()
	for _, q := range queues {
		have := ""
		for _, x := range m.cfg.Queues {
			if strings.EqualFold(x.Name, q.Name) {
				have = x.ID
			}
		}
		if have == "" && len(m.cfg.Queues) < 12 {
			nq := dlFixQueues([]dlQueue{{Name: q.Name, MaxActive: q.MaxActive, Window: q.Window, Days: q.Days}})
			if len(nq) == 1 {
				m.cfg.Queues = append(m.cfg.Queues, nq[0])
				have = nq[0].ID
			}
		}
		qmap[q.ID] = have
	}
	m.dirty = true
	m.mu.Unlock()
	added, skipped := 0, 0
	for _, it := range items {
		a := dlAdd{URL: it.URL, Name: it.Name, Mirrors: it.Mirrors, Hash: it.Hash, Queue: qmap[it.Queue]}
		if it.Dir != "" {
			a.Dir = it.Dir
		}
		if it.Finished { // a finished entry is only kept if the file is not there yet: add it again, paused
			f := false
			a.Start = &f
		}
		if _, err := m.add(a); err != nil {
			skipped++
			continue
		}
		added++
	}
	if added == 0 {
		return nil, errors.New("nothing could be added (the links are already in the list or not valid)")
	}
	return map[string]any{"imported": added, "skipped": skipped, "items": m.views(), "config": m.config()}, nil
}

// startQueue sets every paused or waiting download of a queue running ("" = the main queue).
func (m *dlManager) startQueue(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.items {
		if e.Queue == id && (e.State == "paused" || e.State == "error") && !e.running {
			e.State, e.Err, e.Retries, e.RetryAt = "queued", "", 0, 0
		}
	}
	m.dirty = true
}
