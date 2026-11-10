package main

import "testing"

// The titles come from dm/dl_manager.go; if they change there, the grouping must follow.
func TestNoticeKind(t *testing.T) {
	for _, c := range []struct{ title, text, want string }{
		{"Download finished", "a.zip", "finished"},
		{"Download needs attention", "a.zip", "attention"},
		{"Checksum does not match", "a.zip may be damaged", "checksum"},
		{"Downloads finished", "The PC shuts down in one minute. Open Pullwell to cancel.", "shutdown"},
		{"Downloads finished", "The PC is going to sleep.", "allDone"},
		{"Something new", "", "attention"},
	} {
		if got := noticeKind(c.title, c.text); got != c.want {
			t.Errorf("noticeKind(%q, %q) = %q, want %q", c.title, c.text, got, c.want)
		}
	}
}
