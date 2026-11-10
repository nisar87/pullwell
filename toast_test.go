package main

import (
	"encoding/xml"
	"regexp"
	"strings"
	"testing"
)

func TestToastXML(t *testing.T) {
	x := toastXML(toastSpec{Tag: "finished", Title: "Download finished", Lines: []string{`a <b> & "c".zip`}, ItemID: "d1-1"})
	if err := xml.Unmarshal([]byte(x), new(struct{})); err != nil {
		t.Fatalf("not valid XML: %v\n%s", err, x)
	}
	if !strings.Contains(x, `launch="`+appScheme+`://show?item=d1-1"`) {
		t.Errorf("a click must show the download: %s", x)
	}
	m := regexp.MustCompile(`arguments="([^"]+)"`).FindAllStringSubmatch(x, -1)
	if len(m) != 1 || !strings.Contains(m[0][1], "reveal") {
		t.Fatalf("for a download whose file is unknown only Open folder is offered: %s", x)
	}
	l, err := parseAppLink(strings.ReplaceAll(m[0][1], "&amp;", "&"))
	if err != nil {
		t.Fatal(err)
	}
	if !toastTokens.allows(l) {
		t.Fatal("the button's own token is not accepted")
	}
	l.Verb = "open"
	if toastTokens.allows(l) {
		t.Fatal("a token for Open folder must not open the file")
	}
	g := toastXML(toastSpec{Tag: "done", Title: "3 downloads finished", Lines: []string{"a", "b"}})
	if !strings.Contains(g, appScheme+"://list?t=") || strings.Contains(g, "<actions>") {
		t.Errorf("a grouped toast leads to the list and has no buttons: %s", g)
	}
	p := toastXML(toastSpec{Tag: "problem", Title: "Download needs attention"})
	if !strings.Contains(p, `launch="`+appScheme+`://show"`) {
		t.Errorf("a problem toast shows the window: %s", p)
	}
}

func TestCopiedLinkOfferLink(t *testing.T) {
	key := offerKey("https://x.org/a.zip")
	tok, _ := toastTokens.issue(key, "add")
	l, err := parseAppLink(appScheme + "://add/?item=" + key + "&t=" + tok)
	if err != nil {
		t.Fatal(err)
	}
	d := decideLink(l, toastTokens, func(string) string { return "" })
	if d.Do != "add" || offeredLink(d.ItemID) != "https://x.org/a.zip" {
		t.Fatalf("%+v", d)
	}
	l.Token = strings.Repeat("0", 32)
	if d := decideLink(l, toastTokens, func(string) string { return "" }); d.Do != "show" {
		t.Fatal("an add link without the right code must only show the window")
	}
}
