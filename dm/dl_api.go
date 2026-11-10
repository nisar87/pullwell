package dm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type dlReq struct {
	Action      string   `json:"action"`
	ID          string   `json:"id"`
	URLs        []string `json:"urls"`
	DeleteFiles bool     `json:"deleteFiles"`
	dlAdd
	Config *dlConfig `json:"config"`
	Move   string    `json:"move"`
	// LimitBytes is the speed limit for one download (action "limit").
	LimitBytes float64 `json:"limitBytes"`
	// Browser extension settings (actions "extCode", "extRevoke", "extConfig").
	ExtMinMB int  `json:"extMinMB"`
	ExtOff   bool `json:"extOff"`
	// Text is an exported list to import; Types are the file types the link grabber should keep.
	Text  string   `json:"text"`
	Types []string `json:"types"`
	Count int      `json:"conns"`
}

func registerDownloadRoutes(mux *http.ServeMux) {
	registerExtRoutes(mux)
	mux.HandleFunc("/api/downloads", H.Guard(safeHandler(func(w http.ResponseWriter, r *http.Request) {
		if !Enabled() {
			if r.Method == http.MethodPost {
				writeErr(w, 409, "The download manager is off. Turn it on in Settings.")
				return
			}
			writeJSON(w, map[string]any{"enabled": false})
			return
		}
		if r.Method == http.MethodPost {
			var in dlReq
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil {
				writeErr(w, 400, "bad request")
				return
			}
			out, err := dlMgr.handle(in)
			if err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			if out != nil {
				writeJSON(w, out)
				return
			}
		}
		snap := dlMgr.snapshot()
		snap["enabled"] = true
		writeJSON(w, snap)
	})))
}

func (m *dlManager) snapshot() map[string]any {
	act, sp := m.summary()
	lines := []map[string]any{}
	for _, l := range m.uiLines() {
		lines = append(lines, map[string]any{"name": l.Name, "kind": l.Kind})
	}
	return map[string]any{"items": m.views(), "config": m.config(), "active": act, "speed": sp, "lines": lines, "knownSites": m.knownHosts(), "ffmpeg": dlFindFFmpeg() != "", "hold": m.holdText(), "powerAt": m.powerAtTime(), "ext": ext.view()}
}

func (m *dlManager) handle(in dlReq) (map[string]any, error) {
	switch in.Action {
	case "probe":
		return m.probe(in.URL, in.Header, in.Height)
	case "add":
		// dlReq.Count and dlAdd.Conns share the JSON name "conns"; Go keeps only the shallower one
		if in.Conns == 0 {
			in.Conns = in.Count
		}
		urls := in.URLs
		if in.URL != "" {
			urls = append([]string{in.URL}, urls...)
		}
		if len(urls) == 0 && in.Torrent != "" {
			urls = []string{""}
		}
		if len(urls) == 0 {
			return nil, errors.New("paste at least one link")
		}
		if len(urls) > 500 {
			return nil, errors.New("too many links at once (500 at most)")
		}
		var expanded []string
		for _, u := range urls {
			if in.Torrent != "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(u)), "magnet:") {
				expanded = append(expanded, strings.TrimSpace(u))
				continue
			}
			list, err := dlExpand(strings.TrimSpace(u))
			if err != nil {
				return nil, err
			}
			expanded = append(expanded, list...)
		}
		urls = expanded
		if len(urls) > 500 {
			return nil, errors.New("too many links at once (500 at most)")
		}
		var added []*dlItem
		skipped := 0
		for _, u := range urls {
			a := in.dlAdd
			a.URL = u
			if len(urls) > 1 {
				a.Name = ""
			}
			it, err := m.add(a)
			if err != nil {
				if errors.Is(err, errDlDuplicate) {
					skipped++
					if len(urls) == 1 {
						return map[string]any{"duplicate": true, "items": m.views(), "config": m.config()}, nil
					}
					continue
				}
				if len(urls) == 1 {
					return nil, err
				}
				continue
			}
			added = append(added, it)
		}
		if len(added) == 0 && skipped > 0 {
			return map[string]any{"duplicate": true, "skipped": skipped, "items": m.views(), "config": m.config()}, nil
		}
		if len(added) == 0 {
			return nil, errors.New("none of the links could be used")
		}
		return map[string]any{"added": added, "skipped": skipped, "items": m.views(), "config": m.config()}, nil
	case "torrentInfo":
		return m.torrentPreview(in.Torrent)
	case "files":
		return m.torrentFiles(in.ID)
	case "select":
		return nil, m.setSelect(in.ID, in.Select)
	case "pause":
		return nil, m.pause(in.ID)
	case "resume":
		return nil, m.resume(in.ID)
	case "retry":
		return nil, m.resume(in.ID)
	case "remove":
		return nil, m.remove(in.ID, in.DeleteFiles)
	case "clear":
		m.clearFinished()
		return nil, nil
	case "pauseAll":
		m.forAll(func(id string) { _ = m.pause(id) })
		return nil, nil
	case "resumeAll":
		m.forAll(func(id string) { _ = m.resume(id) })
		return nil, nil
	case "open", "folder":
		return nil, m.openItem(in.ID, in.Action == "folder")
	case "move":
		return nil, m.move(in.ID, in.Move)
	case "limit":
		return nil, m.setLimit(in.ID, in.LimitBytes)
	case "cancelPower":
		m.mu.Lock()
		m.powerAt = 0
		m.mu.Unlock()
		return nil, hPowerCancel()
	case "extCode":
		code, exp := ext.newCode()
		return map[string]any{"code": code[:5] + "-" + code[5:], "expires": exp.Unix(), "repo": H.Repo}, nil
	case "extRevoke":
		ext.revoke(in.ID)
		return nil, nil
	case "extConfig":
		return nil, ext.setConfig(in.ExtMinMB, in.ExtOff)
	case "relink":
		return nil, m.relink(in.ID, in.URL, in.Header)
	case "setMirrors":
		return nil, m.setMirrors(in.ID, in.URLs)
	case "setQueue":
		return nil, m.setQueue(in.ID, in.Queue)
	case "setConns":
		return nil, m.setConns(in.ID, in.Count)
	case "export":
		return map[string]any{"file": m.exportList()}, nil
	case "import":
		items, queues, err := dlParseImport(in.Text)
		if err != nil {
			return nil, err
		}
		return m.importItems(items, queues)
	case "grab":
		return m.grab(in.URL, in.Types)
	case "startQueue":
		m.startQueue(in.Queue)
		return nil, nil
	case "forgetHosts":
		m.forgetHosts()
		return nil, nil
	case "config":
		if in.Config == nil {
			return nil, errors.New("missing settings")
		}
		return nil, m.setConfig(*in.Config)
	}
	return nil, errors.New("unknown action")
}

func (m *dlManager) forAll(f func(id string)) {
	m.mu.Lock()
	var ids []string
	for _, e := range m.items {
		if e.State != "done" {
			ids = append(ids, e.ID)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		f(id)
	}
}

func (m *dlManager) openItem(id string, reveal bool) error {
	m.mu.Lock()
	e := m.find(id)
	var p, state string
	if e != nil {
		p, state = e.Path, e.State
	}
	m.mu.Unlock()
	if e == nil || p == "" {
		return errors.New("the file is not there")
	}
	if state != "done" {
		p = dlPartPath(p)
		reveal = true
	}
	if _, err := os.Stat(p); err != nil {
		if reveal {
			return hOpen(filepath.Dir(p), false)
		}
		return errors.New("the file was moved or deleted")
	}
	return hOpen(p, reveal)
}

// probe: what is behind this link? Used by the "add" dialog.
func (m *dlManager) probe(link string, hdr map[string]string, height int) (map[string]any, error) {
	link, err := dlCleanURL(link)
	if err != nil {
		return nil, err
	}
	lines := m.lines()
	if len(lines) == 0 {
		return nil, errors.New("no internet connection is available right now")
	}
	h := dlCleanHeader(hdr)
	private := dlLocalHost(dlHostOf(link))
	var info dlInfo
	var perr error
	link, auth := dlSplitAuth(link)
	if auth != "" {
		if h == nil {
			h = map[string]string{}
		}
		h["Authorization"] = auth
	}
	for _, l := range lines {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		if dlIsFTP(link) {
			info, perr = dlProbeFTP(ctx, l, link, h, private)
			cancel()
			if perr == nil {
				break
			}
			continue
		}
		pc := dlLineClient(l, private)
		info, _, perr = dlProbeReferer(ctx, pc, link, h)
		cancel()
		pc.CloseIdleConnections()
		if perr == nil {
			break
		}
	}
	if perr != nil {
		msg, _ := dlFriendly(perr)
		return map[string]any{"ok": false, "error": msg}, nil
	}
	var hls *hlsInfo
	if dlDASHLike(info) || dlHLSLike(info) {
		inspect, ext := dlHLSInspect, ".m3u8"
		if dlDASHLike(info) {
			inspect, ext = dlDASHInspect, ".mpd"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var herr error
		for _, l := range lines {
			pc := dlLineClient(l, private)
			hls, herr = inspect(context.WithValue(ctx, dlHeightKey{}, height), pc, info.FinalURL, h)
			pc.CloseIdleConnections()
			if herr == nil {
				break
			}
			var se dlStreamErr
			if errors.As(herr, &se) {
				break
			}
		}
		cancel()
		if herr != nil {
			msg, _ := dlFriendly(herr)
			return map[string]any{"ok": false, "error": msg}, nil
		}
		if hls != nil {
			if i := strings.LastIndexByte(info.Name, '.'); i > 0 && strings.EqualFold(info.Name[i:], ext) {
				info.Name = info.Name[:i]
			}
			info.Name += "." + hls.Kind
			info.Size, info.Ranges, info.Type = -1, false, "video stream"
		}
	}
	cfg := m.config()
	dir := cfg.Dir
	if cfg.UseCategories {
		dir = filepath.Join(dir, dlCategory(info.Name))
	}
	return map[string]any{"ok": true, "info": info, "hls": hls, "mux": hls != nil && hls.Audio && dlFindFFmpeg() != "", "dir": dir, "category": dlCategory(info.Name), "lines": len(lines),
		"locked": m.HostLocked(dlHostOf(link)), "duplicate": m.hasURL(link), "lineNames": dlNames(lines)}, nil
}

func (m *dlManager) knownHosts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, v := range m.hosts {
		if v > time.Now().Unix() {
			n++
		}
	}
	return n
}

func (m *dlManager) hasURL(link string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.items {
		if e.URL == link {
			return true
		}
	}
	return false
}

func dlNames(ls []dlLine) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

func (m *dlManager) uiLines() []dlLine {
	if m.uiLinesFn != nil {
		return m.uiLinesFn()
	}
	return m.lines()
}
