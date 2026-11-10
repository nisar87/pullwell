package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseProxyAddr(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.1:8080":           "http://10.0.0.1:8080",
		" proxy.corp:3128 ":       "http://proxy.corp:3128",
		"socks5://127.0.0.1:1080": "socks5://127.0.0.1:1080",
		"socks://h:1080":          "socks5://h:1080",
		"http://u:p@h:8080/x?y":   "http://u:p@h:8080",
		"":                        "",
		"h":                       "",
		"ftp://h:21":              "",
		"http://:8080":            "",
	} {
		u, err := parseProxyAddr(in)
		got := ""
		if err == nil {
			got = u.String()
		}
		if got != want {
			t.Errorf("parseProxyAddr(%q) = %q (%v), want %q", in, got, err, want)
		}
	}
}

func TestSystemProxyFor(t *testing.T) {
	https, _ := url.Parse("https://example.com/a.zip")
	http1, _ := url.Parse("http://example.com/a.zip")
	intra, _ := url.Parse("http://intranet/a.zip")
	corp, _ := url.Parse("http://files.corp.local/a.zip")
	pick := func(sp systemProxy, u *url.URL) string {
		if p := systemProxyFor(sp, u); p != nil {
			return p.String()
		}
		return ""
	}
	sp := systemProxy{Enabled: true, Server: "10.0.0.1:8080", Bypass: "*.corp.local;<local>"}
	if pick(sp, https) != "http://10.0.0.1:8080" || pick(sp, intra) != "" || pick(sp, corp) != "" {
		t.Errorf("one proxy for all: %q %q %q", pick(sp, https), pick(sp, intra), pick(sp, corp))
	}
	sp = systemProxy{Enabled: true, Server: "http=h1:80;https=h2:443"}
	if pick(sp, http1) != "http://h1:80" || pick(sp, https) != "http://h2:443" {
		t.Error("per-scheme proxies")
	}
	sp = systemProxy{Enabled: true, Server: "socks=s:1080"}
	if pick(sp, https) != "socks5://s:1080" {
		t.Errorf("socks only: %q", pick(sp, https))
	}
	sp = systemProxy{Enabled: false, Server: "10.0.0.1:8080"}
	if pick(sp, https) != "" {
		t.Error("used a proxy that is switched off")
	}
}

func TestWildMatch(t *testing.T) {
	for _, c := range []struct {
		p, s string
		ok   bool
	}{{"*.corp", "a.corp", true}, {"10.*", "10.1.2.3", true}, {"10.*", "110.1.2.3", false}, {"a*c*e", "abcde", true}, {"a*c*e", "abcdf", false}, {"x", "x", true}} {
		if wildMatch(c.p, c.s) != c.ok {
			t.Errorf("wildMatch(%q, %q) != %v", c.p, c.s, c.ok)
		}
	}
}

// A download really goes through a typed proxy (a small forward proxy in the test), and "none" goes direct.
func TestProxyCarriesDownloads(t *testing.T) {
	appRoot = t.TempDir()
	os.MkdirAll(filepath.Join(appRoot, "data"), 0o755)
	loadAppSettings()
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "file body") }))
	defer site.Close()
	var through atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		through.Add(1)
		resp, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()
	// the site must not look local, or it is bypassed: reach it by a name that maps to it
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(site.URL, "http://"))
	target := "http://files.example.test:" + port + "/a.bin"
	client := &http.Client{Transport: &http.Transport{Proxy: proxyFor}}

	saveAppSettings(func(a *appSettings) {
		a.Proxy = proxySetting{Mode: "manual", Addr: strings.TrimPrefix(proxy.URL, "http://")}
	})
	// the proxy resolves files.example.test itself; point it at the site
	dt := http.DefaultTransport.(*http.Transport)
	oldDial := dt.DialContext
	defer func() { dt.DialContext = oldDial }()
	dt.DialContext = func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, strings.Replace(a, "files.example.test", "127.0.0.1", 1))
	}
	resp, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "file body" || through.Load() != 1 {
		t.Fatalf("through the proxy: %q, %d requests at the proxy", b, through.Load())
	}
	saveAppSettings(func(a *appSettings) { a.Proxy = proxySetting{Mode: "none"} })
	if u, _ := proxyFor(httptest.NewRequest("GET", target, nil)); u != nil {
		t.Fatal("\"none\" still used a proxy")
	}
	if redactProxy("http://me:secret@h:1") != "http://me:%2A%2A%2A@h:1" && !strings.Contains(redactProxy("http://me:secret@h:1"), "***") {
		t.Error("password not hidden in the log")
	}
}
