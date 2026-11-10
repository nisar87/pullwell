package dm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type extEnv struct {
	t   *testing.T
	srv *httptest.Server
	m   *dlManager
}

func newExtEnv(t *testing.T) *extEnv {
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	old, oldExt := dlMgr, ext
	dlMgr, ext = m, &extStore{MinMB: 5}
	t.Cleanup(func() { dlMgr, ext = old, oldExt })
	mux := http.NewServeMux()
	registerExtRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &extEnv{t, srv, m}
}

func (e *extEnv) call(method, path, origin, token string, body any) (int, map[string]any) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

const extO = "chrome-extension://abcdef"

func (e *extEnv) pairNow() string {
	code, _ := ext.newCode()
	st, out := e.call("POST", "/ext/pair", extO, "", map[string]string{"code": code, "name": "Chrome"})
	if st != 200 {
		e.t.Fatalf("pair: %d %v", st, out)
	}
	return out["token"].(string)
}

func TestExtPairingAndAuth(t *testing.T) {
	e := newExtEnv(t)
	if st, _ := e.call("GET", "/ext/status", extO, "", nil); st != 401 {
		t.Fatalf("status without token: %d", st)
	}
	tok := e.pairNow()
	if st, out := e.call("GET", "/ext/status", extO, tok, nil); st != 200 || out["minMB"].(float64) != 5 {
		t.Fatalf("status: %d %v", st, out)
	}
	if st, _ := e.call("GET", "/ext/status", extO, tok+"x", nil); st != 401 {
		t.Fatal("a wrong token was accepted")
	}
	// the code works only once
	code, _ := ext.newCode()
	e.call("POST", "/ext/pair", extO, "", map[string]string{"code": code})
	if st, _ := e.call("POST", "/ext/pair", extO, "", map[string]string{"code": code}); st != 403 {
		t.Fatal("a code was used twice")
	}
	// the token is not stored in plain text
	ext.mu.Lock()
	ext.saveLocked()
	ext.mu.Unlock()
	raw, _ := os.ReadFile(extFile())
	if strings.Contains(string(raw), tok) {
		t.Fatal("token stored in plain text")
	}
	if fi, _ := os.Stat(extFile()); fi != nil && fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("file mode %v", fi.Mode())
	}
}

func TestExtCodeGuessingAndExpiry(t *testing.T) {
	e := newExtEnv(t)
	code, _ := ext.newCode()
	for i := 0; i < 5; i++ {
		if st, _ := e.call("POST", "/ext/pair", extO, "", map[string]string{"code": "0000000000"}); st != 403 {
			t.Fatal("wrong code accepted")
		}
	}
	if st, _ := e.call("POST", "/ext/pair", extO, "", map[string]string{"code": code}); st != 403 {
		t.Fatal("the code still worked after 5 wrong guesses")
	}
	code, _ = ext.newCode()
	ext.mu.Lock()
	ext.codeExp = time.Now().Add(-time.Second)
	ext.mu.Unlock()
	if st, _ := e.call("POST", "/ext/pair", extO, "", map[string]string{"code": code}); st != 403 {
		t.Fatal("an expired code worked")
	}
	// the code can be typed with a dash and capitals
	code, _ = ext.newCode()
	typed := strings.ToUpper(code[:5] + "-" + code[5:])
	if st, _ := e.call("POST", "/ext/pair", extO, "", map[string]string{"code": typed}); st != 200 {
		t.Fatal("formatted code refused")
	}
}

func TestExtOnlyExtensionsMayCall(t *testing.T) {
	e := newExtEnv(t)
	tok := e.pairNow()
	for _, o := range []string{"https://evil.example", "http://127.0.0.1:9196", "http://localhost:9196", "null"} {
		if st, _ := e.call("GET", "/ext/ping", o, "", nil); st != 403 {
			t.Errorf("origin %q allowed on ping", o)
		}
		if st, _ := e.call("GET", "/ext/status", o, tok, nil); st != 403 {
			t.Errorf("origin %q allowed with a valid token", o)
		}
	}
	if st, _ := e.call("GET", "/ext/ping", "moz-extension://x", "", nil); st != 200 {
		t.Error("firefox extension refused")
	}
	if st, _ := e.call("GET", "/ext/ping", "", "", nil); st != 200 {
		t.Error("no origin (a plain tool) refused on ping")
	}
	req, _ := http.NewRequest("OPTIONS", e.srv.URL+"/ext/add", nil)
	req.Header.Set("Origin", extO)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 204 || resp.Header.Get("Access-Control-Allow-Origin") != extO || !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
}

func TestExtGuessingTokensIsSlowedDown(t *testing.T) {
	e := newExtEnv(t)
	tok := e.pairNow()
	for i := 0; i < 25; i++ {
		e.call("GET", "/ext/status", extO, "bad", nil)
	}
	if st, _ := e.call("GET", "/ext/status", extO, tok, nil); st != 401 {
		t.Fatal("tokens are still being checked at full speed after many wrong ones")
	}
}

func TestExtAddRefusesLocalNetworkAndBadLinks(t *testing.T) {
	e := newExtEnv(t)
	s := newSim(MiB)
	defer s.Close()
	tok := e.pairNow()
	for _, bad := range []string{"file:///etc/passwd", "gopher://x/y", "javascript:alert(1)", "", "not a link"} {
		if st, _ := e.call("POST", "/ext/add", extO, tok, map[string]string{"url": bad}); st != 400 {
			t.Errorf("accepted %q", bad)
		}
	}
	// the simulated server is on the loopback: the link is refused up front (the browser keeps its own download),
	// no row is left behind and no byte reaches the server
	st, out := e.call("POST", "/ext/add", extO, tok, map[string]string{"url": s.URL + "/file.bin", "cookie": "a=b", "referrer": "https://site/"})
	if st != 400 || !strings.Contains(fmt.Sprint(out["error"]), "inside your own network") {
		t.Fatalf("a link into the local network was not refused: %d %v", st, out)
	}
	if s.bytesFrom("127.0.0.1") != 0 {
		t.Fatal("a browser link reached the local network")
	}
	e.m.mu.Lock()
	n := len(e.m.items)
	e.m.mu.Unlock()
	if n != 0 {
		t.Fatalf("the refused link left %d rows behind", n)
	}
}
func TestExtRevoke(t *testing.T) {
	e := newExtEnv(t)
	tok := e.pairNow()
	ext.mu.Lock()
	id := ext.Browsers[0].ID
	ext.mu.Unlock()
	ext.revoke(id)
	if st, _ := e.call("GET", "/ext/status", extO, tok, nil); st != 401 {
		t.Fatal("revoked browser still works")
	}
	tok = e.pairNow()
	if st, _ := e.call("POST", "/ext/unpair", extO, tok, nil); st != 200 {
		t.Fatal("unpair")
	}
	if st, _ := e.call("GET", "/ext/status", extO, tok, nil); st != 401 {
		t.Fatal("unpaired browser still works")
	}
}

func TestExtLimitsAndConfig(t *testing.T) {
	e := newExtEnv(t)
	for i := 0; i < 8; i++ {
		e.pairNow()
	}
	code, _ := ext.newCode()
	if st, _ := e.call("POST", "/ext/pair", extO, "", map[string]string{"code": code}); st != 403 {
		t.Fatal("more than 8 browsers paired")
	}
	if err := ext.setConfig(-1, false); err == nil {
		t.Fatal("negative size accepted")
	}
	if err := ext.setConfig(20, true); err != nil {
		t.Fatal(err)
	}
	if v := ext.view(); v["minMB"] != 20 || v["enabled"] != false {
		t.Fatal(v)
	}
}

func TestExtCodeOnlyFromTheWindowAPI(t *testing.T) {
	newExtEnv(t)
	// no route under /ext can create a code
	mux := http.NewServeMux()
	registerExtRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for _, p := range []string{"/ext/code", "/ext/newcode"} {
		resp, _ := http.Post(srv.URL+p, "application/json", strings.NewReader("{}"))
		if resp.StatusCode != 404 {
			t.Errorf("%s exists", p)
		}
	}
}
