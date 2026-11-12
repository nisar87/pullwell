package main

import (
	"strings"
	"testing"
)

// These cases catch origin confusion, query smuggling, path aliases, and IDs
// that cannot be issued by dm's d<Unix seconds>-<sequence> generators.
func TestProgressWindowRoute(t *testing.T) {
	const base = "http://127.0.0.1:39281/"
	for _, tc := range []struct{ raw, want string }{
		{base + "?progress=d1794480000-7", "d1794480000-7"},
		{base + "?progress=d1-2", "d1-2"},
		{base + "?progress=%641-2", "d1-2"},
		{base + "?progress=d9223372036854775807-9223372036854775807", "d9223372036854775807-9223372036854775807"},
	} {
		id, ok := progressWindowRoute(base, tc.raw)
		if !ok || id != tc.want {
			t.Errorf("progressWindowRoute(%q) = %q, %v; want %q", tc.raw, id, ok, tc.want)
		}
	}
	for _, raw := range []string{
		"", "/?progress=d1-2", "//127.0.0.1:39281/?progress=d1-2",
		"https://127.0.0.1:39281/?progress=d1-2",
		"http://127.0.0.1:39282/?progress=d1-2",
		"http://localhost:39281/?progress=d1-2",
		"http://127.0.0.1:39281.evil.test/?progress=d1-2",
		"http://127.0.0.1:39281@evil.test/?progress=d1-2",
		"http://user@127.0.0.1:39281/?progress=d1-2",
		"http://127.0.0.1:39281?progress=d1-2",
		base + "other?progress=d1-2", base + "./?progress=d1-2",
		base + "%2f?progress=d1-2", base + "?progress=d1-2#view",
		base + "?progress=d1-2#", base + "?progress=d1-2&add=secret",
		base + "?progress=d1-2&progress=d3-4", base + "?progress=d1-2&progress=d1-2",
		base + "?progress=d1-2;add=secret", base + "?progress=d1-2&x=%zz",
		base + "?progress=", base + "?progress=d1-2%00",
		base + "?progress=other", base + "?progress=d-1", base + "?progress=d1-",
		base + "?progress=d1-2-3", base + "?progress=d1--2", base + "?progress=d1-+2",
		base + "?progress=d1-%32%2fetc", base + "?progress=d1-٢",
		base + "?progress=d" + strings.Repeat("1", 20) + "-1",
		base + "?progress=d1-" + strings.Repeat("1", 20),
		base + "?progress=" + strings.Repeat("a", 3000),
		" " + base + "?progress=d1-2", "javascript:alert(1)",
	} {
		if id, ok := progressWindowRoute(base, raw); ok || id != "" {
			t.Errorf("accepted %q as %q", raw, id)
		}
	}
}

func TestProgressWindowRouteRequiresValidBase(t *testing.T) {
	for _, base := range []string{"", "/", "http://user@127.0.0.1:39281/", "file:///", "http://127.0.0.1:39281/other"} {
		if id, ok := progressWindowRoute(base, "http://127.0.0.1:39281/?progress=d1-2"); ok || id != "" {
			t.Errorf("accepted base %q", base)
		}
	}
}
