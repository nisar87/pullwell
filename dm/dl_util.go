package dm

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ---- rate limiting -------------------------------------------------------------------------------------------------

// dlBucket is a token bucket used to pace reads. rate is bytes per second, 0 = unlimited.
type dlBucket struct {
	mu     sync.Mutex
	rate   float64
	tokens float64
	last   time.Time
}

func (b *dlBucket) setRate(r float64) {
	b.mu.Lock()
	b.rate = r
	b.tokens = 0
	b.last = time.Now()
	b.mu.Unlock()
}

func (b *dlBucket) getRate() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rate
}

// wait reserves n bytes and sleeps until they may be used (or ctx ends).
func (b *dlBucket) wait(ctx context.Context, n int) error {
	b.mu.Lock()
	if b.rate <= 0 {
		b.mu.Unlock()
		return ctx.Err()
	}
	now := time.Now()
	if b.last.IsZero() {
		b.last = now
	}
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	b.last = now
	if max := b.rate * 0.25; b.tokens > max { // at most a quarter second of burst
		b.tokens = max
	}
	b.tokens -= float64(n)
	var d time.Duration
	if b.tokens < 0 {
		d = time.Duration(-b.tokens / b.rate * float64(time.Second))
	}
	b.mu.Unlock()
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ---- file names ----------------------------------------------------------------------------------------------------

var dlReserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

func init() {
	for i := 1; i <= 9; i++ {
		dlReserved[fmt.Sprintf("COM%d", i)] = true
		dlReserved[fmt.Sprintf("LPT%d", i)] = true
	}
}

// dlSafeName turns whatever a server or a link says into a file name that is valid on Windows and cannot leave the folder.
func dlSafeName(n string) string {
	n = strings.ReplaceAll(n, "\\", "/")
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	var sb strings.Builder
	for _, r := range n {
		switch {
		case r < 32 || r == 127, strings.ContainsRune(`<>:"|?*`, r), r == utf8.RuneError:
			sb.WriteRune('_')
		default:
			sb.WriteRune(r)
		}
	}
	n = strings.Trim(sb.String(), " .")
	if n == "" {
		n = "download"
	}
	ext := path.Ext(n)
	base := strings.TrimSuffix(n, ext)
	if len(ext) > 16 {
		ext = ext[:16]
	}
	for utf8.RuneCountInString(base) > 150 {
		_, sz := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-sz]
	}
	if base == "" {
		base = "download"
	}
	if dlReserved[strings.ToUpper(base)] {
		base = "_" + base
	}
	return base + ext
}

// dlPickName: Content-Disposition first, then the last part of the link.
func dlPickName(disposition, link string) string {
	if disposition != "" {
		if _, p, err := mime.ParseMediaType(disposition); err == nil {
			if v := p["filename"]; v != "" {
				return dlSafeName(v)
			}
		}
	}
	if u, err := url.Parse(link); err == nil {
		if b := path.Base(u.Path); b != "" && b != "/" && b != "." {
			if d, err := url.PathUnescape(b); err == nil {
				b = d
			}
			return dlSafeName(b)
		}
	}
	return "download"
}

// dlUniquePath returns dir/name, or dir/name (2).ext... when something with that name (or its partial file) exists.
func dlUniquePath(dir, name string, taken func(string) bool) string {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for i := 2; taken(p); i++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
	return p
}

// ---- addresses -----------------------------------------------------------------------------------------------------

func dlPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() ||
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64) // 100.64/10
}

// dlLocalHost: a link that points at this PC or the home network (typed by the person, so allowed).
func dlLocalHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") || !strings.Contains(h, ".") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return dlPrivateIP(ip)
	}
	return false
}

func dlHostOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

var dlRedactURL = regexp.MustCompile(`(https?://[^\s"?]+)\?[^\s"]*`)

func dlFmtErr(err error) string {
	if err == nil {
		return ""
	}
	s := dlRedactURL.ReplaceAllString(err.Error(), "$1?…")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func filepathDir(p string) string  { return filepath.Dir(p) }
func filepathBase(p string) string { return filepath.Base(p) }
