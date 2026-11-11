package main

import (
	"strings"
	"testing"
)

func TestTrayAndNoticeTextsAreTranslated(t *testing.T) {
	d := loadTransDict("ur")
	if d == nil {
		t.Fatal("ui/i18n/ur.js did not load")
	}
	for _, in := range []string{"Pause all", "Open " + AppName, "Quit " + AppName, "3 downloads finished", "Download finished", "Open the download folder"} {
		if got := d.tr(in); got == in || got == "" {
			t.Errorf("tr(%q) is not translated: %q", in, got)
		}
	}
	if got := d.tr("something never written"); got != "something never written" {
		t.Errorf("an unknown text must stay as it is, got %q", got)
	}
}

func TestEveryShippedLanguageLoads(t *testing.T) {
	for _, code := range []string{"ur"} {
		d := loadTransDict(code)
		if d == nil || len(d.exact)+len(d.pats) < 600 {
			t.Errorf("%s: dictionary missing or short", code)
			continue
		}
		if d.tr("Pause all") == "Pause all" {
			t.Errorf("%s: Pause all is not translated", code)
		}
	}
	if loadTransDict("xx") != nil || loadTransDict("../x") != nil {
		t.Error("a language that does not exist must not load")
	}
}

func TestWithoutALanguageTextStaysEnglish(t *testing.T) {
	langMu.Lock()
	langDict = nil
	langMu.Unlock()
	if tr("Pause all") != "Pause all" || strings.Contains(tr("Open "+AppName), "Abrir") {
		t.Error("English must stay as it is")
	}
}
