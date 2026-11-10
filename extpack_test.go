package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtensionZip(t *testing.T) {
	for kind := range extKinds {
		b, err := extensionZip(kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatalf("%s: not a zip: %v", kind, err)
		}
		files := map[string]string{}
		for _, f := range z.File {
			rc, _ := f.Open()
			var sb strings.Builder
			buf := make([]byte, 1<<16)
			for {
				n, err := rc.Read(buf)
				sb.Write(buf[:n])
				if err != nil {
					break
				}
			}
			rc.Close()
			files[f.Name] = sb.String()
		}
		for _, need := range []string{"manifest.json", "background.js", "core.js", "popup.html", "popup.js", "popup.css", "icons/16.png", "icons/128.png"} {
			if _, ok := files[need]; !ok {
				t.Errorf("%s: %s missing", kind, need)
			}
		}
		var m struct {
			Name    string   `json:"name"`
			Version string   `json:"version"`
			Hosts   []string `json:"host_permissions"`
		}
		if err := json.Unmarshal([]byte(files["manifest.json"]), &m); err != nil {
			t.Fatalf("%s: manifest: %v", kind, err)
		}
		if m.Name != AppName+" Downloads" || m.Version != appVersion || len(m.Hosts) != len(appPorts) {
			t.Errorf("%s: manifest %+v", kind, m)
		}
		for name, text := range files {
			if strings.HasSuffix(name, ".png") {
				continue
			}
			if strings.Contains(text, "{{") {
				t.Errorf("%s/%s: placeholder left", kind, name)
			}
		}
		// the extension looks for the program on exactly the program's ports, and checks it is talking to this app
		if !strings.Contains(files["background.js"], "const PORTS = [9197, 9297, 9397, 9497];") || !strings.Contains(files["background.js"], "const APP_ID = '"+appScheme+"';") {
			t.Errorf("%s: background.js does not carry the port list or app id", kind)
		}
	}
}

func TestExtensionDownload(t *testing.T) {
	h, err := routes(9197, func() string { return "webview2" })
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/extension/"+extZipName("chrome-edge"), nil)
	r.Host = "127.0.0.1:9197"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || w.Body.Len() < 1000 {
		t.Fatalf("code %d, type %q, %d bytes", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
	}
	r = httptest.NewRequest("GET", "/extension/../appname.go", nil)
	r.Host = "127.0.0.1:9197"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == 200 && strings.Contains(w.Body.String(), "AppName") {
		t.Fatal("source file served")
	}
}
