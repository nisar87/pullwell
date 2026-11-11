package main

import (
	"strings"
	"testing"
)

func TestTrayAndNoticeTextsAreTranslated(t *testing.T) {
	d := loadTransDict("es")
	if d == nil {
		t.Fatal("ui/i18n/es.js did not load")
	}
	for in, want := range map[string]string{
		"Pause all":                           "Pausar todo",
		"Open " + AppName:                     "Abrir " + AppName,
		"Quit " + AppName:                     "Salir de " + AppName,
		"3 downloads finished":                "3 descargas terminadas",
		"Download finished":                   "Descarga terminada",
		"Download this with " + AppName + "?": "¿Descargar esto con " + AppName + "?",
		"Open the download folder":            "Abrir la carpeta de descargas",
		"something never written":             "something never written",
	} {
		if got := d.tr(in); got != want {
			t.Errorf("tr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEveryShippedLanguageLoads(t *testing.T) {
	for _, code := range []string{"es", "fr", "de", "pt", "ar", "ur"} {
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
