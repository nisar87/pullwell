package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHostGuard(t *testing.T) {
	h := hostGuard(9197, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for host, want := range map[string]int{
		"127.0.0.1:9197": 200, "localhost:9197": 200, "LOCALHOST:9197": 200,
		"127.0.0.1:9297":      403, // another port: not us
		"evil.example:9197":   403, // DNS rebinding
		"127.0.0.1":           403,
		"192.168.1.10:9197":   403,
		"127.0.0.1:9197.evil": 403,
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %q: got %d, want %d", host, w.Code, want)
		}
	}
}

func TestAPIGuard(t *testing.T) {
	h := apiGuard(9197)(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	for _, c := range []struct {
		method, origin string
		header         bool
		want           int
	}{
		{"GET", "", false, 200},                       // the page reading the list
		{"GET", "http://127.0.0.1:9197", false, 200},  // same, from the page
		{"POST", "http://127.0.0.1:9197", true, 200},  // the page changing something
		{"POST", "http://localhost:9197", true, 200},  // same, other spelling
		{"POST", "", true, 200},                       // a local script (smoke.py)
		{"POST", "http://127.0.0.1:9197", false, 403}, // no custom header
		{"POST", "https://evil.example", true, 403},   // a web page
		{"GET", "https://evil.example", false, 403},   // a web page reading the list
		{"POST", "http://127.0.0.1:8080", true, 403},  // another local program's page
		{"POST", "http://127.0.0.1:91970", true, 403}, // a longer port
		{"POST", "null", true, 403},                   // a sandboxed page or a file
	} {
		r := httptest.NewRequest(c.method, "/api/downloads", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.header {
			r.Header.Set(appHeader, "1")
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != c.want {
			t.Errorf("%s origin=%q header=%v: got %d, want %d", c.method, c.origin, c.header, w.Code, c.want)
		}
	}
}

func TestListenFirstFree(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := free.Addr().(*net.TCPAddr).Port
	free.Close()
	ln, port, err := listenFirstFree([]int{busy.Addr().(*net.TCPAddr).Port, freePort})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if port != freePort {
		t.Fatalf("took port %d, want the fallback %d", port, freePort)
	}
	if _, _, err := listenFirstFree([]int{busy.Addr().(*net.TCPAddr).Port}); err == nil {
		t.Fatal("no error when every port is taken")
	}
}

func TestAppPortsFixed(t *testing.T) {
	// The browser extension tries these in order, so they must never change (handover 4.4).
	want := []int{9197, 9297, 9397, 9497}
	if len(appPorts) != len(want) {
		t.Fatalf("appPorts = %v", appPorts)
	}
	for i := range want {
		if appPorts[i] != want[i] {
			t.Fatalf("appPorts = %v, want %v", appPorts, want)
		}
	}
}

func TestPageServed(t *testing.T) {
	h, err := routes(9197, func() string { return "app-window" })
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/": "<title>" + AppName + "</title>", "/app.js": "loadDL", "/app.css": "{", "/downloads-more.js": "dlProps"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:9197"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: code %d, body without %q", path, w.Code, want)
		}
		if path == "/" {
			body := w.Body.String()
			if strings.Contains(body, "{{") {
				t.Error("page still has an unfilled {{...}} placeholder")
			}
			if !strings.Contains(body, "header: '"+appHeader+"'") || !strings.Contains(body, "host: 'app-window'") {
				t.Error("page does not carry the header name and window kind")
			}
			if w.Header().Get("X-Frame-Options") != "DENY" {
				t.Error("page can be embedded by other sites")
			}
		}
	}
}
