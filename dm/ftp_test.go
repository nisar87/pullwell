package dm

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ftpSrv is a small FTP server for tests: passive mode, SIZE, MDTM, REST, optional login, optional limits.
type ftpSrv struct {
	ln        net.Listener
	size      int64
	user      string // "" = anonymous
	pass      string
	noRest    bool
	noSize    bool
	maxLogins int32
	perConn   float64
	live      int32
	logins    int32
	rests     int32
	mu        sync.Mutex
	seen      map[string]int64 // bytes sent per client address
	wg        sync.WaitGroup
}

func newFTP(size int64, opts ...func(*ftpSrv)) *ftpSrv {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	s := &ftpSrv{ln: ln, size: size, seen: map[string]int64{}}
	for _, o := range opts {
		o(s)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() { defer s.wg.Done(); s.serve(c) }()
		}
	}()
	return s
}

func (s *ftpSrv) Close() { s.ln.Close() }
func (s *ftpSrv) URL(p string) string {
	return "ftp://" + s.ln.Addr().String() + p
}
func (s *ftpSrv) bytesFrom(ip string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[ip]
}

func (s *ftpSrv) serve(c net.Conn) {
	defer c.Close()
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().(*net.TCPAddr).String())
	rd := bufio.NewReader(c)
	say := func(f string, a ...any) { fmt.Fprintf(c, f+"\r\n", a...) }
	if s.maxLogins > 0 && atomic.AddInt32(&s.live, 1) > s.maxLogins {
		atomic.AddInt32(&s.live, -1)
		say("421 Too many connections")
		return
	} else if s.maxLogins > 0 {
		defer atomic.AddInt32(&s.live, -1)
	}
	say("220 test server")
	var rest int64
	var dl net.Listener
	user, authed := "", false
	for {
		ln, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		ln = strings.TrimRight(ln, "\r\n")
		cmd, arg, _ := strings.Cut(ln, " ")
		switch strings.ToUpper(cmd) {
		case "USER":
			user = arg
			say("331 need password")
		case "PASS":
			if (s.user == "" && (user == "anonymous" || user == "ftp")) || (s.user != "" && user == s.user && arg == s.pass) {
				authed = true
				atomic.AddInt32(&s.logins, 1)
				say("230 welcome")
			} else {
				say("530 Login incorrect")
			}
		case "TYPE":
			say("200 ok")
		case "QUIT":
			say("221 bye")
			return
		case "SIZE":
			switch {
			case !authed:
				say("530 login first")
			case s.noSize:
				say("502 not implemented")
			case strings.Contains(arg, "missing"):
				say("550 no such file")
			default:
				say("213 %d", s.size)
			}
		case "MDTM":
			say("213 20240102030405")
		case "REST":
			if s.noRest {
				say("502 REST not implemented")
				break
			}
			rest, _ = strconv.ParseInt(arg, 10, 64)
			atomic.AddInt32(&s.rests, 1)
			say("350 restarting at %d", rest)
		case "EPSV", "PASV":
			if dl != nil {
				dl.Close()
			}
			dl, _ = net.Listen("tcp4", "127.0.0.1:0")
			port := dl.Addr().(*net.TCPAddr).Port
			if strings.ToUpper(cmd) == "EPSV" {
				say("229 Entering Extended Passive Mode (|||%d|)", port)
			} else {
				say("227 Entering Passive Mode (127,0,0,1,%d,%d)", port/256, port%256)
			}
		case "RETR":
			if !authed || dl == nil {
				say("425 no data connection")
				break
			}
			if strings.Contains(arg, "missing") {
				say("550 no such file")
				break
			}
			_ = dl.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
			dc, err := dl.Accept()
			dl.Close()
			dl = nil
			if err != nil {
				say("425 cannot open data connection")
				break
			}
			say("150 opening data connection")
			buf := make([]byte, 16<<10)
			off := rest
			for off < s.size {
				n := int64(len(buf))
				if off+n > s.size {
					n = s.size - off
				}
				simFill(buf[:n], off)
				if _, err := dc.Write(buf[:n]); err != nil {
					break
				}
				off += n
				s.mu.Lock()
				s.seen[ip] += n
				s.mu.Unlock()
				if s.perConn > 0 {
					time.Sleep(time.Duration(float64(n) / s.perConn * float64(time.Second)))
				}
			}
			dc.Close()
			rest = 0
			if off >= s.size {
				say("226 transfer complete")
			} else {
				say("426 transfer aborted")
			}
		default:
			say("502 not implemented")
		}
	}
}

func TestFTPDownloadOverTwoLinesWithResumeParts(t *testing.T) {
	size := int64(24*MiB + 777)
	s := newFTP(size)
	defer s.Close()
	env := newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0))
	tk, _, err := runDl(t, env, s.URL("/pub/file.bin"), dlSpec{})
	mustOK(t, tk, err, size)
	if atomic.LoadInt32(&s.rests) == 0 {
		t.Fatal("the pieces were not fetched with REST")
	}
	if s.bytesFrom("127.0.0.1") == 0 || s.bytesFrom("127.0.0.2") == 0 {
		t.Fatalf("both lines must carry data: %d / %d", s.bytesFrom("127.0.0.1"), s.bytesFrom("127.0.0.2"))
	}
}

func TestFTPLoginAndNoPasswordInTheList(t *testing.T) {
	size := int64(3 * MiB)
	s := newFTP(size, func(s *ftpSrv) { s.user, s.pass = "ann", "s3cret" })
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	// wrong password: a clear message, no endless retries
	out, _ := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: "ftp://ann:wrong@" + s.ln.Addr().String() + "/f.bin"}})
	id := out["added"].([]*dlItem)[0].ID
	v := waitState(t, m, id, "error", 30*time.Second)
	if !strings.Contains(v.Err, "refused the login") {
		t.Fatalf("%+v", v)
	}
	// right password in the link
	out, _ = m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: "ftp://ann:s3cret@" + s.ln.Addr().String() + "/g.bin"}})
	id = out["added"].([]*dlItem)[0].ID
	v = waitState(t, m, id, "done", 30*time.Second)
	if strings.Contains(v.URL, "s3cret") || strings.Contains(v.URL, "ann") {
		t.Fatalf("the login stayed in the link: %s", v.URL)
	}
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
	// login given as fields
	out, _ = m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL("/h.bin"), User: "ann", Pass: "s3cret"}})
	waitState(t, m, out["added"].([]*dlItem)[0].ID, "done", 30*time.Second)
}

func TestFTPWithoutResumeOrSizeStillWorks(t *testing.T) {
	size := int64(2*MiB + 5)
	s := newFTP(size, func(s *ftpSrv) { s.noRest = true })
	defer s.Close()
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)), s.URL("/f.bin"), dlSpec{})
	mustOK(t, tk, err, size)
	s2 := newFTP(size, func(s *ftpSrv) { s.noSize = true })
	defer s2.Close()
	tk, _, err = runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)), s2.URL("/f.bin"), dlSpec{})
	mustOK(t, tk, err, size)
}

func TestFTPMissingFileAndFolderAndEncrypted(t *testing.T) {
	s := newFTP(1 * MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, _ := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL("/missing.bin")}})
	v := waitState(t, m, out["added"].([]*dlItem)[0].ID, "error", 30*time.Second)
	if !strings.Contains(v.Err, "not found") {
		t.Fatalf("%+v", v)
	}
	out, _ = m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: s.URL("/pub/")}})
	v = waitState(t, m, out["added"].([]*dlItem)[0].ID, "error", 30*time.Second)
	if !strings.Contains(v.Err, "folder") {
		t.Fatalf("%+v", v)
	}
	if _, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: "ftps://example.com/a.bin"}}); err == nil || !strings.Contains(err.Error(), "Encrypted FTP") {
		t.Fatalf("ftps: %v", err)
	}
}

func TestFTPServerThatAllowsFewLogins(t *testing.T) {
	size := int64(12 * MiB)
	s := newFTP(size, func(s *ftpSrv) { s.maxLogins = 2 })
	defer s.Close()
	tk, _, err := runDl(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)), s.URL("/f.bin"), dlSpec{})
	mustOK(t, tk, err, size)
}

func TestFTPPauseResumeKeepsProgress(t *testing.T) {
	size := int64(20 * MiB)
	s := newFTP(size, func(s *ftpSrv) { s.perConn = 1 * MiB })
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	id := addOne(t, m, s.URL("/f.bin"))
	waitState(t, m, id, "downloading", 10*time.Second)
	time.Sleep(2 * time.Second)
	_ = m.pause(id)
	waitState(t, m, id, "paused", 10*time.Second)
	m.mu.Lock()
	got := m.find(id).Done
	m.mu.Unlock()
	if got == 0 {
		t.Fatal("nothing was kept")
	}
	_ = m.resume(id)
	v := waitState(t, m, id, "done", 60*time.Second)
	if fileHash(t, v.Path) != simHash(size) {
		t.Fatal("content")
	}
}

func TestFTPProbeInTheAddDialog(t *testing.T) {
	s := newFTP(5 * MiB)
	defer s.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0), newSimLine("B", "127.0.0.2", 0)))
	out, err := m.handle(dlReq{Action: "probe", dlAdd: dlAdd{URL: s.URL("/dir/big file.iso")}})
	if err != nil || out["ok"] != true {
		t.Fatalf("%v %v", out, err)
	}
	info := out["info"].(dlInfo)
	if info.Size != 5*MiB || !info.Ranges || info.Name != "big file.iso" {
		t.Fatalf("%+v", info)
	}
}
