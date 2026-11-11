package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The tray menu and the Windows notifications are drawn by the program, not by the window. They use the same
// dictionaries as the window (ui/i18n/<code>.js), in the language the window last showed.

var (
	langMu   sync.Mutex
	langCode = "en"
	langDict *transDict
)

type transPat struct {
	re  *regexp.Regexp
	to  string
	len int
}

type transDict struct {
	exact map[string]string
	pats  []transPat
}

var wsRe = regexp.MustCompile(`\s+`)
var holeRe = regexp.MustCompile(`\{\d+\}`)

// loadTransDict reads ui/i18n/<code>.js, a single I18N.add("<code>", {...}) call, into a dictionary.
func loadTransDict(code string) *transDict {
	if !regexp.MustCompile(`^[a-z]{2}$`).MatchString(code) {
		return nil
	}
	b, err := uiFiles.ReadFile("ui/i18n/" + code + ".js")
	if err != nil {
		return nil
	}
	s := string(b)
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(s[i:j+1]), &m) != nil {
		return nil
	}
	d := &transDict{exact: map[string]string{}}
	for k, v := range m {
		k = strings.TrimSpace(wsRe.ReplaceAllString(k, " "))
		if !holeRe.MatchString(k) {
			d.exact[k] = v
			continue
		}
		var src strings.Builder
		for _, part := range holeRe.Split(k, -1) {
			src.WriteString(regexp.QuoteMeta(part))
			src.WriteString("(.*?)")
		}
		pat := strings.TrimSuffix(src.String(), "(.*?)")
		if re, err := regexp.Compile("^" + pat + "$"); err == nil {
			d.pats = append(d.pats, transPat{re: re, to: v, len: len(k)})
		}
	}
	sort.Slice(d.pats, func(a, b int) bool { return d.pats[a].len > d.pats[b].len })
	return d
}

func (d *transDict) tr(s string) string {
	k := strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
	if v, ok := d.exact[k]; ok {
		return v
	}
	for _, p := range d.pats {
		if m := p.re.FindStringSubmatch(k); m != nil {
			return holeRe.ReplaceAllStringFunc(p.to, func(h string) string {
				n := 0
				for _, c := range h[1 : len(h)-1] {
					n = n*10 + int(c-'0')
				}
				if n+1 < len(m) {
					return m[n+1]
				}
				return ""
			})
		}
	}
	return s
}

// tr gives the text in the language of the window; text without a translation stays as it is.
func tr(s string) string {
	langMu.Lock()
	d := langDict
	langMu.Unlock()
	if d == nil {
		return s
	}
	return d.tr(s)
}

// setUILang is called when the window reports its language; it is kept for the next start.
func setUILang(code string) {
	if code == "xx" || !regexp.MustCompile(`^[a-z]{2}$`).MatchString(code) {
		return
	}
	langMu.Lock()
	changed := langCode != code
	langCode, langDict = code, loadTransDict(code)
	langMu.Unlock()
	if changed {
		_ = os.WriteFile(filepath.Join(appRoot, "data", "lang"), []byte(code), 0o644)
	}
}

func loadUILang() {
	b, err := os.ReadFile(filepath.Join(appRoot, "data", "lang"))
	if err != nil {
		return
	}
	code := strings.TrimSpace(string(b))
	langMu.Lock()
	langCode, langDict = code, loadTransDict(code)
	langMu.Unlock()
}
