package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopiedLink(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/files/setup.exe":              "https://example.com/files/setup.exe",
		"  https://example.com/a/b/movie.MKV?x=1 \n":       "https://example.com/a/b/movie.MKV?x=1",
		"ftp://ftp.example.org/pub/linux.iso":              "ftp://ftp.example.org/pub/linux.iso",
		"magnet:?xt=urn:btih:abcdef&dn=x":                  "magnet:?xt=urn:btih:abcdef&dn=x",
		"https://example.com/":                             "", // a page, not a file
		"https://example.com/article.html":                 "",
		"see https://example.com/setup.exe for details":    "", // text that contains a link
		"https://example.com/a.zip\nhttps://example.com/b": "",
		"file:///C:/Windows/notepad.exe":                   "",
		"javascript:alert(1)//x.exe":                       "",
		"https:///nohost.zip":                              "",
		"magnet:?dn=no-hash":                               "",
	} {
		if got := copiedLink(in); got != want {
			t.Errorf("copiedLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClipOfferOncePerLink(t *testing.T) {
	var c clipOffer
	if c.take("https://x.org/a.zip", false) == "" {
		t.Fatal("first copy not offered")
	}
	if c.take("https://x.org/a.zip", false) != "" {
		t.Fatal("the same link was offered twice")
	}
	if c.take("https://x.org/b.zip", true) != "" {
		t.Fatal("offered a link copied inside our own window")
	}
	if c.take("https://x.org/b.zip", false) != "" {
		t.Fatal("a link copied in our window was offered when announced again")
	}
	if c.take("https://x.org/c.zip", false) == "" {
		t.Fatal("a new link was not offered")
	}
}

func TestClipboardSettingAndPendingAdd(t *testing.T) {
	appRoot = t.TempDir()
	os.MkdirAll(filepath.Join(appRoot, "data"), 0o755)
	loadAppSettings()
	if clipboardOn() {
		t.Fatal("copied links must be off by default")
	}
	offerCopiedLink("https://x.org/off.zip", false)
	if takeBalloonLink() != "" {
		t.Fatal("offered a link while the setting is off")
	}
	if err := setClipboardWatch(true); err != nil {
		t.Fatal(err)
	}
	loadAppSettings()
	if !clipboardOn() {
		t.Fatal("setting not saved")
	}
	offerCopiedLink("https://x.org/on.zip", false)
	l := takeBalloonLink()
	if l != "https://x.org/on.zip" {
		t.Fatalf("balloon link %q", l)
	}
	setPendingAdd(l)
	if takePendingAdd() != l || takePendingAdd() != "" {
		t.Fatal("the chosen link must be handed to the window exactly once")
	}
}
