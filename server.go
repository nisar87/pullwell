package main

// The local web server: the downloads page, the engine's API and the browser extension's routes, on 127.0.0.1 only.

import (
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"

	"pullwell/dm"
)

// appPorts: a fixed default port and a short list of fallbacks (handover 4.4). The browser extension tries them in
// this order, so they never change between starts or versions. 9196 is left free on purpose (another program uses it).
var appPorts = []int{9197, 9297, 9397, 9497}

//go:embed ui
var uiFiles embed.FS

// listenFirstFree takes the first port of appPorts that is free on 127.0.0.1.
func listenFirstFree(ports []int) (net.Listener, int, error) {
	var errs []string
	for _, p := range ports {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			return ln, p, nil
		}
		errs = append(errs, err.Error())
	}
	return nil, 0, fmt.Errorf("none of the ports %v is free: %s", ports, strings.Join(errs, "; "))
}

// hostGuard answers only requests addressed to this program on this computer. A web page that tricks the browser into
// sending requests to 127.0.0.1 under another name (DNS rebinding) is refused here.
func hostGuard(port int, next http.Handler) http.Handler {
	ok1, ok2 := "127.0.0.1:"+strconv.Itoa(port), "localhost:"+strconv.Itoa(port)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := strings.ToLower(r.Host)
		if h != ok1 && h != ok2 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiGuard protects the local API: a request that comes from a page must come from this program's own page, and every
// change (anything but GET) must carry the custom header, which a web page cannot add without the program's consent.
func apiGuard(port int) func(http.HandlerFunc) http.HandlerFunc {
	own1, own2 := "http://127.0.0.1:"+strconv.Itoa(port), "http://localhost:"+strconv.Itoa(port)
	return func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if o := r.Header.Get("Origin"); o != "" && o != own1 && o != own2 {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet && r.Header.Get(appHeader) != "1" {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
}

// routes builds the whole server. host says how the page is shown ("app-window", "browser"); the page uses it.
func routes(port int, host func() string) (http.Handler, error) {
	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		return nil, err
	}
	page, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	dm.Routes(mux)
	mux.HandleFunc("/extension/", extensionHandler)
	mux.HandleFunc("/api/app", apiGuard(port)(appHandler))
	mux.HandleFunc("/api/update", apiGuard(port)(updateHandler))
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'") // no other page may embed it
			w.Header().Set("X-Frame-Options", "DENY")
			_, _ = w.Write(fillPage(page, host()))
			return
		}
		static.ServeHTTP(w, r)
	})
	return hostGuard(port, mux), nil
}

// fillPage puts the product name and settings into the page. They come from appname.go, never from a request.
func fillPage(page []byte, host string) []byte {
	return []byte(strings.NewReplacer(
		"{{NAME}}", AppName, "{{HEADER}}", appHeader, "{{VERSION}}", appVersion, "{{BUILD}}", appBuild, "{{HOST}}", host, "{{REPO}}", appRepo, "{{COPY}}", map[bool]string{true: "Separate copy"}[customRoot],
	).Replace(string(page)))
}
