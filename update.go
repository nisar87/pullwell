package main

// Updates: install once, then the program updates itself.
//
// Where updates come from:
//   - "local": a file <Name>-update.exe next to the program, with <Name>-update.exe.sha256 beside it ("<sha256>
//     [version]"). Whoever can write that folder could already replace the program itself, so this adds no new way
//     in; the fingerprint only proves the file arrived complete. Used while testing (the developer drops builds there).
//   - GitHub: the newest release of appRepo ("release" channel), or the pre-release "test-build" ("test" channel).
//     The file is checked against the SHA-256 fingerprint published with it. A private repository cannot be read
//     without signing in, so this works once the releases are public.
//
// Installing never uses a script: the running program renames its own file to <Name>-previous.exe (Windows allows
// renaming a running program), moves the new file into its place, starts it and quits. The new copy waits for the
// old one to end before it starts. The previous version stays next to it, and "Go back" swaps it in the same way.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pullwell/dm"
)

var updateAPI = "https://api.github.com" // tests point it at a local server

type updateState struct {
	Current   string `json:"current"`
	Build     string `json:"build"`
	Channel   string `json:"channel"` // "release" | "test"
	Auto      bool   `json:"auto"`    // install by itself when nothing is downloading
	Available bool   `json:"available"`
	Latest    string `json:"latest"` // version (or build) on offer
	Source    string `json:"source"` // "local" | "github"
	Notes     string `json:"notes"`
	Page      string `json:"page"`
	Checked   int64  `json:"checked"`
	Status    string `json:"status"` // "idle" | "checking" | "downloading" | "installing"
	Progress  int    `json:"progress"`
	Error     string `json:"error"`
	Previous  bool   `json:"previous"` // a previous version is kept and can be put back

	asset, sha, localFile string
}

type updateConfig struct {
	Channel string `json:"channel"`
	NoAuto  bool   `json:"noAuto"`
	// Skip is the fingerprint of a version the person went back from: it is never installed by itself again
	// (it can still be installed by hand), so "Go back" sticks until a different version appears.
	Skip string `json:"skip,omitempty"`
}

var (
	updMu   sync.Mutex
	upd     = updateState{Status: "idle"}
	updCfg  updateConfig
	selfSHA string // fingerprint of the running program, to recognise "the same build"
	// applySwap installs a verified file; replaced in tests.
	applySwap = swapAndRestart
)

func updateCfgFile() string { return filepath.Join(appRoot, "data", "update.json") }

func loadUpdateCfg() {
	updCfg = updateConfig{Channel: "release"}
	if b, err := os.ReadFile(updateCfgFile()); err == nil {
		_ = json.Unmarshal(b, &updCfg)
	}
	if updCfg.Channel != "test" {
		updCfg.Channel = "release"
	}
}

func saveUpdateCfg() error {
	b, _ := json.MarshalIndent(updCfg, "", "  ")
	return os.WriteFile(updateCfgFile(), b, 0o644)
}

func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

func localUpdateFile() string { return filepath.Join(filepath.Dir(exePath()), AppName+"-update.exe") }
func previousFile() string    { return filepath.Join(filepath.Dir(exePath()), AppName+"-previous.exe") }

func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func updateSnapshot() updateState {
	updMu.Lock()
	defer updMu.Unlock()
	u := upd
	u.Current, u.Build, u.Channel, u.Auto = appVersion, appBuild, updCfg.Channel, !updCfg.NoAuto
	_, err := os.Stat(previousFile())
	u.Previous = err == nil
	return u
}

// versionParts turns "v2.10.1" into [2 10 1]; newerVersion reports whether a is newer than b.
func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

func newerVersion(a, b string) bool {
	x, y := versionParts(a), versionParts(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			return p > q
		}
	}
	return false
}

// readSHAFile reads "<64 hex> [version]" (the version is optional and only shown).
func readSHAFile(path string) (sha, version string, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 4096 {
		return "", "", false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 || len(f[0]) != 64 {
		return "", "", false
	}
	if _, err := hex.DecodeString(f[0]); err != nil {
		return "", "", false
	}
	if len(f) > 1 && !strings.HasSuffix(strings.ToLower(f[1]), ".exe") {
		version = f[1]
	}
	return strings.ToLower(f[0]), version, true
}

// checkLocal looks for a dropped update next to the program. It is offered when its fingerprint file matches it and
// it is not the build that is already running.
func checkLocal() bool {
	file := localUpdateFile()
	sha, ver, ok := readSHAFile(file + ".sha256")
	if !ok {
		return false
	}
	got, err := fileSHA(file)
	if err != nil || got != sha || got == selfSHA {
		return false
	}
	if ver == "" {
		ver = "new build " + sha[:8]
	}
	updMu.Lock()
	changed := !upd.Available || upd.localFile != file || upd.sha != sha
	upd.Available, upd.Source, upd.Latest, upd.localFile, upd.sha, upd.asset, upd.Notes, upd.Page = true, "local", ver, file, sha, "", "", ""
	upd.Checked, upd.Error = time.Now().Unix(), ""
	updMu.Unlock()
	if changed {
		logf("update: %s found next to the program (%s)", filepath.Base(file), ver)
	}
	return true
}

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Name   string `json:"name"`
	Body   string `json:"body"`
	Page   string `json:"html_url"`
	Pre    bool   `json:"prerelease"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func httpGet(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", AppName+"/"+appVersion)
	req.Header.Set("Accept", "application/vnd.github+json")
	return updClient.Do(req)
}

// updClient goes through the same proxy as the downloads.
var updClient = &http.Client{Transport: &http.Transport{Proxy: proxyFor, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}}

// checkGitHub asks GitHub for the release of the chosen channel. It never changes anything on disk.
func checkGitHub(manual bool) {
	updMu.Lock()
	channel := updCfg.Channel
	updMu.Unlock()
	fail := func(msg string) {
		updMu.Lock()
		if manual {
			upd.Error = msg
		}
		updMu.Unlock()
		logf("update check: %s", msg)
	}
	path := "/repos/" + appRepo + "/releases/latest"
	if channel == "test" {
		path = "/repos/" + appRepo + "/releases/tags/test-build"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := httpGet(ctx, updateAPI+path)
	if err != nil {
		fail("Could not reach GitHub.")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		fail("No release found on GitHub. The project may still be private.")
		return
	}
	if resp.StatusCode != 200 {
		fail(fmt.Sprintf("GitHub answered HTTP %d.", resp.StatusCode))
		return
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil || rel.Tag == "" {
		fail("Could not read the release information.")
		return
	}
	var asset, sha, shaURL string
	for _, a := range rel.Assets {
		switch {
		case strings.EqualFold(a.Name, AppName+".exe"):
			asset = a.URL
			if d := strings.ToLower(a.Digest); strings.HasPrefix(d, "sha256:") {
				sha = strings.TrimPrefix(d, "sha256:")
			}
		case strings.EqualFold(a.Name, AppName+".exe.sha256"):
			shaURL = a.URL
		}
	}
	if sha == "" && shaURL != "" && okDownloadHost(shaURL) {
		c2, cc := context.WithTimeout(context.Background(), 20*time.Second)
		if r2, err := httpGet(c2, shaURL); err == nil {
			b, _ := io.ReadAll(io.LimitReader(r2.Body, 4096))
			r2.Body.Close()
			if f := strings.Fields(string(b)); len(f) > 0 && len(f[0]) == 64 {
				sha = strings.ToLower(f[0])
			}
		}
		cc()
	}
	var newer bool
	latest := strings.TrimPrefix(rel.Tag, "v")
	if channel == "test" {
		newer = sha != "" && sha != selfSHA // a test build is "new" whenever it is a different build
		latest = rel.Name
	} else {
		newer = newerVersion(rel.Tag, appVersion) && !rel.Pre
	}
	updMu.Lock()
	upd.Checked, upd.Error = time.Now().Unix(), ""
	if upd.Source == "local" && upd.Available {
		updMu.Unlock() // a dropped file wins
		return
	}
	upd.Available = newer && asset != "" && len(sha) == 64
	upd.Source, upd.Latest, upd.Notes, upd.Page, upd.asset, upd.sha, upd.localFile = "github", latest, rel.Body, rel.Page, asset, sha, ""
	updMu.Unlock()
}

func okDownloadHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" || strings.HasSuffix(h, ".github.com") || strings.HasSuffix(h, ".githubusercontent.com")
}

// installUpdate gets the update (downloads it, or takes the dropped file), verifies it and installs it.
func installUpdate() error {
	updMu.Lock()
	if !upd.Available || upd.Status != "idle" {
		updMu.Unlock()
		return errors.New("there is no update to install")
	}
	src, asset, sha, local := upd.Source, upd.asset, upd.sha, upd.localFile
	upd.Status, upd.Progress, upd.Error = "downloading", 0, ""
	updMu.Unlock()
	fail := func(err error) error {
		updMu.Lock()
		upd.Status, upd.Error = "idle", err.Error()
		updMu.Unlock()
		logf("update failed: %v", err)
		return err
	}
	dir := filepath.Join(appRoot, "data", "update")
	_ = os.MkdirAll(dir, 0o755)
	dst := filepath.Join(filepath.Dir(exePath()), AppName+"-new.exe") // same folder as the program: the swap is a rename
	_ = os.Remove(dst)
	if !writableDir(filepath.Dir(exePath())) {
		return fail(fmt.Errorf("%s is in a folder Windows protects (%s), so it cannot replace itself. Move %s.exe to a folder of your own, for example in your user folder, and it updates itself from then on", AppName, filepath.Dir(exePath()), AppName))
	}
	if src == "local" {
		if err := copyFile(local, dst); err != nil {
			return fail(err)
		}
	} else {
		if !okDownloadHost(asset) && os.Getenv("PULLWELL_TEST_UPDATES") == "" {
			return fail(errors.New("the download address is not a GitHub address"))
		}
		if err := download(asset, dst); err != nil {
			_ = os.Remove(dst)
			return fail(err)
		}
	}
	if err := verifyProgram(dst, sha); err != nil {
		_ = os.Remove(dst)
		return fail(err)
	}
	updMu.Lock()
	upd.Status, upd.Progress = "installing", 100
	updMu.Unlock()
	logf("update: verified %s; installing", filepath.Base(dst))
	if err := applySwap(dst); err != nil {
		_ = os.Remove(dst)
		return fail(err)
	}
	if src == "local" {
		_ = os.Remove(local) // used: do not offer it again
		_ = os.Remove(local + ".sha256")
	}
	return nil
}

// rollBack puts the previous version back (the same swap; the current one becomes the previous).
func rollBack() error {
	prev := previousFile()
	if _, err := os.Stat(prev); err != nil {
		return errors.New("there is no previous version")
	}
	dst := filepath.Join(filepath.Dir(exePath()), AppName+"-new.exe")
	_ = os.Remove(dst)
	if err := copyFile(prev, dst); err != nil {
		return err
	}
	if err := verifyProgram(dst, ""); err != nil {
		_ = os.Remove(dst)
		return err
	}
	logf("update: going back to the previous version")
	updMu.Lock()
	updCfg.Skip = selfSHA
	_ = saveUpdateCfg()
	updMu.Unlock()
	_ = os.WriteFile(wentBackFile(), []byte(appVersion+"+"+appBuild), 0o644)
	if err := applySwap(dst); err != nil {
		_ = os.Remove(wentBackFile())
		return err
	}
	return nil
}

// wentBackFile marks that the next start is the result of "Go back", so it says so instead of "updated".
func wentBackFile() string { return filepath.Join(appRoot, "data", "went-back") }

// takeWentBack reports (once) whether this start came from "Go back".
func takeWentBack() bool {
	if _, err := os.Stat(wentBackFile()); err != nil {
		return false
	}
	_ = os.Remove(wentBackFile())
	return true
}

func download(u, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	resp, err := httpGet(ctx, u)
	if err != nil {
		return fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	var got int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			got += int64(n)
			if resp.ContentLength > 0 {
				updMu.Lock()
				upd.Progress = int(got * 100 / resp.ContentLength)
				updMu.Unlock()
			}
		}
		if rerr == io.EOF {
			return f.Sync()
		}
		if rerr != nil {
			return fmt.Errorf("download interrupted: %v", rerr)
		}
	}
}

// verifyProgram: the file must match its fingerprint (when one is given) and look like a Windows program.
func verifyProgram(path, sha string) error {
	if sha != "" {
		got, err := fileSHA(path)
		if err != nil {
			return err
		}
		if got != sha {
			return errors.New("the update does not match its fingerprint, so it was discarded")
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 2)
	st, _ := f.Stat()
	if _, err := io.ReadFull(f, head); err != nil || st == nil || st.Size() < 1<<20 || head[0] != 'M' || head[1] != 'Z' {
		return errors.New("the update is not a program")
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// updateLoop: the dropped file is looked for every 15 seconds, GitHub every 6 hours. With automatic updates on, an
// update is installed when nothing is downloading (unfinished downloads continue after the restart).
func updateLoop() {
	if exe := exePath(); exe != "" {
		selfSHA, _ = fileSHA(exe)
	}
	lastGH := time.Now().Add(-6*time.Hour + 2*time.Minute) // first GitHub check 2 minutes after start
	notified := ""
	failedSHA, failedAt := "", time.Time{}
	for {
		local := checkLocal()
		if !local && time.Since(lastGH) > 6*time.Hour {
			lastGH = time.Now()
			checkGitHub(false)
		}
		u := updateSnapshot()
		if u.Available && u.Status == "idle" {
			skipped := u.sha != "" && u.sha == updateSkip()
			retryLater := u.sha == failedSHA && time.Since(failedAt) < time.Hour // a failed install is tried again after an hour
			if u.Auto && !skipped && !retryLater && idleForUpdate() {
				logf("update: installing %s by itself (nothing is downloading)", u.Latest)
				if installUpdate() != nil {
					failedSHA, failedAt = u.sha, time.Now()
				}
			} else if notified != u.Latest && !skipped {
				notified = u.Latest
				showBalloon(AppName+" update", "Version "+u.Latest+" is ready. Open "+AppName+" to install it.")
			}
		}
		time.Sleep(15 * time.Second)
	}
}

func updateSkip() string {
	updMu.Lock()
	defer updMu.Unlock()
	return updCfg.Skip
}

func idleForUpdate() bool {
	b := dm.Brief()
	if b == nil {
		return true
	}
	active, _ := b["active"].(int)
	return active == 0
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		var in struct {
			Action  string `json:"action"`
			Channel string `json:"channel"`
			Auto    *bool  `json:"auto"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad request"})
			return
		}
		var err error
		switch in.Action {
		case "check":
			if !checkLocal() {
				checkGitHub(true)
			}
		case "install":
			err = installUpdate()
		case "rollback":
			err = rollBack()
		case "settings":
			updMu.Lock()
			if in.Channel == "test" || in.Channel == "release" {
				if in.Channel != updCfg.Channel {
					upd.Available = upd.Source == "local" && upd.Available // a GitHub offer was for the other channel
				}
				updCfg.Channel = in.Channel
			}
			if in.Auto != nil {
				updCfg.NoAuto = !*in.Auto
			}
			err = saveUpdateCfg()
			updMu.Unlock()
		default:
			err = errors.New("unknown action")
		}
		if err != nil {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(updateSnapshot())
}
