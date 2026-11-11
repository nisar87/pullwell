package dm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const meta4Doc = `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
 <file name="../evil/name.bin">
  <size>1234</size>
  <hash type="md5">0123456789abcdef0123456789abcdef</hash>
  <hash type="sha-256">%s</hash>
  <url location="de" priority="2">https://second.example.org/f.bin</url>
  <url location="us" priority="1">https://first.example.org/f.bin</url>
  <url priority="3">file:///etc/passwd</url>
  <metaurl mediatype="torrent">https://x.example.org/f.torrent</metaurl>
 </file>
 <file name="b.iso"><url>ftp://ftp.example.org/b.iso</url></file>
</metalink>`

const meta3Doc = `<?xml version="1.0" encoding="utf-8"?>
<metalink version="3.0" xmlns="http://www.metalinker.org/">
 <files>
  <file name="c.zip">
   <size>99</size>
   <verification><hash type="sha1">0123456789abcdef0123456789abcdef01234567</hash></verification>
   <resources>
    <url type="http" preference="50">http://low.example.org/c.zip</url>
    <url type="http" preference="100">http://high.example.org/c.zip</url>
    <url type="bittorrent" preference="100">http://t.example.org/c.torrent</url>
   </resources>
  </file>
 </files>
</metalink>`

func TestParseMetalinkVersion4(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	es, err := parseMetalink([]byte(fmt.Sprintf(meta4Doc, sum)))
	if err != nil || len(es) != 2 {
		t.Fatalf("%v %+v", err, es)
	}
	e := es[0]
	if e.URL != "https://first.example.org/f.bin" || len(e.Mirrors) != 1 || e.Mirrors[0] != "https://second.example.org/f.bin" {
		t.Fatalf("the best address goes first, the others are mirrors: %+v", e)
	}
	if e.Name != "name.bin" {
		t.Fatalf("the name must not leave the folder: %q", e.Name)
	}
	if e.Hash != sum {
		t.Fatalf("sha-256 beats md5: %q", e.Hash)
	}
	if es[1].URL != "ftp://ftp.example.org/b.iso" || es[1].Name != "b.iso" {
		t.Fatalf("%+v", es[1])
	}
}

func TestParseMetalinkVersion3(t *testing.T) {
	es, err := parseMetalink([]byte(meta3Doc))
	if err != nil || len(es) != 1 {
		t.Fatalf("%v %+v", err, es)
	}
	if es[0].URL != "http://high.example.org/c.zip" || len(es[0].Mirrors) != 1 || es[0].Hash != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("%+v", es[0])
	}
}

func TestParseMetalinkRefusesOtherFiles(t *testing.T) {
	for _, bad := range []string{"", "not xml", "<html><body>hi</body></html>", `<metalink xmlns="urn:ietf:params:xml:ns:metalink"></metalink>`,
		`<metalink xmlns="urn:ietf:params:xml:ns:metalink"><file name="a"><url>file:///etc/passwd</url></file></metalink>`} {
		if _, err := parseMetalink([]byte(bad)); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestIsMetalinkLink(t *testing.T) {
	for l, want := range map[string]bool{"https://a/x.meta4": true, "https://a/x.METALINK?dl=1": true, "https://a/x.zip": false, "https://a/meta4": false} {
		if dlIsMetalink(l) != want {
			t.Errorf("%s", l)
		}
	}
}

// A Metalink file is fetched, its file is downloaded from the best working address and checked against its checksum.
func TestMetalinkDownloadUsesMirrorAndChecksum(t *testing.T) {
	data := []byte(strings.Repeat("metalink!", 60000))
	sum := sha256.Sum256(data)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "x", time.Unix(1e9, 0), strings.NewReader(string(data)))
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "gone", 404) }))
	defer bad.Close()
	var metaURL string
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<metalink xmlns="urn:ietf:params:xml:ns:metalink"><file name="from-meta.bin"><size>%d</size><hash type="sha-256">%s</hash>
 <url priority="1">%s/f.bin</url><url priority="2">%s/f.bin</url></file></metalink>`, len(data), hex.EncodeToString(sum[:]), bad.URL, good.URL)
	}))
	defer meta.Close()
	metaURL = meta.URL + "/list.meta4"
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	out, err := m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: metaURL}})
	if err != nil {
		t.Fatal(err)
	}
	items := out["added"].([]*dlItem)
	if len(items) != 1 || items[0].Name != "from-meta.bin" {
		t.Fatalf("%+v", items)
	}
	v := waitState(t, m, items[0].ID, "done", 40*time.Second)
	if v.Size != int64(len(data)) {
		t.Fatalf("%+v", v)
	}
	deadline := time.Now().Add(10 * time.Second)
	for v.Verify != "ok" && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		for _, x := range m.views() {
			if x.ID == items[0].ID {
				v = x
			}
		}
	}
	if v.Verify != "ok" {
		t.Fatalf("the checksum from the Metalink file was not checked: %+v", v)
	}
	// the same list again: nothing new is added, and that is reported as a duplicate
	out, err = m.handle(dlReq{Action: "add", dlAdd: dlAdd{URL: metaURL}})
	if err != nil || out["duplicate"] != true {
		t.Fatalf("%v %v", out, err)
	}
}
