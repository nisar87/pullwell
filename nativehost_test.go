package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func frame(v any) []byte {
	var b bytes.Buffer
	_ = writeHostMsg(&b, v)
	return b.Bytes()
}

func readReply(t *testing.T, b []byte) map[string]any {
	t.Helper()
	raw, err := readHostMsg(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("no reply: %v", err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// fakeApp answers /ext/ping like the program does, on a free port.
func fakeApp(t *testing.T, app string) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ext/ping", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"app": app, "version": "x"})
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	return ln.Addr().(*net.TCPAddr).Port, func() { srv.Close() }
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func TestIsNativeHostCall(t *testing.T) {
	yes := [][]string{
		{"chrome-extension://" + extChromeID + "/"},
		{"chrome-extension://abc/", "--parent-window=123"},
		{`C:\x\browser-host-firefox.json`, extFirefoxID()},
	}
	no := [][]string{nil, {"https://example.com/a.zip"}, {"pullwell://open"}, {"a.json"}, {"a.json", "other@x"}}
	for _, a := range yes {
		if !isNativeHostCall(a) {
			t.Errorf("%v should be a host call", a)
		}
	}
	for _, a := range no {
		if isNativeHostCall(a) {
			t.Errorf("%v should not be a host call", a)
		}
	}
}

func TestHostMessageFraming(t *testing.T) {
	b := frame(map[string]string{"cmd": "start"})
	if binary.LittleEndian.Uint32(b[:4]) != uint32(len(b)-4) {
		t.Fatal("length prefix is wrong")
	}
	if _, err := readHostMsg(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0x7f})); err == nil {
		t.Fatal("a huge message must be refused")
	}
	if _, err := readHostMsg(bytes.NewReader([]byte{0, 0, 0, 0})); err == nil {
		t.Fatal("an empty message must be refused")
	}
	if _, err := readHostMsg(bytes.NewReader([]byte{5, 0, 0, 0, 'a'})); err == nil {
		t.Fatal("a cut message must be refused")
	}
}

func TestHostPingAndStartWhenRunning(t *testing.T) {
	port, stop := fakeApp(t, strings.ToLower(AppName))
	defer stop()
	started := false
	var out bytes.Buffer
	serveHost(bytes.NewReader(append(frame(map[string]string{"cmd": "ping"}), frame(map[string]string{"cmd": "start"})...)), &out,
		hostDeps{ports: []int{freePort(t), port}, start: func() error { started = true; return nil }, wait: time.Second})
	if started {
		t.Fatal("it must not start a second copy when the program answers")
	}
	r1 := readReply(t, out.Bytes())
	if r1["ok"] != true || int(r1["port"].(float64)) != port || r1["started"] != false {
		t.Fatalf("reply: %v", r1)
	}
}

func TestHostStartsTheProgram(t *testing.T) {
	port := freePort(t)
	var stop func()
	var out bytes.Buffer
	serveHost(bytes.NewReader(frame(map[string]string{"cmd": "start"})), &out, hostDeps{ports: []int{port}, wait: 5 * time.Second,
		start: func() error {
			go func() { // the program needs a moment to come up
				time.Sleep(600 * time.Millisecond)
				ln, _ := net.Listen("tcp", net.JoinHostPort("127.0.0.1", itoa(port)))
				mux := http.NewServeMux()
				mux.HandleFunc("/ext/ping", func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(map[string]any{"app": strings.ToLower(AppName)})
				})
				srv := &http.Server{Handler: mux}
				stop = func() { srv.Close() }
				srv.Serve(ln)
			}()
			return nil
		}})
	if stop != nil {
		defer stop()
	}
	r := readReply(t, out.Bytes())
	if r["ok"] != true || r["started"] != true {
		t.Fatalf("reply: %v", r)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestHostGivesUpAndIgnoresOtherPrograms(t *testing.T) {
	other, stop := fakeApp(t, "someone-else")
	defer stop()
	var out bytes.Buffer
	serveHost(bytes.NewReader(frame(map[string]string{"cmd": "start"})), &out, hostDeps{ports: []int{other}, wait: 700 * time.Millisecond, start: func() error { return nil }})
	r := readReply(t, out.Bytes())
	if r["ok"] != false || r["started"] != true {
		t.Fatalf("another program on the port must not count as running: %v", r)
	}
	out.Reset()
	serveHost(bytes.NewReader(frame(map[string]string{"cmd": "start"})), &out, hostDeps{ports: []int{freePort(t)}, wait: time.Second, start: func() error { return errHostNoStart }})
	r = readReply(t, out.Bytes())
	if r["ok"] != false || r["error"] == nil {
		t.Fatalf("a failed start must be reported: %v", r)
	}
	out.Reset()
	serveHost(bytes.NewReader(frame(map[string]string{"cmd": "format-c"})), &out, hostDeps{ports: nil})
	if r = readReply(t, out.Bytes()); r["error"] == nil {
		t.Fatalf("unknown command: %v", r)
	}
}

func TestHostManifests(t *testing.T) {
	c, f := hostManifests(`C:\Apps\Pullwell.exe`, []string{"chrome-extension://zzzz/", "chrome-extension://bad/path/", "https://evil/", `chrome-extension://q"x/`})
	var cm, fm map[string]any
	if json.Unmarshal(c, &cm) != nil || json.Unmarshal(f, &fm) != nil {
		t.Fatal("manifests are not JSON")
	}
	if cm["name"] != nativeHostName() || cm["type"] != "stdio" || cm["path"] != `C:\Apps\Pullwell.exe` {
		t.Fatalf("chrome manifest: %v", cm)
	}
	got := map[string]bool{}
	for _, o := range cm["allowed_origins"].([]any) {
		got[o.(string)] = true
	}
	if !got[chromeOrigin(extChromeID)] || !got[chromeOrigin(extStoreID)] || !got["chrome-extension://zzzz/"] || len(got) != 3 {
		t.Fatalf("origins: %v", got)
	}
	if a := fm["allowed_extensions"].([]any); len(a) != 1 || a[0] != extFirefoxID() {
		t.Fatalf("firefox manifest: %v", fm)
	}
	if !strings.Contains(nativeHostName(), ".") || strings.ToLower(nativeHostName()) != nativeHostName() {
		t.Fatalf("host name %q", nativeHostName())
	}
}

func TestLearnOrigin(t *testing.T) {
	knownOrigins = map[string]bool{}
	if learnOrigin("moz-extension://x") || learnOrigin("chrome-extension://"+extChromeID) || learnOrigin("chrome-extension://"+extStoreID) {
		t.Fatal("firefox and the built-in address are not learned")
	}
	if !learnOrigin("chrome-extension://abcdef") || learnOrigin("chrome-extension://abcdef/") {
		t.Fatal("a new address is learned once")
	}
	if l := originList(); len(l) != 1 || l[0] != "chrome-extension://abcdef/" {
		t.Fatalf("%v", l)
	}
}

// The address Chrome gives the extension comes from the public key in its manifest. If someone changes either one,
// the starter would silently stop being allowed.
func TestExtensionKeyMatchesBuiltInAddress(t *testing.T) {
	zipBytes, err := extensionZip("chrome-edge")
	if err != nil {
		t.Fatal(err)
	}
	mf := zipFile(t, zipBytes, "manifest.json")
	var m struct {
		Key         string   `json:"key"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(mf, &m); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(m.Key)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(der)
	id := make([]byte, 32)
	for i := 0; i < 16; i++ {
		id[2*i], id[2*i+1] = 'a'+h[i]>>4, 'a'+h[i]&15
	}
	if string(id) != extChromeID {
		t.Fatalf("the key in the manifest gives %s, the program expects %s", id, extChromeID)
	}
	has := false
	for _, p := range m.Permissions {
		has = has || p == "nativeMessaging"
	}
	if !has {
		t.Fatal("the extension needs the nativeMessaging permission")
	}
	if fz, err := extensionZip("firefox"); err != nil || !strings.Contains(string(zipFile(t, fz, "manifest.json")), "nativeMessaging") {
		t.Fatal("the Firefox extension needs the nativeMessaging permission")
	}
}

func zipFile(t *testing.T, z []byte, name string) []byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.File {
		if f.Name == name {
			rc, _ := f.Open()
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			return b
		}
	}
	t.Fatalf("%s not in the zip", name)
	return nil
}

func TestHostPairs(t *testing.T) {
	// a fake program that hands out a token for the right key and origin
	mux := http.NewServeMux()
	mux.HandleFunc("/ext/ping", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"app":%q}`, strings.ToLower(AppName)) })
	mux.HandleFunc("/ext/hostpair", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Key, Origin, Name string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.Key != "k1" || in.Origin != "chrome-extension://abc/" || in.Name != "Edge" {
			w.WriteHeader(403)
			return
		}
		fmt.Fprint(w, `{"token":"tok123"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	ask := func(d hostDeps) map[string]any {
		var out bytes.Buffer
		d.ports, d.wait = []int{port}, time.Second
		serveHost(bytes.NewReader(frame(map[string]string{"cmd": "pair", "name": "Edge"})), &out, d)
		var r map[string]any
		n, _ := readHostMsg(bytes.NewReader(out.Bytes()))
		json.Unmarshal(n, &r)
		return r
	}
	if r := ask(hostDeps{origin: "chrome-extension://abc/", key: func() string { return "k1" }}); r["ok"] != true || r["token"] != "tok123" {
		t.Fatalf("pair: %v", r)
	}
	if r := ask(hostDeps{origin: "chrome-extension://abc/", key: func() string { return "bad" }}); r["ok"] == true || r["token"] != nil {
		t.Fatalf("a wrong key must not pair: %v", r)
	}
	if r := ask(hostDeps{origin: "chrome-extension://abc/", key: func() string { return "" }}); r["ok"] == true {
		t.Fatalf("no key file, no pairing: %v", r)
	}
	if r := ask(hostDeps{origin: "chrome-extension://abc/"}); r["ok"] == true {
		t.Fatalf("no key reader, no pairing: %v", r)
	}
}

func TestHostOriginAndAutoPair(t *testing.T) {
	if hostOrigin([]string{"chrome-extension://abc/", "--parent-window=1"}) != "chrome-extension://abc/" || hostOrigin([]string{`C:\m.json`, extFirefoxID()}) != "firefox:"+extFirefoxID() || hostOrigin(nil) != "" {
		t.Fatal("hostOrigin")
	}
	knownOrigins = map[string]bool{"chrome-extension://seen/": true}
	for _, o := range []string{chromeOrigin(extChromeID), chromeOrigin(extStoreID), "firefox:" + extFirefoxID(), "chrome-extension://seen/"} {
		if !autoPairOrigin(o) {
			t.Errorf("%s must be allowed", o)
		}
	}
	for _, o := range []string{"chrome-extension://stranger/", "firefox:other@x", "https://evil/", ""} {
		if autoPairOrigin(o) {
			t.Errorf("%s must not be allowed", o)
		}
	}
	root := t.TempDir()
	if loadHostKey(root, false) != "" {
		t.Fatal("the starter must not make the key")
	}
	k := loadHostKey(root, true)
	if len(k) != 64 || loadHostKey(root, false) != k || loadHostKey(root, true) != k {
		t.Fatalf("the key is made once and read back: %q", k)
	}
}
