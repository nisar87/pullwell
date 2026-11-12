package main

import (
	"net/url"
	"strings"
)

// progressWindowRoute recognizes only the standalone UI on this instance's
// literal origin and root path. Never pass extra query/fragment state into a
// native window. dm/dl_manager.go and dm/dl_torrent.go issue d<seconds>-<seq> IDs.
func progressWindowRoute(base, raw string) (string, bool) {
	if len(raw) > 2048 || strings.Contains(raw, "#") {
		return "", false
	}
	b, err := url.Parse(base)
	if err != nil || b.Scheme != "http" || b.Host == "" || b.User != nil || b.Path != "/" || b.RawPath != "" || b.Opaque != "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil || u.Opaque != "" || u.Path != "/" || u.RawPath != "" {
		return "", false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["progress"]) != 1 {
		return "", false
	}
	id := q.Get("progress")
	if len(id) < 4 || len(id) > 40 || id[0] != 'd' {
		return "", false
	}
	stamp, seq, ok := strings.Cut(id[1:], "-")
	if !ok || len(stamp) == 0 || len(stamp) > 19 || len(seq) == 0 || len(seq) > 19 {
		return "", false
	}
	for _, part := range []string{stamp, seq} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return "", false
			}
		}
	}
	return id, true
}
