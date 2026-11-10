// Command wintest drives the real Windows program on a Windows machine (GitHub's windows-latest runner), through its
// command line and its local API, with no desktop needed. It checks what only Windows can show: the single instance,
// links handed to the running copy, Start with Windows, the Start menu entry, the pullwell:// link, the self-update
// swap and Go back, keeping the PC awake, a protected program folder, and Uninstall.
//
//	go run ./scripts/wintest -exe Program.exe -exe2 Program-build2.exe -name <AppName>
//
// It changes the current user's settings (Run key, Start menu, link registration), so it is meant for a throwaway
// machine, not a person's PC. It refuses to run outside CI unless -force is given.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	exeSrc  = flag.String("exe", "", "the program to test")
	exe2Src = flag.String("exe2", "", "a second build of it, offered as an update")
	name    = flag.String("name", "", "the product name (AppName)")
	force   = flag.Bool("force", false, "run outside CI too (changes this user's settings!)")
	base    = "http://127.0.0.1:9197/"
	failed  int
	logPath string
)

func check(what string, ok bool, detail ...any) {
	if ok {
		fmt.Println("PASS ", what)
		return
	}
	failed++
	fmt.Println("FAIL ", what, fmt.Sprint(detail...))
}

func main() {
	flag.Parse()
	if *exeSrc == "" || *exe2Src == "" || *name == "" {
		fmt.Println("usage: wintest -exe A.exe -exe2 B.exe -name Name")
		os.Exit(2)
	}
	if os.Getenv("CI") == "" && !*force {
		fmt.Println("wintest changes this user's Windows settings; it runs only in CI (or with -force)")
		os.Exit(2)
	}
	scheme := strings.ToLower(*name)
	work := filepath.Join(os.TempDir(), "wintest")
	os.RemoveAll(work)
	dir := filepath.Join(work, "app")
	os.MkdirAll(dir, 0o755)
	exe := filepath.Join(dir, *name+".exe")
	must(copyFile(*exeSrc, exe))
	logPath = filepath.Join(dir, "data", scheme+".log")
	srv := startFiles()
	defer srv.Close()

	// ---- start ----
	start(exe, "-tray")
	check("the program starts and answers on 127.0.0.1:9197", waitUp(30*time.Second))
	app := getJSON("api/app")
	check("it is a normal install (not a test copy)", app["canInstall"] == true, app)
	version, _ := getJSON("api/update")["current"].(string)
	build1, _ := getJSON("api/update")["build"].(string)
	check("it reports its version", version != "", version)

	ok := waitFor(15*time.Second, func() bool { return getJSON("api/app")["startMenu"] == true })
	lnk := filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, *name+".lnk")
	check("Start menu entry made at start", ok && exists(lnk), lnk)
	cmd := regQuery(`HKCU\Software\Classes\`+scheme+`\shell\open\command`, "")
	check(scheme+":// link registered for this program", strings.Contains(strings.ToLower(cmd), strings.ToLower(`\wintest\app\`+*name+`.exe" "%1"`)), cmd)

	// ---- downloads ----
	data := pattern(3 << 20)
	sum := sha256.Sum256(data)
	add := post("api/downloads", map[string]any{"action": "add", "url": srv.URL + "/file/3MB", "hash": "sha256:" + hex.EncodeToString(sum[:]), "force": true})
	check("a download is added", add["error"] == nil, add)
	it := waitItem(srv.URL+"/file/3MB", "done", 30*time.Second)
	check("it finishes", it != nil && it["state"] == "done", it)
	if it != nil {
		check("the checksum is confirmed", it["verify"] == "ok", it["verify"])
		got, _ := os.ReadFile(it["path"].(string))
		check("the file on disk is right", bytes.Equal(got, data), len(got))
	}
	post("api/downloads", map[string]any{"action": "add", "url": srv.URL + "/slow/12MB", "force": true})
	it = waitItem(srv.URL+"/slow/12MB", "downloading", 15*time.Second)
	check("a slow download runs", it != nil)
	check("the PC is kept awake while it runs", waitFor(30*time.Second, func() bool { return logHas("keeping the PC awake") }))
	if it != nil {
		id := it["id"].(string)
		post("api/downloads", map[string]any{"action": "pause", "id": id})
		check("pause", waitItem(srv.URL+"/slow/12MB", "paused", 10*time.Second) != nil)
		post("api/downloads", map[string]any{"action": "resume", "id": id})
		done := waitItem(srv.URL+"/slow/12MB", "done", 180*time.Second)
		check("resume and finish", done != nil)
		if done != nil {
			got, _ := os.ReadFile(done["path"].(string))
			check("the resumed file is right", bytes.Equal(got, pattern(12<<20)), len(got))
		}
	}
	check("the PC may sleep again when nothing downloads", waitFor(45*time.Second, func() bool { return logHas("the PC may sleep again") }))

	// ---- rename, move to another drive, check a checksum afterwards ----
	if it := waitItem(srv.URL+"/file/3MB", "done", time.Second); it != nil {
		id := it["id"].(string)
		r := post("api/downloads", map[string]any{"action": "rename", "id": id, "name": "renamed.bin"})
		it2 := waitItem(srv.URL+"/file/3MB", "done", time.Second)
		check("rename a finished file on disk", r["error"] == nil && it2 != nil && strings.HasSuffix(it2["path"].(string), `\renamed.bin`) && exists(it2["path"].(string)), r, it2["path"])
		other := `D:\wintest-move`
		if !exists(`D:\`) {
			other = filepath.Join(work, "moved")
		}
		r = post("api/downloads", map[string]any{"action": "moveTo", "id": id, "dir": other})
		it3 := waitItem(srv.URL+"/file/3MB", "done", time.Second)
		got := ""
		if it3 != nil {
			got, _ = it3["path"].(string)
		}
		check("move a finished file to another drive ("+other+")", r["error"] == nil && strings.HasPrefix(strings.ToLower(got), strings.ToLower(other)) && exists(got), r, got)
		r = post("api/downloads", map[string]any{"action": "checkFile", "id": id, "hash": hex.EncodeToString(sum[:])})
		check("check its checksum afterwards", r["error"] == nil && waitFor(10*time.Second, func() bool {
			v := waitItem(srv.URL+"/file/3MB", "done", time.Second)
			return v != nil && v["verify"] == "ok"
		}), r)
	}

	// ---- a typed proxy carries the downloads ----
	px := startProxy()
	post("api/app", map[string]any{"proxy": map[string]any{"mode": "manual", "addr": px.addr}})
	// a made-up name: local addresses never go through a proxy, so the proxy maps this name to the test server
	far := strings.Replace(srv.URL, "127.0.0.1", "files.example.test", 1) + "/file/1MB"
	post("api/downloads", map[string]any{"action": "add", "url": far, "force": true})
	check("a download goes through a typed proxy", waitItem(far, "done", 30*time.Second) != nil && px.hits() > 0, px.hits())
	post("api/app", map[string]any{"proxy": map[string]any{"mode": "system"}})

	// ---- one copy only, and what a second start hands over ----
	t0 := time.Now()
	code := run(exe)
	check("a second start ends at once and hands over", code == 0 && time.Since(t0) < 15*time.Second && logHas("already running"), code)
	run(exe, "https://files.example.test/setup.zip")
	got := post("api/app", map[string]any{"take": "add"})
	check("a link on the command line reaches the running copy's Add dialog", got["add"] == "https://files.example.test/setup.zip", got)
	run(exe, scheme+"://show")
	check(scheme+"://show reaches the running copy", waitFor(5*time.Second, func() bool { return logHas(`second start: "link ` + scheme + `://show`) }))
	run(exe, scheme+"://open?item=x&t="+strings.Repeat("0", 32))
	check("a link with a wrong code does not open anything", waitFor(5*time.Second, func() bool { return logHas("link open: link without a valid token") }))

	// ---- Start with Windows ----
	as := post("api/app", map[string]any{"autostart": true})
	run1 := regQuery(`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, *name)
	check("Start with Windows writes one startup entry", strings.Contains(strings.ToLower(run1), strings.ToLower(`\wintest\app\`+*name+`.exe" -tray`)), run1, as)
	post("api/app", map[string]any{"autostart": false})
	check("and removes it", regQuery(`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, *name) == "")

	// ---- Start menu entry off and on ----
	post("api/app", map[string]any{"startMenu": false})
	check("Start menu entry removed when switched off", !exists(lnk))
	post("api/app", map[string]any{"startMenu": true})
	check("and back", exists(lnk))

	// ---- the connection test against the real internet (GitHub) ----
	ct := post("api/app", map[string]any{"proxyTest": true})
	check("the connection test reaches github.com", ct["ok"] == true, ct)

	// ---- save the list ----
	sl := post("api/app", map[string]any{"saveList": true})
	p, _ := sl["path"].(string)
	check("Save the list writes a file", p != "" && exists(p), sl)

	// ---- self-update from a dropped file, then Go back ----
	b2, _ := os.ReadFile(*exe2Src)
	s2 := sha256.Sum256(b2)
	must(os.WriteFile(filepath.Join(dir, *name+"-update.exe"), b2, 0o755))
	must(os.WriteFile(filepath.Join(dir, *name+"-update.exe.sha256"), []byte(hex.EncodeToString(s2[:])+" "+version+"+wintest2"), 0o644))
	ok = waitFor(90*time.Second, func() bool { b, _ := getJSON("api/update")["build"].(string); return b == "wintest2" })
	check("a dropped update is installed by itself and the new build runs", ok, getJSON("api/update")["build"])
	check("the previous version is kept for Go back", exists(filepath.Join(dir, *name+"-previous.exe")))
	check("the used update file is removed", !exists(filepath.Join(dir, *name+"-update.exe")))
	check("the log says it was updated", logHas("updated from "))
	post("api/update", map[string]any{"action": "rollback"})
	ok = waitFor(60*time.Second, func() bool { b, _ := getJSON("api/update")["build"].(string); return b != "" && b == build1 })
	check("Go back brings the first build back", ok, getJSON("api/update")["build"])
	uc, _ := os.ReadFile(filepath.Join(dir, "data", "update.json"))
	check("and remembers not to install the other one by itself", bytes.Contains(uc, []byte(`"skip"`)), string(uc))
	check("the restart says it went back", waitFor(10*time.Second, func() bool { return logHas("going back to the previous version") }))

	// ---- Uninstall ----
	post("api/app", map[string]any{"autostart": true})
	post("api/app", map[string]any{"uninstall": map[string]any{"data": true}})
	ok = waitFor(30*time.Second, func() bool { return !exists(exe) })
	check("Uninstall removes the program file", ok)
	check("and the previous version", !exists(filepath.Join(dir, *name+"-previous.exe")))
	check("and the data folder (when asked)", waitFor(10*time.Second, func() bool { return !exists(filepath.Join(dir, "data")) }))
	check("and the Start menu entry", !exists(lnk))
	check("and the startup entry", regQuery(`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, *name) == "")
	check("and the link registration", regQuery(`HKCU\Software\Classes\`+scheme+`\shell\open\command`, "") == "")
	check("and the program has quit", waitFor(10*time.Second, func() bool { return !up() }))

	// ---- a protected program folder: the data goes to the user's own app data folder ----
	pdir := filepath.Join(work, "protected")
	os.MkdirAll(pdir, 0o755)
	pexe := filepath.Join(pdir, *name+".exe")
	must(copyFile(*exeSrc, pexe))
	// only the folder (and folders in it) is closed for writing: the program file itself stays runnable
	out, err := exec.Command("icacls", pdir, "/deny", "*S-1-1-0:(CI)(W)").CombinedOutput()
	check("set up a folder nobody may write to", err == nil, string(out))
	start(pexe, "-tray")
	check("the program still starts from a protected folder", waitUp(30*time.Second))
	la := filepath.Join(os.Getenv("LOCALAPPDATA"), *name, "data")
	check("and keeps its data in the user's app data folder", waitFor(10*time.Second, func() bool { return exists(filepath.Join(la, scheme+".log")) }), la)
	check("and nothing in the protected folder", !exists(filepath.Join(pdir, "data")))
	post("api/app", map[string]any{"quit": true})
	check("Quit ends it", waitFor(15*time.Second, func() bool { return !up() }))
	exec.Command("icacls", pdir, "/remove:d", "*S-1-1-0").Run()

	if failed > 0 {
		if b, err := os.ReadFile(logPath); err == nil {
			fmt.Println("---- program log ----\n" + string(b))
		}
		fmt.Printf("%d checks failed\n", failed)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}

// ---- helpers ----

func must(err error) {
	if err != nil {
		fmt.Println("setup failed:", err)
		os.Exit(1)
	}
}

func copyFile(a, b string) error {
	d, err := os.ReadFile(a)
	if err != nil {
		return err
	}
	return os.WriteFile(b, d, 0o755)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func start(exe string, args ...string) {
	c := exec.Command(exe, args...)
	c.Dir = filepath.Dir(exe)
	must(c.Start())
	go c.Wait()
}

// run starts a second copy and waits for it to end (it hands over and quits); -1 when it does not end in 20 s.
func run(exe string, args ...string) int {
	c := exec.Command(exe, args...)
	if err := c.Start(); err != nil {
		return -2
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 0
	case <-time.After(20 * time.Second):
		c.Process.Kill()
		return -1
	}
}

func up() bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get(base + "api/app")
	if err != nil {
		return false
	}
	r.Body.Close()
	return r.StatusCode == 200
}

func waitUp(d time.Duration) bool { return waitFor(d, up) }

func waitFor(d time.Duration, f func() bool) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if f() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return f()
}

func getJSON(path string) map[string]any {
	c := http.Client{Timeout: 5 * time.Second}
	r, err := c.Get(base + path)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	defer r.Body.Close()
	var m map[string]any
	json.NewDecoder(r.Body).Decode(&m)
	return m
}

func post(path string, body any) map[string]any {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-"+*name, "1")
	c := http.Client{Timeout: 30 * time.Second}
	r, err := c.Do(req)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	defer r.Body.Close()
	var m map[string]any
	json.NewDecoder(r.Body).Decode(&m)
	if m == nil {
		m = map[string]any{}
	}
	if r.StatusCode != 200 && m["error"] == nil {
		m["error"] = r.Status
	}
	return m
}

func waitItem(url, state string, d time.Duration) map[string]any {
	var found map[string]any
	waitFor(d, func() bool {
		items, _ := getJSON("api/downloads")["items"].([]any)
		for _, x := range items {
			m, _ := x.(map[string]any)
			if m["url"] == url && m["state"] == state {
				found = m
				return true
			}
		}
		return false
	})
	return found
}

func logHas(s string) bool {
	b, _ := os.ReadFile(logPath)
	return bytes.Contains(b, []byte(s))
}

// regQuery returns the value (or the default value when name is "") of a registry key, "" when it is not there.
func regQuery(key, name string) string {
	args := []string{"query", key}
	if name == "" {
		args = append(args, "/ve")
	} else {
		args = append(args, "/v", name)
	}
	out, err := exec.Command("reg", args...).CombinedOutput()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if i := strings.Index(l, "REG_SZ"); i >= 0 {
			return strings.TrimSpace(l[i+len("REG_SZ"):])
		}
	}
	return ""
}

// pattern is the content of the test files: byte i is i % 251.
func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// startFiles serves /file/<n>MB at full speed and /slow/<n>MB at about 50 KB/s per connection, both with Range support.
func startFiles() *server {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	mux := http.NewServeMux()
	serve := func(slow bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			n, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(r.URL.Path), "MB"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			var out http.ResponseWriter = w
			if slow {
				out = slowWriter{w}
			}
			http.ServeContent(out, r, "", time.Unix(1700000000, 0), bytes.NewReader(pattern(n<<20)))
		}
	}
	mux.HandleFunc("/file/", serve(false))
	mux.HandleFunc("/slow/", serve(true))
	s := &http.Server{Handler: mux}
	go s.Serve(ln)
	return &server{URL: "http://" + ln.Addr().String(), s: s}
}

type server struct {
	URL string
	s   *http.Server
}

func (s *server) Close() { s.s.Close() }

type slowWriter struct{ http.ResponseWriter }

func (w slowWriter) Write(b []byte) (int, error) {
	n := 0
	for len(b) > 0 {
		k := min(len(b), 2<<10) // about 50 KB/s for each connection
		m, err := w.ResponseWriter.Write(b[:k])
		n += m
		if err != nil {
			return n, err
		}
		b = b[k:]
		time.Sleep(40 * time.Millisecond)
	}
	return n, nil
}

// startProxy is a small forward proxy (plain HTTP), counting the requests it carried.
type proxy struct {
	addr string
	n    *int64
}

func (p proxy) hits() int64 { return *p.n }

func startProxy() proxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	var n int64
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		r.RequestURI = ""
		r.URL.Host = strings.Replace(r.URL.Host, "files.example.test", "127.0.0.1", 1)
		resp, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	return proxy{addr: ln.Addr().String(), n: &n}
}
