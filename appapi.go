package main

// /api/app: settings that belong to the program rather than to the downloads (start with Windows).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pullwell/dm"
)

// customRoot is true when the program was started with -root: such a copy (a test or development copy) must not
// register itself to start with Windows, or it would replace the real installation's entry (NetMerge 3.11.12).
var customRoot bool

var errCustomRoot = errors.New("this copy was started with its own data folder (-root), so it cannot start with Windows")

func appHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		var in struct {
			Autostart *bool         `json:"autostart"`
			Quit      bool          `json:"quit"`
			Open      string        `json:"open"` // "downloads" | "log": only these fixed folders can be opened from here
			Clipboard *bool         `json:"clipboard"`
			Take      string        `json:"take"` // "add": the link chosen from a notification, handed over once
			StartMenu *bool         `json:"startMenu"`
			Proxy     *proxySetting `json:"proxy"`
			ProxyTest bool          `json:"proxyTest"`
			SaveExt   string        `json:"saveExt"` // "chrome-edge" | "firefox": save the extension in the download folder and show it
			SaveList  bool          `json:"saveList"`
			KeepAwake *bool         `json:"keepAwake"`
			ShowDone  *bool         `json:"showDone"` // write the download list to a file in the download folder and show it
			Uninstall *struct {
				Data bool `json:"data"` // also delete the data folder (list, settings, log); downloads are never deleted
			} `json:"uninstall"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || (in.Autostart == nil && !in.Quit && in.Open == "" && in.Clipboard == nil && in.Take != "add" && in.StartMenu == nil && in.Uninstall == nil && in.SaveExt == "" && in.Proxy == nil && !in.ProxyTest && !in.SaveList && in.KeepAwake == nil && in.ShowDone == nil) {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad request"})
			return
		}
		if in.Quit {
			logf("quit from the window")
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			go func() { time.Sleep(200 * time.Millisecond); requestQuit() }()
			return
		}
		if in.Open != "" {
			dir := ""
			switch in.Open {
			case "downloads":
				dir = dm.DownloadDir()
			case "log":
				dir = filepath.Join(appRoot, "data")
			}
			var err error
			if dir == "" {
				err = errors.New("unknown folder")
			} else {
				_ = os.MkdirAll(dir, 0o755)
				err = sysOpenPath(dir, false)
			}
			if err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			return
		}
		if in.Proxy != nil {
			p := *in.Proxy
			if p.Mode != "none" && p.Mode != "manual" {
				p.Mode = "system"
			}
			p.Addr = strings.TrimSpace(p.Addr)
			if p.Mode == "manual" {
				if _, err := parseProxyAddr(p.Addr); err != nil {
					w.WriteHeader(409)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
					return
				}
			}
			if err := saveAppSettings(func(a *appSettings) { a.Proxy = p }); err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			logf("proxy: %s %s", p.Mode, redactProxy(p.Addr))
			_ = json.NewEncoder(w).Encode(map[string]any{"proxy": proxyConfig()})
			return
		}
		if in.ProxyTest {
			if err := testProxy(); err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			return
		}
		if in.ShowDone != nil {
			on := *in.ShowDone
			if err := saveAppSettings(func(a *appSettings) { a.ShowDone = on }); err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"showDone": showCompleteOn()})
			return
		}
		if in.KeepAwake != nil {
			on := *in.KeepAwake
			if err := saveAppSettings(func(a *appSettings) { a.NoKeepAwake = !on }); err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"keepAwake": keepAwakeOn()})
			return
		}
		if in.SaveList {
			path, err := saveListFile()
			if err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			logf("list saved: %s", path)
			_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
			return
		}
		if in.SaveExt != "" {
			// The window cannot be relied on to save files (WebView2's download handling differs between
			// versions), so the program writes the file itself and shows it in Explorer.
			path, err := saveExtension(in.SaveExt)
			if err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			logf("extension saved: %s", path)
			_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
			return
		}
		if in.StartMenu != nil {
			on := *in.StartMenu
			err := errCustomRoot
			if !customRoot || !on {
				err = startMenuSet(on)
			}
			if err == nil {
				err = saveAppSettings(func(a *appSettings) { a.NoStartMenu = !on })
			}
			if err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			logf("start menu entry: %v", on)
			_ = json.NewEncoder(w).Encode(map[string]bool{"startMenu": startMenuGet()})
			return
		}
		if in.Uninstall != nil {
			if customRoot {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "this copy was started with its own data folder (-root); delete it by hand"})
				return
			}
			logf("uninstall (data too: %v)", in.Uninstall.Data)
			if on, _ := autoStartGet(); on {
				_ = autoStartSet(false)
			}
			_ = startMenuSet(false)
			if err := uninstallLater(in.Uninstall.Data); err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
			go func() { time.Sleep(300 * time.Millisecond); requestQuit() }()
			return
		}
		if in.Take == "add" {
			_ = json.NewEncoder(w).Encode(map[string]string{"add": takePendingAdd(), "view": takePendingView()})
			return
		}
		if in.Clipboard != nil {
			if err := setClipboardWatch(*in.Clipboard); err != nil {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"clipboard": clipboardOn()})
			return
		}
		var err error
		if *in.Autostart && customRoot {
			err = errCustomRoot
		} else {
			err = autoStartSet(*in.Autostart)
		}
		if err != nil {
			logf("start with Windows: %v", err)
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		logf("start with Windows: %v", *in.Autostart)
	}
	on, err := autoStartGet()
	out := map[string]any{"autostart": on, "canAutostart": err == nil && !customRoot, "version": appVersion, "clipboard": clipboardOn(),
		"startMenu": startMenuGet(), "canInstall": !customRoot && runtime.GOOS == "windows",
		"proxy": proxyConfig(), "systemProxy": proxyText(), "keepAwake": keepAwakeOn(), "showDone": showCompleteOn()}
	_ = json.NewEncoder(w).Encode(out)
}

func saveExtension(kind string) (string, error) {
	if kind != "chrome-edge" && kind != "firefox" {
		return "", errors.New("unknown browser")
	}
	b, err := extensionZip(kind)
	if err != nil {
		return "", err
	}
	dir := dm.DownloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, AppName+"-extension-"+kind+".zip")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	_ = sysOpenPath(path, true)
	return path, nil
}

// saveListFile writes the download list next to the downloads ("pullwell-downloads-2026-11-10.json", with a number
// added when that exists) and shows it in Explorer. The window cannot be relied on to save files itself.
func saveListFile() (string, error) {
	b, err := json.MarshalIndent(dm.ExportList(), "", "  ")
	if err != nil {
		return "", err
	}
	dir := dm.DownloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := strings.ToLower(AppName) + "-downloads-" + time.Now().Format("2006-01-02")
	path := filepath.Join(dir, base+".json")
	for i := 2; i < 100; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d).json", base, i))
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	_ = sysOpenPath(path, true)
	return path, nil
}
