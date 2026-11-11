package dm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoginHostFromWhatIsTyped(t *testing.T) {
	for in, want := range map[string]string{
		"example.com":                     "example.com",
		" Example.COM ":                   "example.com",
		"https://www.example.com/files/x": "www.example.com",
		"example.com:8080/path":           "example.com",
		"ann@example.com":                 "example.com",
		"http://[::1]:9000/":              "::1",
	} {
		got, err := dlLoginHost(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "a b.com", "http://"} {
		if _, err := dlLoginHost(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestLoginCoversTheSiteAndItsSubdomainsOnly(t *testing.T) {
	if !dlHostCovers("example.com", "example.com") || !dlHostCovers("example.com", "dl.Example.com") {
		t.Fatal("the site and its subdomains are covered")
	}
	if dlHostCovers("example.com", "notexample.com") || dlHostCovers("example.com", "example.com.evil.net") {
		t.Fatal("another site must not be covered")
	}
}

func TestSavedLoginIsUsedWithoutTypingIt(t *testing.T) {
	data := []byte(strings.Repeat("secret", 100000))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "ann" || p != "pw-s3cret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
			http.Error(w, "no", 401)
			return
		}
		http.ServeContent(w, r, "s.bin", time.Unix(1e9, 0), strings.NewReader(string(data)))
	}))
	defer srv.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	if _, err := m.handle(dlReq{Action: "loginSet", Host: "127.0.0.1", dlAdd: dlAdd{User: "ann", Pass: "pw-s3cret"}}); err != nil {
		t.Fatal(err)
	}
	// the window can list the logins but never gets a password back
	out, err := m.handle(dlReq{Action: "loginList"})
	if err != nil || len(out["logins"].([]dlLoginView)) != 1 || out["logins"].([]dlLoginView)[0] != (dlLoginView{"127.0.0.1", "ann"}) {
		t.Fatalf("%v %v", out, err)
	}
	if b, _ := os.ReadFile(loginsFile()); strings.Contains(string(b), "pw-s3cret") {
		t.Fatal("the password is stored in the clear")
	}
	// the info box probe uses it too
	if pr, err := m.probe(srv.URL+"/s.bin", nil, 0); err != nil || pr["ok"] != true {
		t.Fatalf("probe %v %v", pr, err)
	}
	out, err = m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: srv.URL + "/s.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	id := out["added"].([]*dlItem)[0].ID
	if v := waitState(t, m, id, "done", 30*time.Second); v.Size != int64(len(data)) {
		t.Fatalf("%+v", v)
	}
	// removed: the next download gets no login and fails with a login message
	if _, err := m.handle(dlReq{Action: "loginRemove", Host: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	out, _ = m.handle(dlReq{Action: "loginList"})
	if len(out["logins"].([]dlLoginView)) != 0 {
		t.Fatalf("%v", out)
	}
}

func TestAnExplicitLoginBeatsASavedOne(t *testing.T) {
	m := newTestMgr(t, newSimEnv())
	_ = m
	if err := savedLogins.set("example.com", "saved", "x"); err != nil {
		t.Fatal(err)
	}
	h := dlWithSavedLogin("https://example.com/f", map[string]string{"Authorization": "Basic typed"})
	if h["Authorization"] != "Basic typed" || h[authScopeKey] != "" {
		t.Fatalf("%v", h)
	}
	h = dlWithSavedLogin("https://dl.example.com/f", nil)
	if h["Authorization"] != dlBasicAuth("saved", "x") || h[authScopeKey] != "example.com" {
		t.Fatalf("%v", h)
	}
	// the most specific saved site wins
	if err := savedLogins.set("dl.example.com", "special", "y"); err != nil {
		t.Fatal(err)
	}
	if h := dlWithSavedLogin("https://dl.example.com/f", nil); h["Authorization"] != dlBasicAuth("special", "y") {
		t.Fatalf("%v", h)
	}
}

func TestSavedLoginIsNotSentToAnotherSite(t *testing.T) {
	hdr := map[string]string{"Authorization": "Basic abc", authScopeKey: "example.com"}
	same, _ := dlRequest(context.Background(), "https://cdn.example.com/f", hdr)
	if same.Header.Get("Authorization") != "Basic abc" {
		t.Fatal("the site's own subdomain should get the login")
	}
	other, _ := dlRequest(context.Background(), "https://mirror.example.net/f", hdr)
	if other.Header.Get("Authorization") != "" {
		t.Fatal("a mirror on another site must not get the login")
	}
	if same.Header.Get(authScopeKey) != "" || other.Header.Get(authScopeKey) != "" {
		t.Fatal("the scope marker is not a real header")
	}
	// a login that was typed for one link (no scope) still goes where the person put it, as before
	typed, _ := dlRequest(context.Background(), "https://mirror.example.net/f", map[string]string{"Authorization": "Basic abc"})
	if typed.Header.Get("Authorization") == "" {
		t.Fatal("a typed login must keep working")
	}
}
