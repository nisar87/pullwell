// Package dm is the NetMerge download manager. It is a separate module: it imports nothing from the product, and
// the product reaches it only through Setup, Routes, Enabled/SetEnabled, Shutdown and a few small helpers.
// It is off by default: until it is switched on it starts no goroutine, reads and writes no file, and answers no request.
package dm

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// Line is one way out to the internet (a network adapter) that downloads may use.
type Line = dlLine

// Lines is what the product tells the manager about its connections.
type Lines struct {
	Use    []Line // normal lines
	Mobile []Line // metered / mobile lines (used only when allowed, or when nothing else exists)
	Held   []Line // lines a role keeps out for now (used only when nothing else exists)
}

// Host is everything the manager needs from the product. Every field is optional.
type Host struct {
	Repo             string                                  // "owner/name" of the project on GitHub (where the extension files are published)
	Root             string                                  // the product's folder (the manager keeps its files in Root/data)
	Version          string                                  // product version, for the browser extension
	Lines            func() Lines                            // the connections that exist right now
	LineRate         func(name string) float64               // bytes/s a line is known to carry (0 = unknown)
	CallActive       func() (bool, string)                   // is a call running, on which line
	PresetDownloads  func() (name, mode string)              // the active preset and what it wants for downloads
	Guard            func(http.HandlerFunc) http.HandlerFunc // protects the product's local API
	Log              func(format string, a ...any)           // diagnostic log
	Note             func(kind, format string, a ...any)     // line for the activity page
	Notify           func(title, text string)                // desktop notification
	Power            func(kind string)                       // "sleep" | "hibernate" | "shutdown" | "exit"
	PowerCancel      func() error                            // cancel a started shutdown
	OpenPath         func(path string, reveal bool) error    // open a file or show it in the file manager
	FreeSpace        func(dir string) (int64, error)         // free bytes in a folder
	MarkFromInternet func(path, link string)                 // mark a file as downloaded from the internet
	Run              func(args []string) error               // run a program (no window) and wait for it
}

var H = Host{
	Version: "0",
	Guard:   func(h http.HandlerFunc) http.HandlerFunc { return h },
}

// Setup gives the manager its H. Call it once at start, before Routes and SetEnabled.
func Setup(h Host) {
	if h.Guard == nil {
		h.Guard = func(f http.HandlerFunc) http.HandlerFunc { return f }
	}
	if h.Version == "" {
		h.Version = "0"
	}
	H = h
	if h.CallActive != nil {
		dlMgr.callInfo = h.CallActive
	}
}

func hLog(format string, a ...any) {
	if H.Log != nil {
		H.Log(format, a...)
	}
}
func hNote(kind, format string, a ...any) {
	if H.Note != nil {
		H.Note(kind, format, a...)
	}
}
func hNotify(title, text string) {
	if H.Notify != nil {
		H.Notify(title, text)
	}
}
func hOpen(path string, reveal bool) error {
	if H.OpenPath != nil {
		return H.OpenPath(path, reveal)
	}
	return nil
}
func hPower(kind string) {
	if H.Power != nil {
		H.Power(kind)
	}
}
func hPowerCancel() error {
	if H.PowerCancel != nil {
		return H.PowerCancel()
	}
	return nil
}
func hMark(path, link string) {
	if H.MarkFromInternet != nil {
		H.MarkFromInternet(path, link)
	}
}
func hFree(dir string) (int64, error) {
	if H.FreeSpace != nil {
		return H.FreeSpace(dir)
	}
	return dlFreeSpaceDefault(dir)
}
func hRoot() string {
	if H.Root != "" {
		return H.Root
	}
	return "."
}
func hLineRate(name string) float64 {
	if H.LineRate != nil {
		return H.LineRate(name)
	}
	return 0
}
func hPreset() (string, string) {
	if H.PresetDownloads != nil {
		return H.PresetDownloads()
	}
	return "", ""
}

// ---- small helpers that the manager owns ---------------------------------------------------------------------------

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	writeJSON(w, map[string]string{"error": msg})
}

// inWindow: is now inside the daily window from..to ("15:04"; a window may run over midnight)?
func inWindow(now time.Time, from, to string) bool {
	f, e1 := time.Parse("15:04", from)
	t, e2 := time.Parse("15:04", to)
	if e1 != nil || e2 != nil {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	fm, tm := f.Hour()*60+f.Minute(), t.Hour()*60+t.Minute()
	if fm == tm {
		return false
	}
	if fm < tm {
		return m >= fm && m < tm
	}
	return m >= fm || m < tm
}

// ---- isolation: a bug in here must never take the product down ------------------------------------------------------

// safe runs f and swallows a panic (it is logged).
func safe(what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			hLog("download manager: recovered from a crash in %s: %v\n%s", what, r, strings.TrimSpace(string(debug.Stack())))
		}
	}()
	f()
}

// goSafe starts f in a goroutine that cannot crash the program.
func goSafe(what string, f func()) { go safe(what, f) }

// safeHandler makes an HTTP handler crash-proof.
func safeHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				hLog("download manager: recovered from a crash in %s: %v", r.URL.Path, rec)
				defer func() { _ = recover() }()
				writeErr(w, 500, "something went wrong in the download manager")
			}
		}()
		h(w, r)
	}
}

// ---- the on/off switch ----------------------------------------------------------------------------------------------

func switchFile() string { return filepath.Join(hRoot(), "data", "downloads-switch.json") }

var enabledFlag atomicBool

// Enabled reports whether the manager is on. It is off until the person turns it on.
func Enabled() bool { return enabledFlag.get() }

// LoadSwitch reads the saved on/off choice and, when it is on, starts the manager. Without a saved choice: off.
func LoadSwitch() {
	b, err := os.ReadFile(switchFile())
	if err != nil {
		return
	}
	var s struct {
		On bool `json:"on"`
	}
	if json.Unmarshal(b, &s) == nil && s.On {
		start()
	}
}

// SetEnabled turns the manager on or off and remembers the choice. Turning it off stops every download
// (they stay on disk and continue when it is turned on again).
func SetEnabled(on bool) error {
	if on == Enabled() {
		return nil
	}
	if on {
		start()
	} else {
		stop()
	}
	if err := os.MkdirAll(filepath.Dir(switchFile()), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]bool{"on": on})
	return os.WriteFile(switchFile(), b, 0o600)
}

func start() {
	if !enabledFlag.cas(false, true) {
		return
	}
	safe("start", func() {
		dlMgr.reset()
		dlMgr.load()
		ext.load()
		dlMgr.startLoop()
	})
}

func stop() {
	if !enabledFlag.cas(true, false) {
		return
	}
	safe("stop", dlMgr.shutdown)
}

// Shutdown stops the manager when the product quits (it does not change the saved choice).
func Shutdown() {
	if enabledFlag.cas(true, false) {
		safe("shutdown", dlMgr.shutdown)
	}
}

// Brief is a short status for the product's overview; nil while the manager is off.
func Brief() map[string]any {
	if !Enabled() {
		return nil
	}
	var out map[string]any
	safe("brief", func() { out = dlMgr.brief() })
	return out
}

// BackupExport returns the manager's settings for a settings backup (nothing while it is off).
func BackupExport() any {
	if !Enabled() {
		return nil
	}
	c := dlMgr.config()
	return c
}

// BackupImport restores the settings from a backup made by BackupExport. Ignored while the manager is off.
func BackupImport(raw json.RawMessage) {
	if !Enabled() || len(raw) == 0 {
		return
	}
	var c dlConfig
	if json.Unmarshal(raw, &c) != nil {
		return
	}
	cur := dlMgr.config()
	if err := dlMgr.setConfig(c); err != nil {
		c.Dir = cur.Dir
		_ = dlMgr.setConfig(c)
	}
}

// Routes registers the manager's web routes. While it is off they answer "off" (the browser extension gets 404).
func Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/downloads/switch", H.Guard(safeHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var in struct {
				On bool `json:"on"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
				writeErr(w, 400, "bad request")
				return
			}
			if err := SetEnabled(in.On); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		}
		writeJSON(w, map[string]bool{"enabled": Enabled()})
	})))
	registerDownloadRoutes(mux)
}

func dlFreeSpaceDefault(string) (int64, error) { return 0, errors.New("unknown") }
