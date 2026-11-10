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

func TestNotificationClickLeadsToTheList(t *testing.T) {
	for tag, want := range map[string]string{"finished": "finished", "done": "finished", "allDone": "finished", "problem": "errors", "checksum": "errors", "update": ""} {
		if toastView(tag) != want {
			t.Errorf("toastView(%q) = %q, want %q", tag, toastView(tag), want)
		}
	}
	setBalloonView("errors")
	balloonClicked()
	if takePendingView() != "errors" || takePendingView() != "" {
		t.Fatal("the click must hand the view to the window once")
	}
	setBalloonLink("https://x.org/a.zip")
	setBalloonView("finished")
	balloonClicked()
	if takePendingAdd() != "https://x.org/a.zip" || takePendingView() != "" {
		t.Fatal("a copied-link notification opens the Add dialog, nothing else")
	}
}
