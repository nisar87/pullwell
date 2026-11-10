package main

// Proxy: downloads (and update checks) can go through a proxy. Three choices, in Options → Connection:
//   - "system" (the default): what Windows is set to (Settings → Network → Proxy, "Use a proxy server"). An
//     automatic setup script (PAC) is not read; then the person types the proxy instead.
//   - "none": straight out.
//   - "manual": one proxy the person typed, "host:port" (HTTP) or "socks5://host:port".

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type proxySetting struct {
	Mode string `json:"mode"` // "system" | "none" | "manual"
	Addr string `json:"addr,omitempty"`
}

// systemProxy is what the operating system is set to: the ProxyServer text and the bypass list, already read.
type systemProxy struct {
	Enabled bool
	Server  string // "host:port" or "http=h:p;https=h:p;socks=h:p"
	Bypass  string // "*.local;<local>;10.*"
	Script  string // an automatic setup script, which is not used
}

var (
	sysProxyMu   sync.Mutex
	sysProxyAt   time.Time
	sysProxyLast systemProxy
)

// currentSystemProxy reads the system's proxy at most every 30 seconds (it can change while the program runs).
func currentSystemProxy() systemProxy {
	sysProxyMu.Lock()
	defer sysProxyMu.Unlock()
	if time.Since(sysProxyAt) > 30*time.Second {
		sysProxyLast, sysProxyAt = readSystemProxy(), time.Now()
	}
	return sysProxyLast
}

func proxyConfig() proxySetting {
	appSetMu.Lock()
	defer appSetMu.Unlock()
	p := appSet.Proxy
	if p.Mode != "none" && p.Mode != "manual" {
		p.Mode = "system"
	}
	return p
}

// parseProxyAddr turns what the person typed into a proxy URL: "host:port" means an HTTP proxy.
func parseProxyAddr(s string) (*url.URL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("type the proxy as host:port, for example 10.0.0.1:8080")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("that is not a proxy address; type host:port, for example 10.0.0.1:8080")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	case "socks":
		u.Scheme = "socks5"
	default:
		return nil, fmt.Errorf("proxies of the kind %q are not supported (use http or socks5)", u.Scheme)
	}
	if u.Port() == "" {
		return nil, errors.New("the proxy needs a port, for example 10.0.0.1:8080")
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return u, nil
}

// systemProxyFor picks the proxy Windows would use for this address: a per-scheme entry ("https=...") or the one
// for all, with "socks=" as the fallback; nil when the host is on the bypass list.
func systemProxyFor(sp systemProxy, target *url.URL) *url.URL {
	if !sp.Enabled || strings.TrimSpace(sp.Server) == "" || bypassed(sp.Bypass, target.Hostname()) {
		return nil
	}
	all, per := "", map[string]string{}
	for _, part := range strings.FieldsFunc(sp.Server, func(r rune) bool { return r == ';' || r == ' ' }) {
		if k, v, ok := strings.Cut(part, "="); ok {
			per[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		} else {
			all = strings.TrimSpace(part)
		}
	}
	pick := all
	if v := per[target.Scheme]; v != "" {
		pick = v
	} else if pick == "" && per["http"] != "" && target.Scheme == "https" {
		pick = per["http"] // only an http= entry: Windows uses it for https too
	} else if pick == "" && per["socks"] != "" {
		pick = "socks5://" + strings.TrimPrefix(per["socks"], "socks5://")
	}
	if pick == "" {
		return nil
	}
	u, err := parseProxyAddr(pick)
	if err != nil {
		return nil
	}
	return u
}

// bypassed reports whether the host is on Windows' "don't use the proxy for" list. "<local>" means names without
// a dot; entries may use * as a wildcard.
func bypassed(list, host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	for _, e := range strings.FieldsFunc(strings.ToLower(list), func(r rune) bool { return r == ';' || r == ' ' || r == ',' }) {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if e == "<local>" {
			if !strings.Contains(host, ".") {
				return true
			}
			continue
		}
		if wildMatch(e, host) {
			return true
		}
	}
	return false
}

func wildMatch(pat, s string) bool {
	if !strings.Contains(pat, "*") {
		return pat == s
	}
	parts := strings.Split(pat, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i, p := range parts[1:] {
		if i == len(parts)-2 {
			return strings.HasSuffix(s, p)
		}
		j := strings.Index(s, p)
		if j < 0 {
			return false
		}
		s = s[j+len(p):]
	}
	return true
}

// proxyFor is the engine's (and the update check's) proxy choice for one request.
func proxyFor(r *http.Request) (*url.URL, error) {
	switch c := proxyConfig(); c.Mode {
	case "none":
		return nil, nil
	case "manual":
		u, err := parseProxyAddr(c.Addr)
		if err != nil {
			return nil, err
		}
		if bypassed("", r.URL.Hostname()) {
			return nil, nil
		}
		return u, nil
	}
	return systemProxyFor(currentSystemProxy(), r.URL), nil
}

// proxyText describes the proxy in use, for Options.
func proxyText() string {
	sp := currentSystemProxy()
	switch {
	case sp.Enabled && sp.Server != "":
		return "Windows uses the proxy " + sp.Server + "."
	case sp.Script != "":
		return "Windows uses an automatic proxy setup script, which " + AppName + " cannot read. If downloads fail, type the proxy below."
	}
	return "Windows uses no proxy."
}

// testProxy fetches a small page through the chosen proxy, so the person knows it works.
func testProxy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tr := &http.Transport{Proxy: proxyFor, DialContext: (&net.Dialer{Timeout: 8 * time.Second}).DialContext}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", "https://github.com/", nil)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return fmt.Errorf("could not reach github.com: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusProxyAuthRequired {
		return errors.New("the proxy asks for a user name and password; type it as http://user:password@host:port")
	}
	return nil
}

// redactProxy hides a password in a proxy address before it is written to the log.
func redactProxy(s string) string {
	if u, err := url.Parse(s); err == nil && u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.UserPassword(u.User.Username(), "***")
			return u.String()
		}
	}
	return s
}
