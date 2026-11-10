package dm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueueHoursDaysPauseAndLimit(t *testing.T) {
	size := int64(2 * MiB)
	s := newSim(size)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	now := time.Now()
	setCfg(t, m, func(c *dlConfig) {
		c.Queues = []dlQueue{
			{ID: "night", Name: "Night", Window: dlWindow{Enabled: true, From: now.Add(2 * time.Hour).Format("15:04"), To: now.Add(3 * time.Hour).Format("15:04")}},
			{ID: "mon", Name: "Weekend", Days: []int{int(now.Weekday()+3) % 7}},
			{ID: "hold", Name: "Held", Paused: true},
			{ID: "one", Name: "One", MaxActive: 1},
		}
	})
	add := func(q string, n string) string {
		out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin?" + n, Force: true, Queue: q}})
		if err != nil {
			t.Fatal(err)
		}
		return out["added"].([]*dlItem)[0].ID
	}
	night, mon, held := add("night", "a"), add("mon", "b"), add("hold", "c")
	time.Sleep(1500 * time.Millisecond)
	for _, v := range m.views() {
		if v.State != "queued" {
			t.Fatalf("%s should wait but is %s", v.ID, v.State)
		}
	}
	notes := map[string]string{}
	for _, v := range m.views() {
		notes[v.ID] = v.Note
	}
	if !strings.Contains(notes[night], "hours of queue \"Night\"") || !strings.Contains(notes[mon], "does not run on") || !strings.Contains(notes[held], "is paused") {
		t.Fatalf("notes: %v", notes)
	}
	// open the queues one by one
	setCfg(t, m, func(c *dlConfig) {
		c.Queues[0].Window.Enabled = false
		c.Queues[1].Days = nil
		c.Queues[2].Paused = false
	})
	for _, id := range []string{night, mon, held} {
		waitState(t, m, id, "done", 30*time.Second)
	}
	// a queue with a limit of one runs one at a time even when the general limit is higher
	s.set(func() { s.perConn = 1 * MiB })
	m.mu.Lock()
	m.cfg.MaxActive = 3
	m.mu.Unlock()
	a, b := add("one", "d"), add("one", "e")
	maxSeen := 0
	for end := time.Now().Add(40 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		run, done := 0, 0
		for _, v := range m.views() {
			if v.ID == a || v.ID == b {
				if v.State == "downloading" {
					run++
				}
				if v.State == "done" {
					done++
				}
			}
		}
		if run > maxSeen {
			maxSeen = run
		}
		if done == 2 {
			break
		}
	}
	if maxSeen != 1 {
		t.Fatalf("queue limit: saw %d at once", maxSeen)
	}
}

func TestRunningDownloadStopsWhenItsQueueCloses(t *testing.T) {
	size := int64(6 * MiB)
	s := newSim(size)
	s.perConn = 1 * MiB
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	setCfg(t, m, func(c *dlConfig) { c.Queues = []dlQueue{{ID: "q", Name: "Q"}} })
	out, _ := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin", Queue: "q"}})
	id := out["added"].([]*dlItem)[0].ID
	waitState(t, m, id, "downloading", 10*time.Second)
	setCfg(t, m, func(c *dlConfig) { c.Queues[0].Paused = true })
	v := waitState(t, m, id, "queued", 10*time.Second)
	_ = v
	setCfg(t, m, func(c *dlConfig) { c.Queues[0].Paused = false })
	waitState(t, m, id, "done", 60*time.Second)
}

func TestFoldersBySiteAndTypeAndExistingFiles(t *testing.T) {
	size := int64(1 * MiB)
	s := newSim(size)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	siteDir, catDir := t.TempDir(), t.TempDir()
	if err := m.setConfig(dlConfig{Dir: m.cfg.Dir, MaxActive: 3, MaxPerLine: 8, UseCategories: true,
		CatDirs:   map[string]string{"Other": catDir, "Bogus": "/nope"},
		SiteRules: []dlSiteRule{{Site: "http://127.0.0.1/whatever", Dir: siteDir}}}); err != nil {
		t.Fatal(err)
	}
	if c := m.config(); len(c.CatDirs) != 1 || c.SiteRules[0].Site != "127.0.0.1" {
		t.Fatalf("%+v", c)
	}
	id := addOne(t, m, s.URL+"/file.bin")
	v := waitState(t, m, id, "done", 30*time.Second)
	if filepath.Dir(v.Path) != siteDir {
		t.Fatalf("the site rule was not used: %s", v.Path)
	}
	// without the site rule the folder for the type is used
	_ = m.setConfig(dlConfig{Dir: m.cfg.Dir, MaxActive: 3, MaxPerLine: 8, UseCategories: true, CatDirs: map[string]string{"Other": catDir}})
	v2 := waitState(t, m, addOne(t, m, s.URL+"/file.bin?2"), "done", 30*time.Second)
	if filepath.Dir(v2.Path) != catDir {
		t.Fatalf("the folder for the type was not used: %s", v2.Path)
	}
	// overwrite: the same name replaces the file
	_ = m.setConfig(dlConfig{Dir: m.cfg.Dir, MaxActive: 3, MaxPerLine: 8, UseCategories: true, CatDirs: map[string]string{"Other": catDir}, OnExists: "overwrite"})
	if err := os.WriteFile(v2.Path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.remove(v2.ID, false)
	v3 := waitState(t, m, addOne(t, m, s.URL+"/file.bin?3"), "done", 30*time.Second)
	if v3.Path != v2.Path || fileHash(t, v3.Path) != simHash(size) {
		t.Fatalf("overwrite: %s vs %s", v3.Path, v2.Path)
	}
	// skip: the same name is left alone
	_ = m.setConfig(dlConfig{Dir: m.cfg.Dir, MaxActive: 3, MaxPerLine: 8, UseCategories: true, CatDirs: map[string]string{"Other": catDir}, OnExists: "skip"})
	id4 := addOne(t, m, s.URL+"/file.bin?4")
	v4 := waitState(t, m, id4, "error", 30*time.Second)
	if !strings.Contains(v4.Err, "skipped") {
		t.Fatalf("skip: %+v", v4)
	}
}

func TestLoginForProtectedLinks(t *testing.T) {
	data := []byte(strings.Repeat("secret", 100000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "ann" || p != "pw" {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			http.Error(w, "no", 401)
			return
		}
		http.ServeContent(w, r, "s.bin", time.Unix(1e9, 0), strings.NewReader(string(data)))
	}))
	defer srv.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: srv.URL + "/s.bin", User: "ann", Pass: "pw"}})
	if err != nil {
		t.Fatal(err)
	}
	id := out["added"].([]*dlItem)[0].ID
	v := waitState(t, m, id, "done", 30*time.Second)
	if v.Size != int64(len(data)) {
		t.Fatalf("%+v", v)
	}
	for _, x := range m.views() { // the password never reaches the page
		if b, _ := json.Marshal(x); strings.Contains(string(b), "Basic") {
			t.Fatal("header leaked")
		}
	}
	// wrong password is a clear error
	out, _ = m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: srv.URL + "/s2.bin", User: "ann", Pass: "bad"}})
	id = out["added"].([]*dlItem)[0].ID
	v = waitState(t, m, id, "error", 30*time.Second)
	if !strings.Contains(v.Err, "refused") {
		t.Fatalf("%+v", v)
	}
}

func TestMirrorTakesOverWhenTheMainAddressFails(t *testing.T) {
	size := int64(2 * MiB)
	good := newSim(size)
	defer good.Close()
	bad := newSim(size)
	bad.fails404 = true
	defer bad.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: bad.URL + "/file.bin", Mirrors: []string{good.URL + "/file.bin"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := out["added"].([]*dlItem)[0].ID
	v := waitState(t, m, id, "done", 40*time.Second)
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
	joined := ""
	for _, l := range v.Log {
		joined += l.S + "\n"
	}
	if !strings.Contains(joined, "mirror") || !strings.Contains(joined, "Finished") {
		t.Fatalf("log: %s", joined)
	}
}

func TestChangeLinkKeepsProgress(t *testing.T) {
	size := int64(32 * MiB)
	s := newSim(size)
	s.perConn = 512 << 10
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	id := addOne(t, m, s.URL+"/file.bin")
	waitState(t, m, id, "downloading", 10*time.Second)
	time.Sleep(1500 * time.Millisecond)
	if _, err := m.handle(dlReq{Action: "relink", ID: id, dlAdd: dlAdd{URL: s.URL + "/other.bin"}}); err == nil {
		t.Fatal("a running download cannot be re-linked")
	}
	_ = m.pause(id)
	waitState(t, m, id, "paused", 10*time.Second)
	m.mu.Lock()
	before := m.find(id).Done
	m.mu.Unlock()
	if before == 0 {
		t.Fatal("nothing downloaded before the pause")
	}
	if _, err := m.handle(dlReq{Action: "relink", ID: id, dlAdd: dlAdd{URL: s.URL + "/file.bin?fresh=1"}}); err != nil {
		t.Fatal(err)
	}
	_ = m.resume(id)
	v := waitState(t, m, id, "done", 60*time.Second)
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content after the link changed")
	}
	if !strings.Contains(v.URL, "fresh=1") {
		t.Fatal(v.URL)
	}
}

func TestLinkPatterns(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"http://a/img[001-003].jpg", []string{"http://a/img001.jpg", "http://a/img002.jpg", "http://a/img003.jpg"}},
		{"http://a/p[8-11].zip", []string{"http://a/p8.zip", "http://a/p9.zip", "http://a/p10.zip", "http://a/p11.zip"}},
		{"http://a/[a-c]/f.bin", []string{"http://a/a/f.bin", "http://a/b/f.bin", "http://a/c/f.bin"}},
		{"http://a/x[0-10:5].bin", []string{"http://a/x0.bin", "http://a/x5.bin", "http://a/x10.bin"}},
		{"http://a/[1-2]/[a-b].bin", []string{"http://a/1/a.bin", "http://a/1/b.bin", "http://a/2/a.bin", "http://a/2/b.bin"}},
		{"http://[::1]:80/file.bin", []string{"http://[::1]:80/file.bin"}},
		{"http://a/plain.bin", []string{"http://a/plain.bin"}},
	} {
		got, err := dlExpand(c.in)
		if err != nil || strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s -> %v %v, want %v", c.in, got, err, c.want)
		}
	}
	if _, err := dlExpand("http://a/[1-9999].bin"); err == nil {
		t.Error("more than 500 links must be refused")
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	s := newSim(1 * MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	setCfg(t, m, func(c *dlConfig) { c.Queues = []dlQueue{{ID: "x", Name: "Evening", MaxActive: 2}} })
	f := false
	for i, n := range []string{"a", "b"} {
		_, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin?" + n, Start: &f, Queue: map[bool]string{true: "x", false: ""}[i == 0], Mirrors: []string{s.URL + "/m" + n}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := m.handle(dlReq{Action: "export"})
	if err != nil {
		t.Fatal(err)
	}
	file := out["file"].(dlExportFile)
	if len(file.Items) != 2 || len(file.Queues) != 1 {
		t.Fatalf("%+v", file)
	}
	b, _ := jsonMarshal(file)
	m2 := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	res, err := m2.handle(dlReq{Action: "import", Text: string(b)})
	if err != nil || res["imported"].(int) != 2 {
		t.Fatalf("%v %v", res, err)
	}
	if c := m2.config(); len(c.Queues) != 1 || c.Queues[0].Name != "Evening" {
		t.Fatalf("queue not created: %+v", c.Queues)
	}
	vs := m2.views()
	if vs[0].Queue != m2.config().Queues[0].ID || len(vs[0].Mirrors) != 1 {
		t.Fatalf("%+v", vs[0])
	}
	// a plain list of links
	res, err = m2.handle(dlReq{Action: "import", Text: "# my list\n" + s.URL + "/file.bin?z1\r\n\r\nftp://nope\n" + s.URL + "/file.bin?z2\n"})
	if err != nil || res["imported"].(int) != 3 { // ftp lines are links too (they used to be skipped)
		t.Fatalf("%v %v", res, err)
	}
	if _, err := m2.handle(dlReq{Action: "import", Text: "hello"}); err == nil {
		t.Fatal("garbage must be refused")
	}
}

func TestGrabLinksFromAPage(t *testing.T) {
	page := `<html><body>
	<a href="/files/a.zip">a</a> <a href='b.mp4'>b</a> <a href="https://cdn.example.com/c.pdf?x=1&amp;y=2">c</a>
	<a href="#top">top</a> <a href="mailto:x@y.z">m</a> <a href="/about.html">about</a> <img src="/i/p.png"> <a href="/files/a.zip">again</a>
	</body></html>`
	got := dlGrabLinks("http://site.test/dir/page.html", []byte(page), nil)
	var urls []string
	for _, g := range got {
		urls = append(urls, g.URL)
	}
	want := "http://site.test/files/a.zip http://site.test/dir/b.mp4 https://cdn.example.com/c.pdf?x=1&y=2 http://site.test/i/p.png"
	if strings.Join(urls, " ") != want {
		t.Fatalf("all files: %v", urls)
	}
	zips := dlGrabLinks("http://site.test/", []byte(page), []string{".ZIP", "mp4"})
	if len(zips) != 2 {
		t.Fatalf("types: %v", zips)
	}
	// through a real page and the manager
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<a href="one.zip">1</a><a href="two.bin">2</a>`))
	}))
	defer srv.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, err := m.handle(dlReq{Action: "grab", dlAdd: dlAdd{URL: srv.URL + "/p"}, Types: []string{"zip"}})
	if err != nil {
		t.Fatal(err)
	}
	if l := out["links"].([]dlGrabbed); len(l) != 1 || l[0].Name != "one.zip" {
		t.Fatalf("%+v", out)
	}
}

func TestCommandAfterDownloadAndSplitting(t *testing.T) {
	got := dlSplitCommand(`"C:\Program Files\Scan\scan.exe" --file "%f" -q`, `C:\d\a b.zip`)
	if len(got) != 4 || got[0] != `C:\Program Files\Scan\scan.exe` || got[2] != `C:\d\a b.zip` {
		t.Fatalf("%q", got)
	}
	size := int64(1 * MiB)
	s := newSim(size)
	defer s.Close()
	ran := make(chan []string, 1)
	old := H.Run
	H.Run = func(a []string) error { ran <- a; return nil }
	defer func() { H.Run = old }()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	setCfg(t, m, func(c *dlConfig) { c.AfterCmd = "scanner --check %f" })
	id := addOne(t, m, s.URL+"/file.bin")
	v := waitState(t, m, id, "done", 30*time.Second)
	select {
	case a := <-ran:
		if a[0] != "scanner" || a[2] != v.Path {
			t.Fatalf("%q", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the command was not run")
	}
}

func TestSegmentMapAndPerDownloadConnections(t *testing.T) {
	size := int64(24 * MiB)
	s := newSim(size)
	s.perConn = 2 * MiB
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, _ := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL + "/file.bin", Conns: 2}})
	id := out["added"].([]*dlItem)[0].ID
	waitState(t, m, id, "downloading", 10*time.Second)
	time.Sleep(2500 * time.Millisecond)
	for _, v := range m.views() {
		if v.ID == id {
			if len(v.Map) < 10 {
				t.Fatalf("no segment map while running: %v", v.Map)
			}
			for _, l := range v.LineStat {
				if l.Workers > 2 {
					t.Fatalf("more than the 2 connections asked for: %d", l.Workers)
				}
			}
		}
	}
	if err := m.setConns(id, 99); err == nil {
		t.Fatal("99 connections must be refused")
	}
	_ = m.remove(id, true)
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// the page reads the segment map as a list of numbers (bytes would arrive as one text string)
func TestSegmentMapIsAJSONList(t *testing.T) {
	b, _ := json.Marshal(dlProgress{Map: dlPieceMap([]*dlPiece{{start: 0, end: 99, have: 50}, {start: 100, end: 199, have: 100}}, 2)})
	if !strings.Contains(string(b), `"map":[50,100]`) {
		t.Fatalf("%s", b)
	}
}

func TestGrabFindsMagnetAndTorrentLinks(t *testing.T) {
	page := `<a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=Cool+Film">m</a>
<a href="/files/a.torrent">t</a><a href="/x.zip">z</a><a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=Cool+Film">dup</a>`
	got := dlGrabLinks("http://site.test/", []byte(page), nil)
	if len(got) != 3 || got[0].Ext != "magnet" || got[0].Name != "Cool Film" || got[1].Ext != "torrent" {
		t.Fatalf("%+v", got)
	}
	if only := dlGrabLinks("http://site.test/", []byte(page), []string{"magnet"}); len(only) != 1 {
		t.Fatalf("%+v", only)
	}
}

// BUG found by an outside tester: "conns" in an add request was lost because dlReq.Count has the same JSON name.
func TestAddKeepsTheConnectionCountFromJSON(t *testing.T) {
	m := newTestMgr(t, newSimEnv())
	var in dlReq
	if err := json.Unmarshal([]byte(`{"action":"add","url":"http://example.com/a.zip","start":false,"conns":1}`), &in); err != nil {
		t.Fatal(err)
	}
	out, err := m.handle(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := out["added"].([]*dlItem)[0].Conns; got != 1 {
		t.Fatalf("conns = %d, want 1", got)
	}
}

// Found by an outside review: [max-max] counted past the largest number, wrapped around and never stopped.
func TestPatternArithmeticCannotOverflow(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, in := range []string{
			"http://x/f[9223372036854775807-9223372036854775807].bin",
			"http://x/f[9223372036854775806-9223372036854775807].bin",
			"http://x/f[99999999999999999999-99999999999999999999].bin",
			"http://x/f[1-9223372036854775807:9223372036854775807].bin",
			"http://x/f[1-2:99999999999999999999].bin",
		} {
			out, err := dlExpand(in)
			if err == nil && len(out) > 500 {
				t.Errorf("%s made %d links", in, len(out))
			}
			if err == nil && strings.Contains(in, "99999999999999999999") {
				t.Errorf("%s was accepted", in)
			}
		}
		if out, err := dlExpand("http://x/f[1-9223372036854775807:9223372036854775807].bin"); err != nil || len(out) != 1 {
			t.Errorf("a huge step: only the first value fits: %v %v", out, err)
		}
		if out, err := dlExpand("http://x/f[007-010].bin"); err != nil || len(out) != 4 || !strings.Contains(out[0], "007") {
			t.Errorf("ordinary pattern: %v %v", out, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pattern expansion did not stop")
	}
}

func TestImportAcceptsFTPAndMagnetLines(t *testing.T) {
	items, _, err := dlParseImport("# list\nhttp://a/1.bin\nftp://b/2.bin\nmagnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567\nnotalink\n")
	if err != nil || len(items) != 3 {
		t.Fatalf("%v %v", items, err)
	}
}
