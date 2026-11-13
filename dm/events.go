package dm

// Events for the program's log. They say WHAT happened to a download and why, never WHICH download: no link, site,
// file name or folder appears here (the log is read by people who help fix problems, and may one day be sent).

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"runtime/debug"
	"strings"
	"syscall"
)

// dlErrClass names the cause of a failed download in a few fixed words, so equal causes can be counted.
func dlErrClass(err error) string {
	if err == nil {
		return "none"
	}
	var he dlHTTPErr
	var se dlStreamErr
	switch {
	case errors.As(err, &he):
		return fmt.Sprintf("http-%d", he.Code)
	case errors.Is(err, errDlChanged):
		return "file-changed"
	case errors.As(err, &se):
		return "stream"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "reset"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, os.ErrPermission):
		return "no-permission"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	var de *net.DNSError
	if errors.As(err, &de) {
		return "dns"
	}
	var ce *tls.CertificateVerificationError
	if errors.As(err, &ce) {
		return "certificate"
	}
	low := strings.ToLower(err.Error())
	for _, c := range []struct{ has, class string }{
		{"no such host", "dns"}, {"server misbehaving", "dns"}, {"connection refused", "refused"},
		{"x509", "certificate"}, {"certificate", "certificate"}, {"timeout", "timeout"}, {"deadline exceeded", "timeout"},
		{"stopped sending", "timeout"}, {"connection reset", "reset"}, {"unexpected eof", "reset"}, {"broken pipe", "reset"},
		{"not enough space", "disk-full"}, {"no space left", "disk-full"}, {"disk is full", "disk-full"},
		{"cannot write", "cannot-write"}, {"access is denied", "cannot-write"}, {"permission denied", "cannot-write"},
		{"cannot find the path", "folder-gone"}, {"device is not ready", "folder-gone"},
		{"too long", "path-too-long"}, {"no connection is available", "no-connection"},
	} {
		if strings.Contains(low, c.has) {
			return c.class
		}
	}
	return fmt.Sprintf("other(%T)", errors.Unwrap(err))
}

// evt writes one line about a download, by its number only.
func evt(e *dlEntry, format string, a ...any) {
	kind := "file"
	if e.Kind == "torrent" {
		kind = "torrent"
	}
	hLog("download %s (%s): "+format, append([]any{e.ID, kind}, a...)...)
}

// hCount counts a frequent event; the program writes the counts once a minute.
func hCount(key string) {
	if H.Count != nil {
		H.Count(key)
	}
}

// hPanic logs a panic that was caught: what was being done, the value, and the first places in our own code.
func hPanic(what string, r any) {
	hCount("panic")
	var at []string
	lines := strings.Split(string(debug.Stack()), "\n")
	for i := 0; i+1 < len(lines) && len(at) < 4; i++ {
		l := strings.TrimSpace(lines[i+1])
		if strings.HasPrefix(l, "/") || strings.Contains(l, ":\\") || strings.Contains(l, "pullwell") {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "pullwell") && !strings.Contains(lines[i], "hPanic") && !strings.Contains(lines[i], ".safe") {
				at = append(at, strings.Fields(l[strings.LastIndexAny(l, `/\`)+1:])[0])
			}
		}
	}
	msg := fmt.Sprint(r)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	hLog("recovered from a crash in %s: %s [%s]", what, msg, strings.Join(at, " < "))
}

// cfgDiff says which settings changed, with the value for plain switches and numbers (never a folder, site or name).
func cfgDiff(old, cur dlConfig) string {
	ov, nv := reflect.ValueOf(old), reflect.ValueOf(cur)
	tt := ov.Type()
	var out []string
	for i := 0; i < tt.NumField(); i++ {
		if reflect.DeepEqual(ov.Field(i).Interface(), nv.Field(i).Interface()) {
			continue
		}
		name := tt.Field(i).Tag.Get("json")
		f := nv.Field(i)
		switch f.Kind() {
		case reflect.Bool, reflect.Int, reflect.Int64, reflect.Float64:
			out = append(out, fmt.Sprintf("%s=%v", name, f.Interface()))
		case reflect.String:
			if name == "callMode" || name == "onExists" || name == "afterAll" {
				out = append(out, fmt.Sprintf("%s=%v", name, f.Interface()))
			} else {
				out = append(out, name+" changed")
			}
		case reflect.Slice, reflect.Map:
			out = append(out, fmt.Sprintf("%s changed (%d)", name, f.Len()))
		default:
			out = append(out, name+" changed")
		}
	}
	return strings.Join(out, ", ")
}
