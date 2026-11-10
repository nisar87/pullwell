package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The product name may be written only in appname.go, so that a rename stays a one-line change.
func TestNameOnlyInAppnameGo(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || path == "appname.go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), AppName) {
			t.Errorf("%s contains the product name %q; use AppName from appname.go instead", path, AppName)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVersionMatchesVersionFile(t *testing.T) {
	b, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if v := strings.TrimSpace(string(b)); v != appVersion {
		t.Fatalf("appVersion is %q but the VERSION file says %q", appVersion, v)
	}
}

func TestDerivedNames(t *testing.T) {
	for name, got := range map[string]string{"appID": appID, "appScheme": appScheme, "appHeader": appHeader, "appDataDir": appDataDir, "appRepo": appRepo} {
		if got == "" || strings.ContainsAny(got, " \t\r\n") {
			t.Errorf("%s = %q: must be non-empty and contain no spaces", name, got)
		}
	}
	if appScheme != strings.ToLower(appScheme) {
		t.Errorf("appScheme %q must be lower case (link schemes are)", appScheme)
	}
	if len(appID) > 129 {
		t.Errorf("appID is %d characters; Windows allows at most 129", len(appID))
	}
}
