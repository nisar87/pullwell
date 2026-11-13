package dm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrClass(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{dlHTTPErr{Code: 403}, "http-403"},
		{context.DeadlineExceeded, "timeout"},
		{errors.New(`Get "https://secret.example/x": dial tcp: lookup secret.example: no such host`), "dns"},
		{errors.New("write /tmp/x: no space left on device"), "disk-full"},
		{fmt.Errorf("wrapped: %w", errDlChanged), "file-changed"},
	} {
		if got := dlErrClass(c.err); got != c.want {
			t.Errorf("dlErrClass(%v) = %q, want %q", c.err, got, c.want)
		}
		if strings.Contains(dlErrClass(c.err), "secret") {
			t.Errorf("a class must never carry the site")
		}
	}
}

func TestPanicLineHasNoUserData(t *testing.T) {
	var got string
	H.Log = func(f string, a ...any) { got = fmt.Sprintf(f, a...) }
	defer func() { H.Log = nil }()
	func() {
		defer func() { hPanic("testing", recover()) }()
		var m map[string]int
		m["x"] = 1
	}()
	if !strings.Contains(got, "recovered from a crash in testing") || !strings.Contains(got, "assignment to entry in nil map") {
		t.Fatalf("unexpected line: %q", got)
	}
	t.Log(got)
}
