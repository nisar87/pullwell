package main

import (
	"reflect"
	"testing"
	"time"
)

func TestNoticeAllowed(t *testing.T) {
	for _, c := range []struct {
		level, kind string
		want        bool
	}{
		{notifyAll, "finished", true}, {notifyAll, "attention", true}, {notifyAll, "allDone", true},
		{notifyProblems, "finished", false}, {notifyProblems, "allDone", false},
		{notifyProblems, "attention", true}, {notifyProblems, "checksum", true}, {notifyProblems, "shutdown", true},
		{notifyOff, "attention", false}, {notifyOff, "shutdown", false},
		{"", "finished", true}, // unknown or missing setting behaves like "all"
	} {
		if got := noticeAllowed(c.level, c.kind); got != c.want {
			t.Errorf("noticeAllowed(%q, %q) = %v, want %v", c.level, c.kind, got, c.want)
		}
	}
	if notifyLevelFromOld(true) != notifyProblems || notifyLevelFromOld(false) != notifyAll {
		t.Error("old noNotify setting not translated")
	}
}

func TestGrouperSingleFinished(t *testing.T) {
	g := newNoticeGrouper()
	t0 := time.Unix(1_800_000_000, 0)
	if out := g.Add(Notice{Kind: "finished", Title: "Download finished", Text: "a.iso", ItemID: "d1"}, t0); out != nil {
		t.Fatalf("shown at once: %+v", out)
	}
	if out := g.Due(t0.Add(groupWait - time.Millisecond)); out != nil {
		t.Fatalf("shown before the wait was over: %+v", out)
	}
	want := []toastSpec{{Tag: "done", Title: "Download finished", Lines: []string{"a.iso"}, ItemID: "d1", Buttons: []string{"open", "reveal"}}}
	if out := g.Due(t0.Add(groupWait)); !reflect.DeepEqual(out, want) {
		t.Fatalf("got %+v\nwant %+v", out, want)
	}
	if out := g.Due(t0.Add(time.Hour)); out != nil {
		t.Fatalf("shown twice: %+v", out)
	}
}

func TestGrouperStorm(t *testing.T) {
	g := newNoticeGrouper()
	t0 := time.Unix(1_800_000_000, 0)
	for i, name := range []string{"a", "b", "c", "d", "e"} {
		g.Add(Notice{Kind: "finished", Title: "Download finished", Text: name, ItemID: name}, t0.Add(time.Duration(i)*100*time.Millisecond))
	}
	g.Add(Notice{Kind: "attention", Title: "Download needs attention", Text: "x", ItemID: "x"}, t0.Add(time.Second))
	g.Add(Notice{Kind: "checksum", Title: "Checksum does not match", Text: "y may be damaged", ItemID: "y"}, t0.Add(2*time.Second))

	out := g.Due(t0.Add(groupWait)) // finished group is due, problems not yet (they started 1 s later)
	want := []toastSpec{{Tag: "done", Title: "5 downloads finished", Lines: []string{"a", "b", "and 3 more"}, Buttons: []string{"list"}}}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got %+v\nwant %+v", out, want)
	}
	out = g.Due(t0.Add(time.Second + groupWait))
	want = []toastSpec{{Tag: "problem", Title: "2 downloads need attention", Lines: []string{"x", "y may be damaged"}, Buttons: []string{"list"}}}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got %+v\nwant %+v", out, want)
	}
}

func TestGrouperExactlyThree(t *testing.T) {
	g := newNoticeGrouper()
	t0 := time.Unix(1_800_000_000, 0)
	for _, n := range []string{"a", "b", "c"} {
		g.Add(Notice{Kind: "finished", Text: n}, t0)
	}
	out := g.Due(t0.Add(groupWait))
	if len(out) != 1 || !reflect.DeepEqual(out[0].Lines, []string{"a", "b", "c"}) {
		t.Fatalf("got %+v", out)
	}
}

func TestGrouperNotGrouped(t *testing.T) {
	g := newNoticeGrouper()
	t0 := time.Unix(1_800_000_000, 0)
	out := g.Add(Notice{Kind: "shutdown", Title: "Downloads finished", Text: "The PC shuts down in one minute."}, t0)
	want := []toastSpec{{Tag: "shutdown", Title: "Downloads finished", Lines: []string{"The PC shuts down in one minute."}, Buttons: []string{"cancel-shutdown"}}}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got %+v\nwant %+v", out, want)
	}
	if out := g.Add(Notice{Kind: "allDone", Title: "Downloads finished", Text: "The PC is going to sleep."}, t0); len(out) != 1 || out[0].Buttons != nil {
		t.Fatalf("allDone: %+v", out)
	}
}
