package main

import (
	"encoding/xml"
	"strings"

	"pullwell/dm"
)

var toastTokens = newLinkTokens(nil)

func xmlEsc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// toastXML builds the Windows toast for a notification: the title and lines, a click that leads to the right part
// of the list, and for one finished download the buttons Open (not for programs) and Open folder.
func toastXML(t toastSpec) string {
	link := func(verb, item string) string {
		u := appScheme + "://" + verb
		if item != "" {
			u += "?item=" + item
		}
		if verb != "show" {
			tok, err := toastTokens.issue(item, verb)
			if err != nil {
				return appScheme + "://show"
			}
			if item != "" {
				u += "&t=" + tok
			} else {
				u += "?t=" + tok
			}
		}
		return u
	}
	launch := link("show", t.ItemID)
	if t.ItemID == "" && toastView(t.Tag) == "finished" {
		launch = link("list", "")
	}
	var b strings.Builder
	b.WriteString(`<toast activationType="protocol" launch="` + xmlEsc(launch) + `"><visual><binding template="ToastGeneric">`)
	b.WriteString(`<text>` + xmlEsc(tr(t.Title)) + `</text>`)
	if len(t.Lines) > 0 {
		b.WriteString(`<text>` + xmlEsc(tr(strings.Join(t.Lines, "\n"))) + `</text>`)
	}
	b.WriteString(`</binding></visual>`)
	if t.ItemID != "" && toastView(t.Tag) == "finished" {
		b.WriteString(`<actions>`)
		if name := dm.ItemFile(t.ItemID); name != "" && !isProgramFile(name) {
			b.WriteString(`<action content="` + xmlEsc(tr("Open")) + `" activationType="protocol" arguments="` + xmlEsc(link("open", t.ItemID)) + `"/>`)
		}
		b.WriteString(`<action content="` + xmlEsc(tr("Open folder")) + `" activationType="protocol" arguments="` + xmlEsc(link("reveal", t.ItemID)) + `"/>`)
		b.WriteString(`</actions>`)
	}
	b.WriteString(`</toast>`)
	return b.String()
}

// handleAppLink carries out a pullwell:// link that reached the running program (from a toast, or typed somewhere).
func handleAppLink(raw string) {
	l, err := parseAppLink(raw)
	if err != nil {
		logf("link refused: %v", err)
		showMainWindow()
		return
	}
	d := decideLink(l, toastTokens, dm.ItemFile)
	if d.Why != "" {
		logf("link %s: %s", l.Verb, d.Why)
	}
	switch d.Do {
	case "open", "reveal":
		if err := dm.OpenItem(d.ItemID, d.Do == "reveal"); err != nil {
			logf("link %s: %v", d.Do, err)
			showMainWindow()
		}
	case "add":
		if l := offeredLink(d.ItemID); l != "" {
			setPendingAdd(l)
		}
		showMainWindow()
	case "list":
		pendingMu.Lock()
		pendingViewV = "finished"
		pendingMu.Unlock()
		showMainWindow()
	default: // show (and cancel-shutdown, which the window offers itself)
		showMainWindow()
	}
}
