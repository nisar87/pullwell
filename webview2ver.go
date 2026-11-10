package main

import (
	"strconv"
	"strings"
)

// webview2MinVersion is the oldest WebView2 runtime the window is built for (the open-source loaders use the same).
const webview2MinVersion = "86.0.616.0"

// parseVer reads "120.0.2210.91" into four numbers; ok is false for anything else.
func parseVer(s string) (v [4]int, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 4 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 1<<20 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// verAtLeast: is version a at least version b? Unreadable versions are never good enough.
func verAtLeast(a, b string) bool {
	x, ok1 := parseVer(a)
	y, ok2 := parseVer(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 4; i++ {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return true
}

// safeExternalURL: links the window hands to the default browser must be ordinary web addresses, never a file,
// a program or another app's link scheme.
func safeExternalURL(u string) bool {
	l := strings.ToLower(strings.TrimSpace(u))
	return (strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://")) && len(u) < 4096 && !strings.ContainsAny(u, "\"\r\n\x00")
}
