package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "0.1.0", true}, {"0.1.1", "0.1.0", true}, {"0.1.0", "0.1.0", false}, {"0.1.0", "0.2.0", false},
		{"1.0", "0.9.9", true}, {"v0.10.0", "0.9.0", true}, {"0.1.0-rc1", "0.1.0", false},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestRestartArgs(t *testing.T) {
	got := restartArgs([]string{"-root", "D:\\x", "-tray", "-wait-pid=5", "-updated", "0.0.9+abc", "-debug"}, 42, true)
	want := []string{"-root", "D:\\x", "-debug", "-wait-pid=42", "-updated=" + appVersion + "+" + appBuild}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := restartArgs(nil, 7, false); got[len(got)-1] != "-tray" {
		t.Fatalf("window was closed, so the new copy must start in the tray: %q", got)
	}
}

func TestReadSHAFile(t *testing.T) {
	dir := t.TempDir()
	sha := strings.Repeat("ab", 32)
	for body, want := range map[string][2]string{
		sha + "\n":               {sha, ""},
		sha + "  0.2.0\n":        {sha, "0.2.0"},
		sha + "  Pullwell.exe\n": {sha, ""}, // the sha256sum format: a file name is not a version
		strings.ToUpper(sha):     {sha, ""},
		"nonsense":               {"", ""},
		sha[:60]:                 {"", ""},
		strings.Repeat("zz", 32): {"", ""},
	} {
		p := filepath.Join(dir, "x.sha256")
		os.WriteFile(p, []byte(body), 0o644)
		s, v, ok := readSHAFile(p)
		if ok != (want[0] != "") || s != want[0] || v != want[1] {
			t.Errorf("readSHAFile(%q) = %q, %q, %v", body, s, v, ok)
		}
	}
}

// fakeProgram makes a file that passes verifyProgram: "MZ" and at least 1 MB.
func fakeProgram(t *testing.T, path string, mark byte) string {
	b := make([]byte, 1<<20+10)
	b[0], b[1], b[2] = 'M', 'Z', mark
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func resetUpdate(t *testing.T) (swapped *string) {
	t.Helper()
	appRoot = t.TempDir()
	os.MkdirAll(filepath.Join(appRoot, "data"), 0o755)
	upd = updateState{Status: "idle"}
	updCfg = updateConfig{Channel: "release"}
	var got string
	old := applySwap
	applySwap = func(f string) error { got = f; return nil }
	t.Cleanup(func() {
		applySwap = old
		os.Remove(localUpdateFile())
		os.Remove(localUpdateFile() + ".sha256")
		os.Remove(filepath.Join(filepath.Dir(exePath()), AppName+"-new.exe"))
	})
	return &got
}

func TestLocalUpdate(t *testing.T) {
	swapped := resetUpdate(t)
	selfSHA = "0000"
	if checkLocal() {
		t.Fatal("offered an update with no file")
	}
	sha := fakeProgram(t, localUpdateFile(), 1)
	if checkLocal() {
		t.Fatal("offered a file without its fingerprint file")
	}
	os.WriteFile(localUpdateFile()+".sha256", []byte(strings.Repeat("0", 64)+" 0.2.0"), 0o644)
	if checkLocal() {
		t.Fatal("offered a file that does not match its fingerprint (still being copied, or damaged)")
	}
	os.WriteFile(localUpdateFile()+".sha256", []byte(sha+" 0.2.0"), 0o644)
	selfSHA = sha
	if checkLocal() {
		t.Fatal("offered the build that is already running")
	}
	selfSHA = "0000"
	if !checkLocal() {
		t.Fatal("a good dropped update was not offered")
	}
	if u := updateSnapshot(); !u.Available || u.Source != "local" || u.Latest != "0.2.0" {
		t.Fatalf("snapshot %+v", u)
	}
	if err := installUpdate(); err != nil {
		t.Fatal(err)
	}
	if got, _ := fileSHA(*swapped); got != sha {
		t.Fatalf("installed %q (sha %s), want the dropped file", *swapped, got)
	}
	if _, err := os.Stat(localUpdateFile()); !os.IsNotExist(err) {
		t.Fatal("the used update file was left behind (it would be offered again)")
	}
}

func TestGitHubUpdate(t *testing.T) {
	swapped := resetUpdate(t)
	selfSHA = "0000"
	os.Setenv("PULLWELL_TEST_UPDATES", "1")
	defer os.Unsetenv("PULLWELL_TEST_UPDATES")
	dir := t.TempDir()
	good := fakeProgram(t, filepath.Join(dir, "good.exe"), 2)
	var srv *httptest.Server
	digest := "sha256:" + good
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + appRepo + "/releases/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v9.9.9", "name": "9.9.9", "body": "notes", "html_url": "https://github.com/x",
				"assets": []map[string]any{{"name": AppName + ".exe", "browser_download_url": srv.URL + "/dl", "digest": digest}}})
		case "/dl":
			http.ServeFile(w, r, filepath.Join(dir, "good.exe"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	updateAPI = srv.URL
	defer func() { updateAPI = "https://api.github.com" }()

	checkGitHub(true)
	u := updateSnapshot()
	if !u.Available || u.Latest != "9.9.9" || u.Source != "github" {
		t.Fatalf("snapshot %+v", u)
	}
	if err := installUpdate(); err != nil {
		t.Fatal(err)
	}
	if got, _ := fileSHA(*swapped); got != good {
		t.Fatal("the installed file is not the downloaded one")
	}

	// a download that does not match its fingerprint is thrown away and nothing is installed
	*swapped = ""
	upd = updateState{Status: "idle"}
	digest = "sha256:" + strings.Repeat("1", 64)
	checkGitHub(true)
	if err := installUpdate(); err == nil || !strings.Contains(err.Error(), "fingerprint") || *swapped != "" {
		t.Fatalf("err %v, swapped %q", err, *swapped)
	}

	// the test channel offers a different build even with the same version
	upd = updateState{Status: "idle"}
	updCfg.Channel = "test"
	checkGitHub(true)
	if u := updateSnapshot(); u.Available {
		t.Fatal("test channel: no test-build release exists, nothing must be offered")
	}
	if !strings.Contains(updateSnapshot().Error, "No release found") {
		t.Fatalf("error %q", updateSnapshot().Error)
	}
}

func TestUpdateRefusesNonGitHubDownloads(t *testing.T) {
	resetUpdate(t)
	upd = updateState{Status: "idle", Available: true, Source: "github", asset: "https://evil.example/Pullwell.exe", sha: strings.Repeat("a", 64)}
	if err := installUpdate(); err == nil || !strings.Contains(err.Error(), "GitHub") {
		t.Fatalf("err %v", err)
	}
}

func TestVerifyProgram(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.exe")
	os.WriteFile(p, []byte("MZ tiny"), 0o644)
	if verifyProgram(p, "") == nil {
		t.Fatal("a tiny file passed")
	}
	os.WriteFile(p, append([]byte("<html>"), make([]byte, 2<<20)...), 0o644)
	if verifyProgram(p, "") == nil {
		t.Fatal("a web page passed")
	}
	sha := fakeProgram(t, p, 3)
	if err := verifyProgram(p, sha); err != nil {
		t.Fatal(err)
	}
	_ = fmt.Sprint()
}

func TestRollBackRemembersTheVersionLeft(t *testing.T) {
	swapped := resetUpdate(t)
	if err := rollBack(); err == nil {
		t.Fatal("went back without a previous version")
	}
	prev := previousFile()
	sha := fakeProgram(t, prev, 7)
	t.Cleanup(func() { os.Remove(prev) })
	selfSHA = "the-running-build"
	if err := rollBack(); err != nil {
		t.Fatal(err)
	}
	if got, _ := fileSHA(*swapped); got != sha {
		t.Fatal("did not install the previous version")
	}
	if updCfg.Skip != "the-running-build" {
		t.Fatalf("Skip = %q: the version gone back from would be installed by itself again", updCfg.Skip)
	}
	loadUpdateCfg()
	if updCfg.Skip != "the-running-build" {
		t.Fatal("Skip was not saved")
	}
	if !takeWentBack() || takeWentBack() {
		t.Fatal("the next start should say once that it went back")
	}
}
