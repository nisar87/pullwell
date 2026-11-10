package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseAppLink(t *testing.T) {
	tok := strings.Repeat("ab", linkTokenBytes)
	good := map[string]appLink{
		"pullwell://open?item=d1762741234-7&t=" + tok: {Verb: "open", ItemID: "d1762741234-7", Token: tok},
		"pullwell://open/?item=d1-2&t=" + tok:         {Verb: "open", ItemID: "d1-2", Token: tok},
		"PULLWELL://Reveal?item=d1-2&t=" + tok:        {Verb: "reveal", ItemID: "d1-2", Token: tok},
		"pullwell:show":                               {Verb: "show"},
		"pullwell://list?t=" + tok:                    {Verb: "list", Token: tok},
		"pullwell://cancel-shutdown?t=" + tok:         {Verb: "cancel-shutdown", Token: tok},
		" pullwell://show?item=d9-1 ":                 {Verb: "show", ItemID: "d9-1"},
	}
	for raw, want := range good {
		got, err := parseAppLink(raw)
		if err != nil || got != want {
			t.Errorf("parseAppLink(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}
	bad := []string{
		"",
		"https://open?item=d1-2",                              // wrong scheme
		"pullwell://run?item=d1-2",                            // unknown verb
		"pullwell://open/extra?item=d1-2",                     // extra path
		"pullwell://open?item=..%2F..%2Fwindows",              // item with path characters
		"pullwell://open?item=C:%5Cx.exe",                     // a path instead of an id
		"pullwell://open?item=d1-2&t=XYZ",                     // token not hex
		"pullwell://open?item=d1-2&t=" + tok[:30],             // token too short
		"pullwell://open?item=d1-2&t=" + strings.ToUpper(tok), // upper-case hex is not what we issue
		"pullwell://open?item=" + strings.Repeat("a", 65),     // item too long
		"pullwell://show?item=" + strings.Repeat("a", 600),    // whole link too long
	}
	for _, raw := range bad {
		if l, err := parseAppLink(raw); err == nil {
			t.Errorf("parseAppLink(%q) accepted: %+v", raw, l)
		}
	}
}

func TestLinkTokens(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := newLinkTokens(func() time.Time { return now })
	tok, err := s.issue("d1-2", "open", "reveal")
	if err != nil || len(tok) != 2*linkTokenBytes || !validHex(tok) {
		t.Fatalf("issue = %q, %v", tok, err)
	}
	if tok2, _ := s.issue("d1-2", "open"); tok2 == tok {
		t.Fatal("two tokens are the same")
	}
	cases := []struct {
		l    appLink
		want bool
	}{
		{appLink{Verb: "open", ItemID: "d1-2", Token: tok}, true},
		{appLink{Verb: "reveal", ItemID: "d1-2", Token: tok}, true},
		{appLink{Verb: "list", ItemID: "d1-2", Token: tok}, false}, // verb not granted
		{appLink{Verb: "open", ItemID: "d1-3", Token: tok}, false}, // other item
		{appLink{Verb: "open", ItemID: "d1-2", Token: ""}, false},  // no token
		{appLink{Verb: "open", ItemID: "d1-2", Token: strings.Repeat("0", 32)}, false},
	}
	for _, c := range cases {
		if got := s.allows(c.l); got != c.want {
			t.Errorf("allows(%+v) = %v, want %v", c.l, got, c.want)
		}
	}
	now = now.Add(linkTokenLife + time.Second)
	if s.allows(appLink{Verb: "open", ItemID: "d1-2", Token: tok}) {
		t.Error("token still valid after 24 hours")
	}
}

func TestLinkTokensLimit(t *testing.T) {
	s := newLinkTokens(nil)
	first, _ := s.issue("a", "open")
	for i := 0; i < linkMaxTokens; i++ {
		s.issue("b", "open")
	}
	if len(s.list) != linkMaxTokens {
		t.Fatalf("%d tokens kept, want %d", len(s.list), linkMaxTokens)
	}
	if s.allows(appLink{Verb: "open", ItemID: "a", Token: first}) {
		t.Error("oldest token not dropped")
	}
}

func TestDecideLink(t *testing.T) {
	s := newLinkTokens(nil)
	files := map[string]string{"d1-1": "photo.jpg", "d1-2": "setup.exe", "d1-3": "Setup.EXE. "}
	name := func(id string) string { return files[id] }
	tok := func(item string, verbs ...string) string { x, _ := s.issue(item, verbs...); return x }

	cases := []struct {
		name string
		l    appLink
		do   string
		item string
	}{
		{"show needs no token", appLink{Verb: "show", ItemID: "d1-1"}, "show", "d1-1"},
		{"open with token", appLink{Verb: "open", ItemID: "d1-1", Token: tok("d1-1", "open")}, "open", "d1-1"},
		{"reveal with token", appLink{Verb: "reveal", ItemID: "d1-2", Token: tok("d1-2", "reveal")}, "reveal", "d1-2"},
		{"open without token only shows", appLink{Verb: "open", ItemID: "d1-1"}, "show", ""},
		{"web page guessing an id", appLink{Verb: "open", ItemID: "d1-2", Token: strings.Repeat("1", 32)}, "show", ""},
		{"program file is never opened", appLink{Verb: "open", ItemID: "d1-2", Token: tok("d1-2", "open")}, "show", "d1-2"},
		{"program file with trailing dot", appLink{Verb: "open", ItemID: "d1-3", Token: tok("d1-3", "open")}, "show", "d1-3"},
		{"deleted item", appLink{Verb: "open", ItemID: "gone", Token: tok("gone", "open")}, "show", ""},
		{"list", appLink{Verb: "list", Token: tok("", "list")}, "list", ""},
		{"cancel shutdown", appLink{Verb: "cancel-shutdown", Token: tok("", "cancel-shutdown")}, "cancel-shutdown", ""},
		{"cancel shutdown without token", appLink{Verb: "cancel-shutdown"}, "show", ""},
	}
	for _, c := range cases {
		d := decideLink(c.l, s, name)
		if d.Do != c.do || d.ItemID != c.item {
			t.Errorf("%s: got %+v, want do=%s item=%q", c.name, d, c.do, c.item)
		}
		if d.Do == "show" && c.l.Verb != "show" && d.Why == "" {
			t.Errorf("%s: reduced to show without a reason for the log", c.name)
		}
	}
}

func TestIsProgramFile(t *testing.T) {
	for name, want := range map[string]bool{
		"setup.exe": true, "SETUP.MSI": true, "run.bat": true, "x.ps1": true, "a.lnk": true, "evil.exe. ": true,
		"movie.mp4": false, "archive.zip": false, "doc.pdf": false, "noext": false, "exe": false, "photo.exe.jpg": false,
	} {
		if got := isProgramFile(name); got != want {
			t.Errorf("isProgramFile(%q) = %v, want %v", name, got, want)
		}
	}
}
