package main

import "testing"

func TestVerAtLeast(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"120.0.2210.91", webview2MinVersion, true},
		{"86.0.616.0", webview2MinVersion, true},
		{"86.0.615.99", webview2MinVersion, false},
		{"85.9.999.999", webview2MinVersion, false},
		{"130.0.0.0", "130.0.0.1", false},
		{"", webview2MinVersion, false},
		{"120.0.x.1", webview2MinVersion, false},
		{"120.0.1", webview2MinVersion, false},
		{"-1.0.0.0", "0.0.0.0", false},
	} {
		if got := verAtLeast(c.a, c.b); got != c.want {
			t.Errorf("verAtLeast(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestSafeExternalURL(t *testing.T) {
	for u, want := range map[string]bool{
		"https://github.com/x":         true,
		"HTTP://example.com":           true,
		"file:///C:/Windows/calc.exe":  false,
		"C:\\Windows\\calc.exe":        false,
		"ms-settings:":                 false,
		"javascript:alert(1)":          false,
		"https://x.com/\" --malicious": false,
		"":                             false,
	} {
		if got := safeExternalURL(u); got != want {
			t.Errorf("safeExternalURL(%q) = %v, want %v", u, got, want)
		}
	}
}
