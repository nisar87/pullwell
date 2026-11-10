package dm

// The download manager: the list of downloads, the queue, what is kept on disk, and the connection to the lines.

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type dlWindow struct {
	Enabled bool   `json:"enabled"`
	From    string `json:"from"` // HH:MM
	To      string `json:"to"`
}

type dlConfig struct {
	Dir           string             `json:"dir"`
	UseCategories bool               `json:"useCategories"`
	MaxActive     int                `json:"maxActive"`
	MaxPerLine    int                `json:"maxPerLine"`
	AllowMobile   bool               `json:"allowMobile"`
	LimitAll      float64            `json:"limitAll"`     // bytes/s for all downloads together, 0 = no limit
	LimitLines    map[string]float64 `json:"limitLines"`   // bytes/s per line
	ExcludeLines  []string           `json:"excludeLines"` // lines never used for downloads
	CallMode      string             `json:"callMode"`     // "avoid" (keep off the call's line) | "pause" | "off"
	Window        dlWindow           `json:"window"`       // only download between these hours
	AfterAll      string             `json:"afterAll"`     // when everything is finished: "" | "sleep" | "shutdown"
	Queues        []dlQueue          `json:"queues"`       // extra named queues with their own hours
	OnExists      string             `json:"onExists"`     // a file with that name is already there: "rename" | "overwrite" | "skip"
	CatDirs       map[string]string  `json:"catDirs"`      // a folder for a type (Videos, Music ...), instead of Dir/Type
	SiteRules     []dlSiteRule       `json:"siteRules"`    // a folder for a website
	NoNotify      bool               `json:"noNotify"`     // no desktop notification when a download finishes
	NoTorrents    bool               `json:"noTorrents"`   // refuse magnet links and .torrent files
	NoUpload      bool               `json:"noUpload"`     // torrents: do not upload while downloading
	UploadLimit   float64            `json:"uploadLimit"`  // torrents: upload speed limit in bytes/s (0 = none)
	AfterCmd      string             `json:"afterCmd"`     // run after each finished download (%f = the file), e.g. a virus scanner
}

type dlItem struct {
	ID       string            `json:"id"`
	URL      string            `json:"url"`
	Header   map[string]string `json:"header,omitempty"`
	Name     string            `json:"name"`
	Dir      string            `json:"dir"` // chosen by the person ("" = default)
	Path     string            `json:"path"`
	Category string            `json:"category"`
	Size     int64             `json:"size"`
	Done     int64             `json:"done"`
	State    string            `json:"state"` // queued | downloading | paused | done | error
	Err      string            `json:"err"`
	Detail   string            `json:"detail"` // technical text, shown on hover
	Note     string            `json:"note"`
	Lines    []string          `json:"lines"`
	Added    int64             `json:"added"`
	Finished int64             `json:"finished"`
	Retries  int               `json:"retries"`
	RetryAt  int64             `json:"retryAt"`
	Bytes    map[string]int64  `json:"bytes"`   // how much each line carried
	Limit    float64           `json:"limit"`   // bytes/s for this download, 0 = none
	StartAt  int64             `json:"startAt"` // do not start before this time (unix), 0 = now
	Hash     string            `json:"hash"`    // checksum the person gave
	Verify   string            `json:"verify"`  // "" | "checking" | "ok" | "bad"
	Web      bool              `json:"web"`
	Queue    string            `json:"queue,omitempty"`   // "" = the main queue
	Mirrors  []string          `json:"mirrors,omitempty"` // other addresses of the same file
	Conns    int               `json:"conns,omitempty"`   // connections per line for this download (0 = automatic)
	Height   int               `json:"height,omitempty"`  // streams: tallest picture wanted (0 = best)
	Log      []dlLogLine       `json:"log,omitempty"`
	TUp      int64             `json:"tup,omitempty"`    // torrents: bytes uploaded in earlier runs
	Kind     string            `json:"kind,omitempty"`   // "" = a file, "torrent"
	Files    []dlTFile         `json:"files,omitempty"`  // torrents: what is inside
	Select   []bool            `json:"select,omitempty"` // torrents: which files to download (nil = all)
}

type dlEntry struct {
	dlItem
	cancel      context.CancelFunc
	task        dlRunner
	wantPause   bool
	wantHold    bool
	running     bool
	mirrorTries int
	prog        dlProgress
	doneCh      chan struct{}
}

type dlView struct {
	dlItem
	Header     struct{}     `json:"-"`
	Speed      float64      `json:"speed"`
	LineStat   []dlLineStat `json:"lineStats"`
	Order      int          `json:"order"`
	Map        []int        `json:"map,omitempty"` // progress of the parts of the file, for the segment bar
	Up         int64        `json:"up,omitempty"`
	NFiles     int          `json:"nFiles,omitempty"` // torrents: how many files (the list itself is fetched on demand)
	Peers      int          `json:"peers,omitempty"`
	Seeds      int          `json:"seeds,omitempty"`
	SwarmSeeds int          `json:"swarmSeeds,omitempty"`
}

type dlManager struct {
	mu     sync.Mutex
	items  []*dlEntry
	cfg    dlConfig
	hosts  map[string]int64
	dirty  bool
	global dlBucket
	perLn  map[string]*dlBucket
	lines  func() []dlLine
	// uiLinesFn lists the lines for the settings page (nil: the same as lines)
	uiLinesFn func() []dlLine
	seq       int
	quit      bool
	wg        sync.WaitGroup
	loopWG    sync.WaitGroup

	callInfo   func() (bool, string)   // is a call running, on which line
	presetInfo func() (string, string) // the active preset's name and what it wants for downloads
	power      func(kind string)       // sleep / shutdown (replaced in tests)
	hold       string                  // why downloads wait right now ("" = they do not)
	presetName string
	powerAt    int64 // when a shutdown was started (it can be cancelled for a minute)
	finished   int   // downloads finished since "when finished" was set
	armedFor   string
}

var dlMgr *dlManager

func init() { dlMgr = newDlManager(dlProdLines); dlMgr.uiLinesFn = dlUILines }

func newDlManager(lines func() []dlLine) *dlManager {
	m := &dlManager{hosts: map[string]int64{}, perLn: map[string]*dlBucket{}, lines: lines, callInfo: func() (bool, string) { return false, "" }, power: hPower,
		presetInfo: hPreset}
	m.cfg = dlFixConfig(dlConfig{Dir: defaultDlDir(), UseCategories: true, MaxActive: 3, MaxPerLine: 8})
	return m
}

func defaultDlDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Downloads")
	}
	return filepath.Join(hRoot(), "downloads")
}

func dlFile() string { return filepath.Join(hRoot(), "data", "downloads.json") }

// ---- dlEnv ---------------------------------------------------------------------------------------------------------

func (m *dlManager) Lines() []dlLine { return m.activeLines() }

// activeLines applies the person's choices to the lines that exist: lines left out of downloads, and (while a call
// runs) the call's line, which stays free.
func (m *dlManager) activeLines() []dlLine {
	cfg := m.config()
	call, callLine := false, ""
	if cfg.CallMode == "avoid" && m.callInfo != nil {
		call, callLine = m.callInfo()
	}
	var out []dlLine
	for _, l := range m.lines() {
		skip := false
		for _, n := range cfg.ExcludeLines {
			if n == l.Name {
				skip = true
			}
		}
		if skip {
			continue
		}
		if call && callLine != "" && l.Name == callLine {
			base, name := l.Alive, l.Name
			l.Alive = func() bool {
				if on, ln := m.callInfo(); on && ln == name {
					return false
				}
				return base == nil || base()
			}
		}
		out = append(out, l)
	}
	return out
}

func (m *dlManager) Limit(ctx context.Context, line string, n int) error {
	if err := m.global.wait(ctx, n); err != nil {
		return err
	}
	m.mu.Lock()
	b := m.perLn[line]
	m.mu.Unlock()
	if b != nil {
		return b.wait(ctx, n)
	}
	return ctx.Err()
}

func (m *dlManager) HostLocked(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return host != "" && m.hosts[host] > time.Now().Unix()
}

func (m *dlManager) LockHost(host string) {
	if host == "" {
		return
	}
	m.mu.Lock()
	m.hosts[host] = time.Now().Add(3 * 24 * time.Hour).Unix()
	m.dirty = true
	m.mu.Unlock()
	hNote("info", "Downloads: %s only accepts one address per file, so downloads from it use one line", host)
}

func (m *dlManager) Changed() { m.mu.Lock(); m.dirty = true; m.mu.Unlock() }

// ---- the real lines ------------------------------------------------------------------------------------------------

func dlLineCap(name string) float64 { return hLineRate(name) }

func dlHostLines() (out, mobile, held []dlLine) {
	if H.Lines == nil {
		return nil, nil, nil
	}
	l := H.Lines()
	return l.Use, l.Mobile, l.Held
}

func dlProdLines() []dlLine {
	out, mobile, held := dlHostLines()
	if len(out)+len(mobile) == 0 { // never leave downloads without any line
		out = held
	}
	return dlPickLines(out, mobile, dlMgr.config().AllowMobile)
}

// dlUILines: the lines the settings page lists (every fixed line, also one that a role keeps out for now).
func dlUILines() []dlLine {
	out, mobile, held := dlHostLines()
	return dlPickLines(append(out, held...), mobile, dlMgr.config().AllowMobile)
}

// dlPickLines: the mobile line (metered, slow) is only used for downloads when the person allowed it, or when it is the only line.
func dlPickLines(fixed, mobile []dlLine, allowMobile bool) []dlLine {
	if allowMobile || len(fixed) == 0 {
		return append(append([]dlLine{}, fixed...), mobile...)
	}
	return fixed
}

// ---- config and persistence ----------------------------------------------------------------------------------------

func (m *dlManager) config() dlConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

type dlDisk struct {
	Config dlConfig         `json:"config"`
	Items  []dlItem         `json:"items"`
	Hosts  map[string]int64 `json:"hosts"`
	Seq    int              `json:"seq"`
}

// reset puts the manager back to a clean "just created" state (used when it is switched on again).
func (m *dlManager) reset() {
	m.mu.Lock()
	m.quit = false
	m.items = nil
	m.hosts = map[string]int64{}
	m.perLn = map[string]*dlBucket{}
	m.hold, m.powerAt, m.finished, m.armedFor = "", 0, 0, ""
	m.cfg = dlFixConfig(dlConfig{Dir: defaultDlDir(), UseCategories: true, MaxActive: 3, MaxPerLine: 8})
	m.mu.Unlock()
}

func (m *dlManager) load() {
	b, err := os.ReadFile(dlFile())
	if err != nil {
		return
	}
	var d dlDisk
	if json.Unmarshal(b, &d) != nil {
		hLog("downloads.json could not be read - starting with an empty list")
		_ = os.Rename(dlFile(), dlFile()+".bad")
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.Config.Dir != "" {
		m.cfg = d.Config
	}
	m.cfg = dlFixConfig(m.cfg)
	for k, v := range d.Hosts {
		if v > time.Now().Unix() {
			m.hosts[k] = v
		}
	}
	m.seq = d.Seq
	for _, it := range d.Items {
		e := &dlEntry{dlItem: it}
		switch it.State {
		case "downloading":
			e.State = "queued" // it was running when NetMerge closed: carry on
		case "queued", "paused", "done", "error":
		default:
			e.State = "error"
		}
		e.RetryAt = 0
		m.items = append(m.items, e)
	}
}

func dlFixConfig(c dlConfig) dlConfig {
	if c.Dir == "" {
		c.Dir = defaultDlDir()
	}
	if c.MaxActive < 1 || c.MaxActive > 10 {
		c.MaxActive = 3
	}
	if c.MaxPerLine < 1 || c.MaxPerLine > 16 {
		c.MaxPerLine = 8
	}
	if c.CallMode != "avoid" && c.CallMode != "pause" && c.CallMode != "off" {
		c.CallMode = "avoid"
	}
	if c.AfterAll != "sleep" && c.AfterAll != "shutdown" {
		c.AfterAll = ""
	}
	if c.LimitAll < 0 || c.LimitAll > 1e12 {
		c.LimitAll = 0
	}
	ll := map[string]float64{}
	for k, v := range c.LimitLines {
		if v > 0 && v < 1e12 && len(ll) < 32 && k != "" {
			ll[k] = v
		}
	}
	c.LimitLines = ll
	var ex []string
	for _, n := range c.ExcludeLines {
		if n != "" && len(ex) < 32 {
			ex = append(ex, n)
		}
	}
	c.ExcludeLines = ex
	if _, e1 := time.Parse("15:04", c.Window.From); e1 != nil {
		c.Window.Enabled, c.Window.From = false, "22:00"
	}
	if _, e2 := time.Parse("15:04", c.Window.To); e2 != nil {
		c.Window.Enabled, c.Window.To = false, "07:00"
	}
	c.Queues = dlFixQueues(c.Queues)
	if c.OnExists != "overwrite" && c.OnExists != "skip" {
		c.OnExists = "rename"
	}
	cd := map[string]string{}
	for _, cat := range dlCategories {
		if d := strings.TrimSpace(c.CatDirs[cat]); d != "" {
			cd[cat] = d
		}
	}
	c.CatDirs = cd
	c.SiteRules = dlFixRules(c.SiteRules)
	if c.UploadLimit < 0 || c.UploadLimit > 1e12 {
		c.UploadLimit = 0
	}
	c.AfterCmd = strings.TrimSpace(c.AfterCmd)
	if len(c.AfterCmd) > 400 {
		c.AfterCmd = ""
	}
	return c
}

func (m *dlManager) save() {
	m.mu.Lock()
	d := dlDisk{Config: m.cfg, Hosts: m.hosts, Seq: m.seq}
	for _, e := range m.items {
		it := e.dlItem
		if e.task != nil {
			pr := e.prog
			it.Done = pr.Done
		}
		d.Items = append(d.Items, it)
	}
	m.dirty = false
	m.mu.Unlock()
	b, _ := json.MarshalIndent(d, "", " ")
	_ = os.MkdirAll(filepath.Dir(dlFile()), 0o755)
	_ = os.WriteFile(dlFile()+".tmp", b, 0o600)
	_ = os.Rename(dlFile()+".tmp", dlFile())
}

// ---- adding --------------------------------------------------------------------------------------------------------

// dlCleanDir checks a folder given by the person or the page: a full path, no device paths.
func dlCleanDir(d string) (string, error) {
	d = strings.TrimSpace(d)
	if !filepath.IsAbs(d) || strings.HasPrefix(d, `\\.\`) || strings.HasPrefix(d, `\\?\`) || strings.ContainsRune(d, 0) {
		return "", errors.New("choose a full folder path, for example C:\\Downloads")
	}
	return filepath.Clean(d), nil
}

func dlCleanURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 8000 {
		return "", errors.New("that is not a valid link")
	}
	u, err := url.Parse(raw)
	if err == nil && strings.EqualFold(u.Scheme, "ftps") {
		return "", errFTPS
	}
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ftp") || u.Hostname() == "" {
		return "", errors.New("only http, https and ftp links can be downloaded")
	}
	return u.String(), nil
}

// dlSplitAuth takes a login out of a link (http://user:pass@host/...) so the password is not kept in the link.
func dlSplitAuth(link string) (clean, auth string) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil {
		return link, ""
	}
	pw, _ := u.User.Password()
	user := u.User.Username()
	if un, e := url.PathUnescape(user); e == nil {
		user = un
	}
	u.User = nil
	return u.String(), dlBasicAuth(user, pw)
}

type dlAdd struct {
	URL     string            `json:"url"`
	Header  map[string]string `json:"header"`
	Dir     string            `json:"dir"`
	Name    string            `json:"name"`
	Lines   []string          `json:"lines"`
	Start   *bool             `json:"start"`
	StartAt int64             `json:"startAt"`
	Hash    string            `json:"hash"`
	Force   bool              `json:"force"`
	Limit   float64           `json:"limit"`
	FromWeb bool              `json:"fromWeb"` // sent by the browser extension: local-network targets are refused
	Queue   string            `json:"queue"`
	Mirrors []string          `json:"mirrors"`
	Conns   int               `json:"conns"`
	Height  int               `json:"height"` // streams: tallest picture wanted
	User    string            `json:"user"`   // a site that asks for a user name and password (HTTP Basic)
	Pass    string            `json:"pass"`
	Torrent string            `json:"torrent"` // the contents of a .torrent file (base64)
	Select  []bool            `json:"select"`  // torrents: which files to download
}

var errDlDuplicate = errors.New("this link is already in your list")

// dlCleanHash accepts "sha256:abc..." or bare hex and returns "algo:hex" (md5, sha1, sha256, sha512 by length).
func dlCleanHash(h string) (string, error) {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" {
		return "", nil
	}
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[i+1:]
	}
	for _, r := range h {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return "", errors.New("the checksum should be letters a-f and digits only")
		}
	}
	switch len(h) {
	case 32:
		return "md5:" + h, nil
	case 40:
		return "sha1:" + h, nil
	case 64:
		return "sha256:" + h, nil
	case 128:
		return "sha512:" + h, nil
	}
	return "", errors.New("the checksum has the wrong length (MD5, SHA-1, SHA-256 or SHA-512)")
}

func (m *dlManager) add(a dlAdd) (*dlItem, error) {
	if it, err := m.addTorrent(a); it != nil || err != nil {
		return it, err
	}
	link, err := dlCleanURL(a.URL)
	if err != nil {
		return nil, err
	}
	link, auth := dlSplitAuth(link)
	if auth != "" {
		h := map[string]string{}
		for k, v := range a.Header {
			h[k] = v
		}
		h["Authorization"] = auth
		a.Header = h
	}
	if a.Dir != "" {
		d, err := dlCleanDir(a.Dir)
		if err != nil {
			return nil, err
		}
		a.Dir = d
	}
	hash, err := dlCleanHash(a.Hash)
	if err != nil {
		return nil, err
	}
	if a.Limit < 0 || a.Limit > 1e12 {
		a.Limit = 0
	}
	mirrors, err := dlCleanMirrors(a.Mirrors, link)
	if err != nil {
		return nil, err
	}
	if a.User != "" {
		if a.Header == nil {
			a.Header = map[string]string{}
		}
		a.Header["Authorization"] = dlBasicAuth(a.User, a.Pass)
	}
	if a.Conns < 0 || a.Conns > 16 {
		a.Conns = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.Queue != "" && m.cfg.queue(a.Queue) == nil {
		return nil, errors.New("that queue does not exist")
	}
	if len(m.items) >= 5000 {
		return nil, errors.New("the list is full (5000 downloads): clear the finished ones first")
	}
	if !a.Force {
		for _, o := range m.items {
			if o.URL == link {
				return nil, errDlDuplicate
			}
		}
	}
	m.seq++
	e := &dlEntry{}
	e.Hash, e.Limit, e.Web = hash, a.Limit, a.FromWeb
	if a.StartAt > time.Now().Unix() {
		e.StartAt = a.StartAt
	}
	e.ID = fmt.Sprintf("d%d-%d", time.Now().Unix(), m.seq)
	e.URL, e.Header, e.Dir, e.Lines, e.Added = link, dlCleanHeader(a.Header), a.Dir, a.Lines, time.Now().Unix()
	e.Name = dlPickName("", link)
	if a.Name != "" {
		e.Name = dlSafeName(a.Name)
	}
	e.Category = e.cat(e.Name)
	e.State = "queued"
	if a.Start != nil && !*a.Start {
		e.State = "paused"
	}
	e.Queue, e.Mirrors, e.Conns = a.Queue, mirrors, a.Conns
	if a.Height > 0 && a.Height <= 8640 {
		e.Height = a.Height
	}
	m.elog(e, "Added from %s", dlHostOf(link))
	m.items = append(m.items, e)
	m.dirty = true
	it := e.dlItem
	it.Header = nil
	hNote("info", "Download added: %s", e.Name)
	return &it, nil
}

func dlCategory(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exe", ".msi", ".msix", ".apk", ".dmg", ".iso", ".deb", ".rpm", ".appx":
		return "Programs"
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".cab":
		return "Archives"
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".webm", ".flv", ".m4v", ".ts":
		return "Videos"
	case ".mp3", ".flac", ".wav", ".aac", ".ogg", ".m4a", ".opus":
		return "Music"
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".epub", ".csv", ".odt", ".rtf":
		return "Documents"
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg", ".tif", ".tiff", ".heic":
		return "Images"
	}
	return "Other"
}

// pickPath decides where the file goes once the server told us its name. Also used again on retry, so an item keeps its path.
func (m *dlManager) pickPath(e *dlEntry, info dlInfo) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.Path != "" {
		return e.Path, nil
	}
	name := e.Name
	if info.Name != "" && info.Name != "download" {
		name = info.Name
	}
	if e.nameFixed() {
		name = e.Name
	}
	dir := e.Dir
	if dir == "" {
		dir = m.cfg.ruleDir(e.URL)
	}
	if dir == "" {
		dir = m.cfg.CatDirs[e.cat(name)]
	}
	if dir == "" {
		dir = m.cfg.Dir
		if m.cfg.UseCategories {
			dir = filepath.Join(dir, e.cat(name))
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create the folder %s: %v", dir, err)
	}
	taken := func(p string) bool {
		if _, err := os.Stat(p); err == nil {
			return true
		}
		if _, err := os.Stat(dlPartPath(p)); err == nil {
			return true
		}
		for _, o := range m.items {
			if o != e && o.Path == p {
				return true
			}
		}
		return false
	}
	var p string
	mode := m.cfg.OnExists
	if e.Kind == "torrent" && mode == "overwrite" {
		mode = "rename" // a torrent always gets its own folder: removing it later must never touch other files
	}
	switch mode {
	case "overwrite": // only another download in the list keeps the name; a file on the disk is replaced at the end
		p = dlUniquePath(dir, name, func(p string) bool {
			for _, o := range m.items {
				if o != e && o.Path == p {
					return true
				}
			}
			return false
		})
	case "skip":
		p = filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return "", dlStreamErr("A file with this name is already in the folder, so this download was skipped.")
		}
	default:
		p = dlUniquePath(dir, name, taken)
	}
	e.Path, e.Name = p, filepath.Base(p)
	e.Category = e.cat(e.Name)
	m.dirty = true
	return p, nil
}

func (e *dlEntry) nameFixed() bool { return false }

// ---- running -------------------------------------------------------------------------------------------------------

func (m *dlManager) startLocked(e *dlEntry) {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel, e.running, e.wantPause, e.wantHold = cancel, true, false, false
	e.State, e.Err, e.Note = "downloading", "", ""
	e.doneCh = make(chan struct{})
	var t dlRunner
	if e.Kind == "torrent" {
		m.elog(e, "Started")
		t = m.newTorrentTask(e)
		t.SetRate(e.Limit)
	} else {
		spec := dlSpec{URL: e.URL, Header: e.Header, Path: e.Path, Lines: e.Lines, MaxPerLine: m.cfg.MaxPerLine, NoPrivate: e.Web}
		if e.Conns > 0 {
			spec.MaxPerLine = e.Conns
		}
		spec.MaxHeight = e.Height
		spec.Overwrite = m.cfg.OnExists == "overwrite"
		m.elog(e, "Started")
		spec.Pick = func(info dlInfo) (string, error) { return m.pickPath(e, info) }
		spec.OnInfo = func(info dlInfo, path string) {
			m.mu.Lock()
			e.Size = info.Size
			if e.Path == "" {
				e.Path = path
			}
			m.dirty = true
			m.mu.Unlock()
		}
		dt := newDlTask(spec, m)
		dt.bucket.setRate(e.Limit)
		t = dt

	}
	e.task = t
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					hLog("download manager: recovered from a crash while downloading: %v", r)
					err = errors.New("the download stopped because of an internal error")
				}
			}()
			err = t.Run(ctx)
		}()
		m.finishRun(e, t, err)
	}()
}

func (m *dlManager) finishRun(e *dlEntry, t dlRunner, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pr := t.Progress()
	e.running = false
	e.prog = pr
	e.task = nil
	e.Done = pr.Done
	if e.Bytes == nil {
		e.Bytes = map[string]int64{}
	}
	for _, l := range pr.Lines {
		e.Bytes[l.Name] += l.Bytes
	}
	m.dirty = true
	e.TUp += pr.Up
	close(e.doneCh)
	switch {
	case err == nil:
		e.State, e.Err, e.Finished = "done", "", time.Now().Unix()
		e.Header = nil // sign-in cookies are not needed any more
		if p := t.FinalPath(); p != "" {
			e.Path = p
			e.Name = filepath.Base(p)
		}
		if e.Size < 0 || pr.Done > e.Size {
			e.Size = pr.Done
		}
		e.Done = e.Size
		e.Note = pr.Note
		hNote("good", "Download finished: %s", e.Name)
		m.elog(e, "Finished (%d MB)", e.Size>>20)
		if !m.cfg.NoNotify {
			goSafe("notify", func() { hNotify("Download finished", e.Name) })
		}
		m.runAfter(e, m.cfg.AfterCmd)
		m.finished++
		if e.Hash != "" {
			e.Verify = "checking"
			goSafe("verify", func() { m.verify(e.ID, e.Path, e.Hash) })
		}
	case e.wantPause:
		e.State = "paused"
		m.elog(e, "Paused at %d%%", pctOf(e.Done, e.Size))
	case e.wantHold && !m.quit:
		e.State, e.Note = "queued", m.holdTextForLocked(m.hold)
	case m.quit || errors.Is(err, context.Canceled):
		e.State = "queued" // NetMerge is closing: carry on at the next start
	default:
		msg, retry := dlFriendly(err)
		e.Err, e.Detail, e.Note = msg, dlFmtErr(err), ""
		m.elog(e, "Problem: %s", msg)
		var he dlHTTPErr
		sourceErr := errors.As(err, &he) || retry
		if sourceErr && (!retry || e.Retries >= 1) && m.rotateMirror(e, msg) {
			return
		}
		if !retry || e.Retries >= 6 {
			goSafe("notify", func() { hNotify("Download needs attention", e.Name) })
		}
		if retry && e.Retries < 6 {
			e.Retries++
			e.State = "queued"
			e.RetryAt = time.Now().Add(time.Duration(5<<uint(e.Retries-1)) * time.Second).Unix()
			e.Note = "Trying again soon"
		} else if retry && e.Retries >= 6 {
			e.State = "error"
			hNote("bad", "Download failed: %s (%s)", e.Name, msg)
		} else {
			e.State = "error"
			hNote("bad", "Download failed: %s (%s)", e.Name, msg)
		}
	}
}

// dlFriendly turns an error into words a person understands. The bool says whether trying again could help.
func dlFriendly(err error) (string, bool) {
	var he dlHTTPErr
	var se dlStreamErr
	switch {
	case errors.As(err, &se):
		return string(se), false
	case errors.Is(err, errDlChanged):
		return "The file on the website changed while it was downloading. Start it again.", false
	case errors.As(err, &he):
		switch he.Code {
		case 401, 403:
			return "The website refused the download (it may need you to sign in, or the link expired). Open the page in your browser and add the link again.", false
		case 404, 410:
			return "The file was not found on the website. The link may be old.", false
		case 429, 503:
			return "The website is busy or limits downloads. NetMerge will try again.", true
		}
		return he.Error(), he.Code >= 500
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "no connection is available"):
		return "Waiting for an internet connection.", true
	case strings.Contains(s, "not enough space"), strings.Contains(s, "no space left"), strings.Contains(s, "disk is full"), strings.Contains(s, "There is not enough space"):
		return "The disk is full. Free some space and press Resume.", false
	case strings.Contains(s, "cannot write"), strings.Contains(s, "cannot create the folder"), strings.Contains(s, "Access is denied"), strings.Contains(s, "permission denied"):
		return "NetMerge cannot write to the download folder. Choose another folder in the Downloads settings.", false
	case strings.Contains(s, "only http and https"), strings.Contains(s, "refused: the link leads"):
		return s, false
	}
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "no such host"), strings.Contains(low, "server misbehaving") && strings.Contains(low, "lookup"):
		return "The website address could not be found. Check the link, or your DNS.", true
	case strings.Contains(low, "connection refused"):
		return "The website refused the connection.", true
	case strings.Contains(low, "certificate"), strings.Contains(low, "x509"):
		return "The website's security certificate is not trusted, so NetMerge stopped. Open the link in your browser to see why.", false
	case strings.Contains(low, "timeout"), strings.Contains(low, "deadline exceeded"), strings.Contains(low, "stopped sending"):
		return "The website did not answer in time.", true
	case strings.Contains(low, "connection reset"), strings.Contains(low, "unexpected eof"), strings.Contains(low, "broken pipe"):
		return "The connection was cut by the website or the network.", true
	case strings.Contains(low, "network is unreachable"), strings.Contains(low, "no route"):
		return "The connection is not reaching the internet.", true
	}
	return "The download could not continue.", true
}

// startLoop runs the queue in the background until shutdown.
func (m *dlManager) startLoop() {
	m.loopWG.Add(1)
	goSafe("queue", func() { defer m.loopWG.Done(); m.loop() })
}

// loop starts queued downloads when there is room, applies limits and "wait" rules, refreshes progress and saves.
func (m *dlManager) loop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	n := 0
	for range tick.C {
		n++
		now := time.Now()
		m.mu.Lock()
		if m.quit {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		hold, slow := m.evalHold(now)
		m.applyLimits(slow)
		m.mu.Lock()
		if m.hold != hold {
			m.hold = hold
			if hold != "" {
				hNote("info", "Downloads: %s", strings.ToLower(m.holdTextForLocked(hold)))
			} else {
				hNote("info", "Downloads continue")
			}
		}
		active := 0
		for _, e := range m.items {
			if e.running {
				if (hold != "" || m.queueBlock(e, now) != "") && !e.wantHold {
					e.wantHold = true
					e.cancel()
				}
				active++
				if e.task != nil {
					e.prog = e.task.Progress()
				}
			}
		}
		unix := now.Unix()
		queued := 0
		perQ := map[string]int{}
		for _, e := range m.items {
			if e.running {
				perQ[e.Queue]++
			}
		}
		for _, e := range m.items {
			if e.State == "queued" && !e.running {
				queued++
				switch {
				case hold != "":
					e.Note = m.holdTextForLocked(hold)
				case m.queueBlock(e, now) != "":
					e.Note = m.queueBlock(e, now)
				case e.StartAt > unix:
					e.Note = "Starts at " + time.Unix(e.StartAt, 0).Format("15:04")
				case e.RetryAt > unix:
					e.Note = "Trying again soon"
				default:
					e.Note = ""
				}
			}
		}
		if hold == "" {
			for _, e := range m.items {
				if active >= m.cfg.MaxActive {
					break
				}
				if e.State == "queued" && !e.running && e.RetryAt <= unix && e.StartAt <= unix && m.queueBlock(e, now) == "" {
					if q := m.cfg.queue(e.Queue); q != nil && q.MaxActive > 0 && perQ[e.Queue] >= q.MaxActive {
						e.Note = "Waiting for a free place in queue \"" + q.Name + "\""
						continue
					}
					m.startLocked(e)
					perQ[e.Queue]++
					active++
				}
			}
		}
		power := ""
		if m.cfg.AfterAll != "" {
			if m.armedFor != m.cfg.AfterAll {
				m.armedFor, m.finished = m.cfg.AfterAll, 0
			}
			if active == 0 && queued == 0 && m.finished > 0 {
				power = m.cfg.AfterAll
				m.cfg.AfterAll, m.armedFor, m.finished = "", "", 0
				m.dirty = true
			}
		} else {
			m.armedFor = ""
		}
		save := m.dirty && n%4 == 0
		m.mu.Unlock()
		if power != "" {
			hNote("info", "Downloads finished: %s", map[string]string{"sleep": "the PC goes to sleep", "shutdown": "the PC shuts down in one minute"}[power])
			hNotify("Downloads finished", map[string]string{"sleep": "The PC is going to sleep.", "shutdown": "The PC shuts down in one minute. Open NetMerge to cancel."}[power])
			if power == "shutdown" {
				m.mu.Lock()
				m.powerAt = time.Now().Unix()
				m.mu.Unlock()
			}
			if m.power != nil {
				m.power(power)
			}
		}
		if save {
			m.save()
		}
	}
}

// evalHold decides whether downloads must wait right now (call, download hours, preset) and whether a preset wants them slow.
func (m *dlManager) evalHold(now time.Time) (hold string, slow bool) {
	cfg := m.config()
	if m.presetInfo != nil {
		if name, what := m.presetInfo(); what == "pause" {
			m.mu.Lock()
			m.presetName = name
			m.mu.Unlock()
			return "preset", false
		} else if what == "slow" {
			slow = true
		}
	}
	if cfg.Window.Enabled && !inWindow(now, cfg.Window.From, cfg.Window.To) {
		return "window", slow
	}
	if cfg.CallMode == "pause" && m.callInfo != nil {
		if on, _ := m.callInfo(); on {
			return "call", slow
		}
	}
	return "", slow
}

func (m *dlManager) holdText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.holdTextForLocked(m.hold)
}

func (m *dlManager) holdTextFor(h string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.holdTextForLocked(h)
}

func (m *dlManager) holdTextForLocked(h string) string {
	switch h {
	case "call":
		return "Waiting: you are on a call"
	case "window":
		return "Waiting for your download hours (" + m.cfg.Window.From + " to " + m.cfg.Window.To + ")"
	case "preset":
		return "Preset \"" + m.presetName + "\" is on, downloads wait"
	}
	return ""
}

// applyLimits sets the speed limits (all downloads, each line). A "slow" preset limits to a quarter of what the lines can carry.
func (m *dlManager) applyLimits(slow bool) {
	cfg := m.config()
	eff := cfg.LimitAll
	if slow {
		var sum float64
		for _, l := range m.lines() {
			sum += l.Rate
		}
		lim := sum / 4
		if lim < 1.5*MiBf {
			lim = 1.5 * MiBf
		}
		if eff == 0 || lim < eff {
			eff = lim
		}
	}
	if m.global.getRate() != eff {
		m.global.setRate(eff)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, r := range cfg.LimitLines {
		b := m.perLn[name]
		if b == nil {
			b = &dlBucket{}
			m.perLn[name] = b
		}
		if b.getRate() != r {
			b.setRate(r)
		}
	}
	for name, b := range m.perLn {
		if _, ok := cfg.LimitLines[name]; !ok && b.getRate() != 0 {
			b.setRate(0)
		}
	}
}

const MiBf = float64(1 << 20)

// verify checks the finished file against the checksum the person gave.
func (m *dlManager) verify(id, path, want string) {
	ok := dlCheckHash(path, want)
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.find(id); e != nil {
		if ok {
			e.Verify = "ok"
		} else {
			e.Verify = "bad"
			hNote("bad", "Checksum does NOT match: %s", e.Name)
			goSafe("notify", func() { hNotify("Checksum does not match", e.Name+" may be damaged or not the file you expected.") })
		}
		m.dirty = true
	}
}

func dlCheckHash(path, want string) bool {
	algo, hex, _ := strings.Cut(want, ":")
	var h hash.Hash
	switch algo {
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return fmt.Sprintf("%x", h.Sum(nil)) == hex
}

// ---- actions -------------------------------------------------------------------------------------------------------

func (m *dlManager) find(id string) *dlEntry {
	for _, e := range m.items {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (m *dlManager) pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.find(id)
	if e == nil {
		return errors.New("download not found")
	}
	switch {
	case e.running:
		e.wantPause = true
		e.cancel()
	case e.State == "queued" || e.State == "error":
		e.State = "paused"
	}
	m.dirty = true
	return nil
}

func (m *dlManager) resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.find(id)
	if e == nil {
		return errors.New("download not found")
	}
	if e.State == "paused" || e.State == "error" {
		e.State, e.Err, e.Retries, e.RetryAt = "queued", "", 0, 0
		if e.running { // a pause is still finishing: it will start from the loop
			e.State = "paused"
			return errors.New("still pausing, try again in a second")
		}
	}
	m.dirty = true
	return nil
}

// remove takes a download off the list. deleteFiles also deletes the finished file or the partial one.
func (m *dlManager) remove(id string, deleteFiles bool) error {
	m.mu.Lock()
	e := m.find(id)
	if e == nil {
		m.mu.Unlock()
		return errors.New("download not found")
	}
	var wait chan struct{}
	if e.running {
		e.wantPause = true
		e.cancel()
		wait = e.doneCh
	}
	m.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, x := range m.items {
		if x == e {
			m.items = append(m.items[:i], m.items[i+1:]...)
			break
		}
	}
	if e.Kind == "torrent" {
		m.dropTorrentFiles(e, deleteFiles)
	} else if e.Path != "" {
		if e.State != "done" { // unfinished: the partial files are of no use any more
			_ = os.Remove(dlPartPath(e.Path))
			_ = os.Remove(dlStatePath(e.Path))
			_ = os.RemoveAll(hlsDir(e.Path))
			_ = os.Remove(hlsStatePath(e.Path))
			_ = os.RemoveAll(hlsDir(e.Path + ".audio"))
			_ = os.Remove(hlsStatePath(e.Path + ".audio"))
			for _, x := range []string{".aac", ".m4a"} {
				_ = os.Remove(e.Path + ".audio" + x)
				_ = os.Remove(e.Path + ".audio" + x + ".part")
			}
		} else if deleteFiles {
			_ = os.Remove(e.Path)
			for _, x := range []string{".aac", ".m4a"} {
				_ = os.Remove(e.Path + ".audio" + x)
			}
		}
	}
	m.dirty = true
	return nil
}

func (m *dlManager) clearFinished() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keep []*dlEntry
	n := 0
	for _, e := range m.items {
		if e.State == "done" {
			n++
			continue
		}
		keep = append(keep, e)
	}
	m.items = keep
	m.dirty = true
	return n
}

func (m *dlManager) setConfig(c dlConfig) error {
	c = dlFixConfig(c)
	d, err := dlCleanDir(c.Dir)
	if err != nil {
		return err
	}
	c.Dir = d
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("cannot create that folder: %v", err)
	}
	f, err := os.CreateTemp(c.Dir, ".nm-test-*")
	if err != nil {
		return fmt.Errorf("NetMerge cannot write into that folder: %v", err)
	}
	f.Close()
	_ = os.Remove(f.Name())
	for cat, dir := range c.CatDirs {
		d, err := dlCleanDir(dir)
		if err != nil {
			return fmt.Errorf("folder for %s: %v", cat, err)
		}
		c.CatDirs[cat] = d
	}
	for i := range c.SiteRules {
		d, err := dlCleanDir(c.SiteRules[i].Dir)
		if err != nil {
			return fmt.Errorf("folder for %s: %v", c.SiteRules[i].Site, err)
		}
		c.SiteRules[i].Dir = d
	}
	m.mu.Lock()
	m.cfg = c
	m.dirty = true
	m.mu.Unlock()
	return nil
}

func (m *dlManager) views() []dlView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]dlView, 0, len(m.items))
	for i, e := range m.items {
		v := dlView{dlItem: e.dlItem, Order: i}
		v.dlItem.Header = nil
		v.NFiles, v.dlItem.Files = len(e.Files), nil
		v.Up = e.TUp
		if e.running && e.task != nil {
			pr := e.prog
			v.Done, v.Speed, v.LineStat = pr.Done, pr.Speed, pr.Lines
			v.Peers, v.Seeds, v.SwarmSeeds, v.Up = pr.Peers, pr.Seeds, pr.SwarmSeeds, e.TUp+pr.Up
			if pr.Note != "" {
				v.Note = pr.Note
			}
			if pr.Total > 0 {
				v.Size = pr.Total
			}
			v.Map = pr.Map
		}
		out = append(out, v)
	}
	return out
}

func (m *dlManager) shutdown() {
	m.mu.Lock()
	m.quit = true
	for _, e := range m.items {
		if e.running {
			e.cancel()
		}
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
	}
	m.loopWG.Wait()
	m.save()
}

func (m *dlManager) summary() (active int, speed float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.items {
		if e.running {
			active++
			speed += e.prog.Speed
		}
	}
	return
}

var _ = sort.Strings

// brief is what the Overview shows: how many downloads run and the first few of them.
func (m *dlManager) brief() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	var run []map[string]any
	active, queued := 0, 0
	var sp float64
	for _, e := range m.items {
		switch {
		case e.running:
			active++
			sp += e.prog.Speed
			if len(run) < 3 {
				size := e.Size
				if e.prog.Total > 0 {
					size = e.prog.Total
				}
				run = append(run, map[string]any{"name": e.Name, "done": e.prog.Done, "size": size, "speed": e.prog.Speed})
			}
		case e.State == "queued":
			queued++
		}
	}
	return map[string]any{"active": active, "queued": queued, "speed": sp, "items": run, "total": len(m.items)}
}

// forgetHosts drops what was learned about websites (for example "accepts one address only").
func (m *dlManager) forgetHosts() {
	m.mu.Lock()
	m.hosts = map[string]int64{}
	m.dirty = true
	m.mu.Unlock()
}

// move changes the place of a download in the queue.
func (m *dlManager) move(id, where string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i, e := range m.items {
		if e.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return errors.New("download not found")
	}
	e := m.items[idx]
	to := idx
	switch where {
	case "up":
		to = idx - 1
	case "down":
		to = idx + 1
	case "top":
		to = 0
	case "bottom":
		to = len(m.items) - 1
	default:
		return errors.New("unknown move")
	}
	if to < 0 || to >= len(m.items) || to == idx {
		return nil
	}
	m.items = append(m.items[:idx], m.items[idx+1:]...)
	m.items = append(m.items[:to], append([]*dlEntry{e}, m.items[to:]...)...)
	m.dirty = true
	return nil
}

// setLimit sets the speed limit of one download (bytes/s, 0 = none), also while it runs.
func (m *dlManager) setLimit(id string, limit float64) error {
	if limit < 0 || limit > 1e12 {
		limit = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.find(id)
	if e == nil {
		return errors.New("download not found")
	}
	e.Limit = limit
	if e.task != nil {
		e.task.SetRate(limit)
	}
	m.dirty = true
	return nil
}

func (m *dlManager) powerAtTime() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.powerAt > 0 && time.Now().Unix()-m.powerAt < 75 {
		return m.powerAt
	}
	return 0
}

func pctOf(done, size int64) int {
	if size <= 0 {
		return 0
	}
	return int(done * 100 / size)
}
