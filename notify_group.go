package main

// Notification grouping (design section 6.2): notices are collected for a few seconds so that ten downloads
// finishing together give one toast, not ten. Problems are grouped separately and never hidden by finished ones.

import (
	"fmt"
	"sync"
	"time"
)

// Notice is what the download engine reports.
type Notice struct {
	Kind   string // "finished", "attention", "checksum", "allDone", "shutdown"
	Title  string
	Text   string
	ItemID string
}

// Notification levels (design D8).
const (
	notifyAll      = "all"
	notifyProblems = "problems"
	notifyOff      = "off"
)

// noticeAllowed says whether a notice of this kind is shown at this level. The old setting noNotify=true means
// "problems" (see notifyLevelFromOld).
func noticeAllowed(level, kind string) bool {
	switch level {
	case notifyOff:
		return false
	case notifyProblems:
		return kind != "finished" && kind != "allDone"
	}
	return true
}

func notifyLevelFromOld(noNotify bool) string {
	if noNotify {
		return notifyProblems
	}
	return notifyAll
}

// toastSpec is one notification to show: as a toast in the installed version, as a tray balloon otherwise.
type toastSpec struct {
	Tag     string // a new toast with the same tag replaces the old one
	Title   string
	Lines   []string // at most 3 lines of body text
	ItemID  string   // set when the toast is about one download
	Buttons []string // link verbs: "open", "reveal", "show", "list", "cancel-shutdown"
}

const (
	groupWait    = 3 * time.Second
	groupMaxName = 3 // names listed in a grouped toast
)

// noticeGrouper collects notices. Add is called by the engine; Due is called by a timer on the UI thread.
type noticeGrouper struct {
	mu      sync.Mutex
	pending map[string][]Notice // "done" or "problem"
	since   map[string]time.Time
}

func newNoticeGrouper() *noticeGrouper {
	return &noticeGrouper{pending: map[string][]Notice{}, since: map[string]time.Time{}}
}

func groupOf(kind string) string {
	switch kind {
	case "finished":
		return "done"
	case "attention", "checksum":
		return "problem"
	}
	return "" // allDone and shutdown are shown at once, never grouped
}

// Add takes a notice. Notices that are never grouped come back at once as a toast; the others wait for Due.
func (g *noticeGrouper) Add(n Notice, now time.Time) []toastSpec {
	grp := groupOf(n.Kind)
	if grp == "" {
		return []toastSpec{singleToast(n)}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.pending[grp]) == 0 {
		g.since[grp] = now
	}
	g.pending[grp] = append(g.pending[grp], n)
	return nil
}

// Due returns the toasts whose wait is over. Problems come first.
func (g *noticeGrouper) Due(now time.Time) []toastSpec {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []toastSpec
	for _, grp := range []string{"problem", "done"} {
		ns := g.pending[grp]
		if len(ns) == 0 || now.Sub(g.since[grp]) < groupWait {
			continue
		}
		out = append(out, groupToast(grp, ns))
		delete(g.pending, grp)
	}
	return out
}

func singleToast(n Notice) toastSpec {
	t := toastSpec{Tag: n.Kind, Title: n.Title, ItemID: n.ItemID}
	if n.Text != "" {
		t.Lines = []string{n.Text}
	}
	switch n.Kind {
	case "finished":
		t.Tag, t.Buttons = "done", []string{"open", "reveal"}
	case "attention", "checksum":
		t.Tag, t.Buttons = "problem", []string{"show"}
	case "shutdown":
		t.Buttons = []string{"cancel-shutdown"}
	}
	return t
}

func groupToast(grp string, ns []Notice) toastSpec {
	if len(ns) == 1 {
		return singleToast(ns[0])
	}
	t := toastSpec{Tag: grp, Buttons: []string{"list"}}
	if grp == "done" {
		t.Title = fmt.Sprintf("%d downloads finished", len(ns))
	} else {
		t.Title = fmt.Sprintf("%d downloads need attention", len(ns))
	}
	show := len(ns)
	if show > groupMaxName {
		show = groupMaxName - 1 // keep the last line for "and N more"
	}
	for _, n := range ns[:show] {
		t.Lines = append(t.Lines, n.Text)
	}
	if show < len(ns) {
		t.Lines = append(t.Lines, fmt.Sprintf("and %d more", len(ns)-show))
	}
	return t
}
