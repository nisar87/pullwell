package main

// The browser extension is built into the program: its files are embedded, and the zip a browser needs is made on
// request with the product name and port list filled in (the same job as extension/build.py), so the extension
// always matches the program it came with.

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"
)

//go:embed extension/src extension/icons extension/manifest.chrome.json extension/manifest.firefox.json
var extFiles embed.FS

var extKinds = map[string]string{"chrome-edge": "manifest.chrome.json", "firefox": "manifest.firefox.json"}

func extZipName(kind string) string { return AppName + "-extension-" + kind + ".zip" }

var leftover = regexp.MustCompile(`\{\{[A-Z]+\}\}`)

func extFill(text string) (string, error) {
	ports, _ := json.Marshal(appPorts)
	hosts := make([]string, len(appPorts))
	for i, p := range appPorts {
		hosts[i] = fmt.Sprintf("http://127.0.0.1:%d/*", p)
	}
	hj, _ := json.Marshal(hosts)
	out := strings.NewReplacer("{{NAME}}", AppName, "{{SCHEME}}", appScheme, "{{PORTS}}", strings.ReplaceAll(string(ports), ",", ", "), "{{HOSTS}}", string(hj)).Replace(text)
	if m := leftover.FindString(out); m != "" {
		return "", fmt.Errorf("unknown placeholder %s", m)
	}
	return out, nil
}

// extensionZip builds the package for "chrome-edge" or "firefox".
func extensionZip(kind string) ([]byte, error) {
	mf, ok := extKinds[kind]
	if !ok {
		return nil, fmt.Errorf("no extension for %q", kind)
	}
	raw, err := extFiles.ReadFile("extension/" + mf)
	if err != nil {
		return nil, err
	}
	filled, err := extFill(string(raw))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(filled), &m); err != nil {
		return nil, fmt.Errorf("manifest: %v", err)
	}
	m["version"] = appVersion
	manifest, _ := json.MarshalIndent(m, "", "  ")

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name string, data []byte) error {
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := add("manifest.json", manifest); err != nil {
		return nil, err
	}
	err = fs.WalkDir(extFiles, "extension/src", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := extFiles.ReadFile(p)
		if err != nil {
			return err
		}
		text, err := extFill(string(b))
		if err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
		return add(strings.TrimPrefix(p, "extension/src/"), []byte(text))
	})
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(extFiles, "extension/icons", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := extFiles.ReadFile(p)
		if err != nil {
			return err
		}
		return add(strings.TrimPrefix(p, "extension/"), b)
	})
	if err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// extensionHandler serves /extension/<Name>-extension-<kind>.zip to the program's own page.
func extensionHandler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/extension/")
	for kind := range extKinds {
		if name == extZipName(kind) {
			b, err := extensionZip(kind)
			if err != nil {
				logf("extension package: %v", err)
				http.Error(w, "the extension could not be packed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(b)
			return
		}
	}
	http.NotFound(w, r)
}
