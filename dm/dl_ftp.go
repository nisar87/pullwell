package dm

// FTP downloads: a small client of our own (passive mode, resume with REST, several connections at once).
// Every connection - control and data - leaves through the chosen line, like the HTTP ones.

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

var errFTPS = dlStreamErr("Encrypted FTP (ftps://) is not supported yet. Use an http or https link, or plain ftp://.")

func dlIsFTP(link string) bool { return strings.HasPrefix(strings.ToLower(link), "ftp://") }

// ftpCreds reads the login from the Authorization header we keep for the download (anonymous when there is none).
func ftpCreds(hdr map[string]string) (user, pass string) {
	for k, v := range hdr {
		if strings.EqualFold(k, "Authorization") && strings.HasPrefix(v, "Basic ") {
			if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, "Basic ")); err == nil {
				u, p, _ := strings.Cut(string(b), ":")
				return u, p
			}
		}
	}
	return "anonymous", "netmerge@"
}

// ftpPath: ftp://host/pub/a.zip means pub/a.zip from the login folder; ftp://host//pub/a.zip is absolute.
func ftpPath(u *url.URL) string {
	p := u.Path
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "//") {
		return p[1:]
	}
	return strings.TrimPrefix(p, "/")
}

type ftpConn struct {
	c     net.Conn
	br    *bufio.Reader
	line  dlLine
	local bool // links into the own network are allowed
	ip    net.IP
	port  string
	stop  func() bool
}

type ftpReply struct {
	code int
	text string
}

func (f *ftpConn) read() (ftpReply, error) {
	_ = f.c.SetReadDeadline(time.Now().Add(45 * time.Second))
	var first string
	var text strings.Builder
	for {
		ln, err := f.br.ReadString('\n')
		if err != nil {
			return ftpReply{}, err
		}
		ln = strings.TrimRight(ln, "\r\n")
		if len(ln) < 4 {
			continue
		}
		code, cerr := strconv.Atoi(ln[:3])
		if cerr != nil {
			if first != "" { // a continuation line of a multi-line reply
				text.WriteString(ln + "\n")
			}
			continue
		}
		if first == "" {
			first = ln[:3]
		}
		text.WriteString(ln[4:] + "\n")
		if ln[3] == ' ' && ln[:3] == first {
			return ftpReply{code: code, text: strings.TrimSpace(text.String())}, nil
		}
	}
}

func (f *ftpConn) cmd(format string, a ...any) (ftpReply, error) {
	_ = f.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintf(f.c, format+"\r\n", a...); err != nil {
		return ftpReply{}, err
	}
	return f.read()
}

func (f *ftpConn) close() {
	if f.stop != nil {
		f.stop()
	}
	if f.c != nil {
		_ = f.c.Close()
	}
}

// ftpOpen connects, reads the greeting and logs in.
func ftpOpen(ctx context.Context, line dlLine, link string, hdr map[string]string, local bool) (*ftpConn, *url.URL, error) {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return nil, nil, errors.New("that is not a valid FTP link")
	}
	port := u.Port()
	if port == "" {
		port = "21"
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, err := line.Dial(dctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, nil, err
	}
	f := &ftpConn{c: c, br: bufio.NewReader(c), line: line, local: local}
	if ta, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		f.ip, f.port = ta.IP, port
		if !local && dlPrivateIP(ta.IP) {
			c.Close()
			return nil, nil, errors.New("refused: the link leads to an address inside your own network")
		}
	}
	f.stop = context.AfterFunc(ctx, func() { _ = c.Close() })
	r, err := f.read()
	if err != nil {
		f.close()
		return nil, nil, err
	}
	if r.code == 421 {
		f.close()
		return nil, nil, dlHTTPErr{Code: 503, Retry: 3 * time.Second}
	}
	if r.code != 220 {
		f.close()
		return nil, nil, fmt.Errorf("the FTP server said: %s", r.text)
	}
	user, pass := ftpCreds(hdr)
	r, err = f.cmd("USER %s", user)
	if err == nil && r.code == 331 {
		r, err = f.cmd("PASS %s", pass)
	}
	if err != nil {
		f.close()
		return nil, nil, err
	}
	switch {
	case r.code == 230 || r.code == 202:
	case r.code == 421 || (r.code == 530 && strings.Contains(strings.ToLower(r.text), "too many")):
		f.close()
		return nil, nil, dlHTTPErr{Code: 503, Retry: 3 * time.Second}
	case r.code == 530 || r.code == 331 || r.code == 332:
		f.close()
		return nil, nil, dlStreamErr("The FTP server refused the login. It may need a user name and password: use Add with options.")
	default:
		f.close()
		return nil, nil, fmt.Errorf("the FTP server said: %s", r.text)
	}
	if r, err = f.cmd("TYPE I"); err != nil || r.code != 200 {
		f.close()
		if err == nil {
			err = fmt.Errorf("the FTP server said: %s", r.text)
		}
		return nil, nil, err
	}
	return f, u, nil
}

// pasv opens the data connection (EPSV first, then PASV), out of the same line.
func (f *ftpConn) pasv(ctx context.Context) (net.Conn, error) {
	var host, port string
	r, err := f.cmd("EPSV")
	if err == nil && r.code == 229 {
		if i, j := strings.Index(r.text, "(|||"), strings.LastIndex(r.text, "|)"); i >= 0 && j > i+4 {
			port = r.text[i+4 : j]
			host = f.ip.String()
		}
	}
	if port == "" {
		r, err = f.cmd("PASV")
		if err != nil {
			return nil, err
		}
		if r.code != 227 {
			return nil, fmt.Errorf("the FTP server cannot send the file (%s)", r.text)
		}
		i, j := strings.IndexByte(r.text, '('), strings.IndexByte(r.text, ')')
		if i < 0 || j < i {
			// some servers answer without brackets
			i = strings.IndexAny(r.text, "0123456789") - 1
			j = len(r.text)
		}
		parts := strings.Split(r.text[i+1:j], ",")
		if len(parts) != 6 {
			return nil, errors.New("the FTP server sent an unusable address for the file")
		}
		p1, e1 := strconv.Atoi(strings.TrimSpace(parts[4]))
		p2, e2 := strconv.Atoi(strings.TrimSpace(parts[5]))
		if e1 != nil || e2 != nil {
			return nil, errors.New("the FTP server sent an unusable address for the file")
		}
		host = strings.Join(parts[:4], ".")
		port = strconv.Itoa(p1*256 + p2)
		// a server behind a NAT may name its private address: the control connection's address is the right one
		if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() || (dlPrivateIP(ip) && !dlPrivateIP(f.ip)) || !ip.Equal(f.ip) {
			host = f.ip.String()
		}
	}
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d, err := f.line.Dial(dctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	if ta, ok := d.RemoteAddr().(*net.TCPAddr); ok && !f.local && dlPrivateIP(ta.IP) {
		d.Close()
		return nil, errors.New("refused: the link leads to an address inside your own network")
	}
	return d, nil
}

// dlProbeFTP asks the server for the size, the date and whether it can resume.
func dlProbeFTP(ctx context.Context, line dlLine, link string, hdr map[string]string, local bool) (dlInfo, error) {
	info := dlInfo{URL: link, FinalURL: link, Size: -1, Type: "ftp"}
	f, u, err := ftpOpen(ctx, line, link, hdr, local)
	if err != nil {
		return info, err
	}
	defer f.close()
	p := ftpPath(u)
	if p == "" || strings.HasSuffix(u.Path, "/") {
		return info, dlStreamErr("That FTP link is a folder, not a file.")
	}
	info.Name = path.Base(p)
	if n, err := url.PathUnescape(info.Name); err == nil {
		info.Name = dlSafeName(n)
	}
	r, err := f.cmd("SIZE %s", p)
	if err != nil {
		return info, err
	}
	switch {
	case r.code == 213:
		if n, e := strconv.ParseInt(strings.TrimSpace(r.text), 10, 64); e == nil && n >= 0 {
			info.Size = n
		}
	case r.code == 550 || r.code == 450:
		return info, dlHTTPErr{Code: 404}
	}
	if r, err = f.cmd("MDTM %s", p); err == nil && r.code == 213 {
		info.LastMod = strings.TrimSpace(r.text)
	}
	if info.Size >= 0 {
		if r, err = f.cmd("REST 0"); err == nil && r.code == 350 {
			info.Ranges = true
		}
	}
	_, _ = f.cmd("QUIT")
	return info, nil
}

// ftpBody is the data connection of one transfer; closing it ends the transfer.
type ftpBody struct {
	io.Reader
	data net.Conn
	ctl  *ftpConn
}

func (b *ftpBody) Close() error {
	_ = b.data.Close()
	b.ctl.close()
	return nil
}

// ftpOpenRange starts RETR at offset from and reads at most want bytes (want < 0: to the end).
func (t *dlTask) ftpOpenRange(ctx context.Context, r *dlRun, from, want int64) (io.ReadCloser, error) {
	f, u, err := ftpOpen(ctx, r.line, t.info.FinalURL, t.hdr, t.private)
	if err != nil {
		return nil, err
	}
	p := ftpPath(u)
	if from > 0 {
		rep, err := f.cmd("REST %d", from)
		if err != nil {
			f.close()
			return nil, err
		}
		if rep.code != 350 {
			f.close()
			return nil, errors.New("the FTP server cannot continue a download from the middle")
		}
	}
	data, err := f.pasv(ctx)
	if err != nil {
		f.close()
		return nil, err
	}
	rep, err := f.cmd("RETR %s", p)
	if err != nil {
		data.Close()
		f.close()
		return nil, err
	}
	switch {
	case rep.code == 125 || rep.code == 150:
	case rep.code == 550 || rep.code == 450:
		data.Close()
		f.close()
		return nil, dlHTTPErr{Code: 404}
	case rep.code == 421 || rep.code == 425 || rep.code == 426:
		data.Close()
		f.close()
		return nil, dlHTTPErr{Code: 503, Retry: 2 * time.Second}
	default:
		data.Close()
		f.close()
		return nil, fmt.Errorf("the FTP server said: %s", rep.text)
	}
	var rd io.Reader = data
	if want >= 0 {
		rd = io.LimitReader(data, want)
	}
	return &ftpBody{Reader: rd, data: data, ctl: f}, nil
}
