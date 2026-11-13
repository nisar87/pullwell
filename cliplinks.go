package main

// Copied links: when switched on (Options → General, off by default), a link to a file that is copied anywhere in
// Windows brings up a notification "Download this?". Clicking it opens the window with the Add dialog filled in;
// nothing is downloaded without the person confirming. Only links that look like files (by their extension) or
// magnet links are offered, each one once, and nothing is offered while the person is working in our own window.

import (
	"fmt"
	"pullwell/dm"

	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// appSettings are the program's own settings (not the engine's), kept in data/app.json.
type appSettings struct {
	Clipboard   bool         `json:"clipboard"`             // offer to download copied links
	NoStartMenu bool         `json:"noStartMenu,omitempty"` // the person removed the Start menu entry: do not add it back
	Proxy       proxySetting `json:"proxy"`
	NoKeepAwake bool         `json:"noKeepAwake,omitempty"` // let the PC sleep even while downloading
	ShowDone    bool         `json:"showDone,omitempty"`    // bring up the window when a download finishes
	NoDiag      bool         `json:"noDiag,omitempty"`      // the person turned diagnostics off (see report.go)
	DiagNoticed bool         `json:"diagNoticed,omitempty"` // the window has shown the diagnostics notice; nothing is sent before
	InstallID   string       `json:"installId,omitempty"`   // random id of this installation, for reports
}

func showCompleteOn() bool {
	appSetMu.Lock()
	defer appSetMu.Unlock()
	return appSet.ShowDone
}

func keepAwakeOn() bool {
	appSetMu.Lock()
	defer appSetMu.Unlock()
	return !appSet.NoKeepAwake
}

// downloadsActive is how many downloads are running now.
func downloadsActive() int {
	b := dm.Brief()
	n, _ := b["active"].(int)
	return n
}

var (
	appSetMu sync.Mutex
	appSet   appSettings
)

func appSettingsFile() string { return filepath.Join(appRoot, "data", "app.json") }

func loadAppSettings() {
	appSetMu.Lock()
	defer appSetMu.Unlock()
	appSet = appSettings{}
	if b, err := os.ReadFile(appSettingsFile()); err == nil {
		_ = json.Unmarshal(b, &appSet)
	}
}

func saveAppSettings(change func(*appSettings)) error {
	appSetMu.Lock()
	change(&appSet)
	b, _ := json.MarshalIndent(appSet, "", "  ")
	appSetMu.Unlock()
	return os.WriteFile(appSettingsFile(), b, 0o644)
}

func setClipboardWatch(on bool) error {
	if err := saveAppSettings(func(a *appSettings) { a.Clipboard = on }); err != nil {
		return err
	}
	clipWatch(on)
	return nil
}

// keepStartMenu puts the Start menu entry in place (or points it at where the program is now) at every start,
// unless the person removed it or this is a copy with its own data folder.
// startMenuMu keeps the Start menu entry and the link registration from being written while Uninstall removes them
// (Uninstall right after a start would otherwise race with keepStartMenu and leave them behind).
var (
	startMenuMu  sync.Mutex
	uninstalling bool
)

func keepStartMenu() {
	startMenuMu.Lock()
	defer startMenuMu.Unlock()
	appSetMu.Lock()
	no := appSet.NoStartMenu
	appSetMu.Unlock()
	if no || uninstalling || customRoot || runtime.GOOS != "windows" {
		return
	}
	if err := startMenuSet(true); err != nil {
		logf("start menu: %v", err)
	}
	if err := registerLinkScheme(); err != nil {
		logf("%s:// link: %v", appScheme, err)
	}
}

func clipboardOn() bool {
	appSetMu.Lock()
	defer appSetMu.Unlock()
	return appSet.Clipboard
}

// fileExts are the endings that make a copied link worth offering.
var fileExts = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`zip rar 7z gz tgz bz2 xz zst tar iso img dmg exe msi msix appx appxbundle apk xapk
		deb rpm appimage pkg jar bin cab torrent mp4 mkv avi mov wmv flv webm m4v mpg mpeg ts mp3 flac wav aac ogg
		opus m4a wma pdf epub mobi djvu doc docx xls xlsx ppt pptx odt ods odp csv psd ai svgz ttf otf woff2 vhd vhdx
		qcow2 vmdk ova ovf`) {
		fileExts["."+e] = true
	}
}

// copiedLink returns the download link in copied text, or "" when there is none worth offering. Only a single link
// (the whole copied text, trimmed) counts: a paragraph that happens to contain a link is not a request to download.
func copiedLink(text string) string {
	t := strings.TrimSpace(text)
	if t == "" || len(t) > 4096 || strings.ContainsAny(t, " \t\r\n") {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(t), "magnet:?") && strings.Contains(strings.ToLower(t), "xt=urn:bt") {
		return t
	}
	u, err := url.Parse(t)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ftp") || u.Host == "" {
		return ""
	}
	if fileExts[strings.ToLower(path.Ext(u.Path))] {
		return t
	}
	return ""
}

// clipOffer decides what to do with copied text: the link to offer, or "". It remembers the last link so copying
// the same link again (or the clipboard being re-announced) does not repeat the notification.
type clipOffer struct {
	mu   sync.Mutex
	last string
}

func (c *clipOffer) take(text string, ourWindowInFront bool) string {
	l := copiedLink(text)
	c.mu.Lock()
	defer c.mu.Unlock()
	if l == "" || l == c.last {
		return ""
	}
	c.last = l
	if ourWindowInFront {
		return "" // copied inside our own window (Copy link): it already knows
	}
	return l
}

var clipOffers clipOffer

// pendingAdd is a link the person chose from a notification; the window's next refresh picks it up and opens the
// Add dialog with it (once).
var (
	pendingMu  sync.Mutex
	pendingURL string
)

func setPendingAdd(u string) {
	pendingMu.Lock()
	pendingURL = u
	pendingMu.Unlock()
}

func takePendingAdd() string {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	u := pendingURL
	pendingURL = ""
	if u != "" {
		logf("add dialog: a link was handed to the window")
	}
	return u
}

// balloonLink is the link the notification on screen offers ("" for every other notification), so a click on it
// can tell "add this link" from "open the window".
var (
	balloonMu   sync.Mutex
	balloonLink string
)

func setBalloonLink(u string) {
	balloonMu.Lock()
	balloonLink = u
	balloonMu.Unlock()
}

func takeBalloonLink() string {
	balloonMu.Lock()
	defer balloonMu.Unlock()
	u := balloonLink
	balloonLink = ""
	return u
}

// offerCopiedLink is called (on Windows, from the clipboard listener) with newly copied text.
func offerCopiedLink(text string, ourWindowInFront bool) {
	if !clipboardOn() {
		return
	}
	l := clipOffers.take(text, ourWindowInFront)
	if l == "" {
		return
	}
	name := l
	if u, err := url.Parse(l); err == nil && u.Scheme != "magnet" {
		if b := path.Base(u.Path); b != "" && b != "/" && b != "." {
			if d, err := url.PathUnescape(b); err == nil {
				b = d
			}
			name = b
		}
	} else {
		name = "a magnet link"
	}
	if len(name) > 120 {
		name = name[:117] + "..."
	}
	logf("copied link offered: %s", l)
	if toastsUsable() {
		key := offerKey(l)
		if tok, err := toastTokens.issue(key, "add"); err == nil {
			x := `<toast activationType="protocol" launch="` + xmlEsc(appScheme+"://add?item="+key+"&t="+tok) + `"><visual><binding template="ToastGeneric">` +
				`<text>` + xmlEsc(tr("Download this with "+AppName+"?")) + `</text><text>` + xmlEsc(name) + `</text></binding></visual>` +
				`<actions><action content="` + xmlEsc(tr("Download…")) + `" activationType="protocol" arguments="` + xmlEsc(appScheme+"://add?item="+key+"&t="+tok) + `"/></actions></toast>`
			if showToastXML(x) == nil {
				return
			}
		}
	}
	showBalloon(tr("Download this with "+AppName+"?"), name+"\n"+tr("Click here to add it."))
	setBalloonLink(l)
}

// offered links, by a short key that goes into the notification instead of the link itself
var (
	offerMu   sync.Mutex
	offerSeq  int
	offerKeys = map[string]string{}
)

func offerKey(l string) string {
	offerMu.Lock()
	defer offerMu.Unlock()
	offerSeq++
	k := fmt.Sprintf("c%d", offerSeq)
	if len(offerKeys) > 50 {
		offerKeys = map[string]string{}
	}
	offerKeys[k] = l
	return k
}

func offeredLink(key string) string {
	offerMu.Lock()
	defer offerMu.Unlock()
	return offerKeys[key]
}

// isWebLink accepts any http, https or ftp link with a host, or a magnet link (for the command line, where the
// person chose the link, so it need not look like a file).
func isWebLink(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) > 8192 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	if strings.HasPrefix(strings.ToLower(s), "magnet:?") {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "ftp") && u.Host != ""
}

// balloonView is the list view the notification on screen leads to ("finished" or "errors"), and pendingView the
// view the window should switch to on its next refresh after the notification was clicked.
var (
	balloonViewV string
	pendingViewV string
)

func setBalloonView(v string) {
	balloonMu.Lock()
	balloonViewV = v
	balloonMu.Unlock()
}

// balloonClicked is called when the person clicks the notification.
func balloonClicked() {
	l := takeBalloonLink()
	balloonMu.Lock()
	v := balloonViewV
	balloonViewV = ""
	balloonMu.Unlock()
	if l != "" {
		setPendingAdd(l)
	} else if v != "" {
		pendingMu.Lock()
		pendingViewV = v
		pendingMu.Unlock()
	}
}

func takePendingView() string {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	v := pendingViewV
	pendingViewV = ""
	return v
}
