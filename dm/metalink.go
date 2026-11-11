package dm

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- Metalink: one file that lists a download with all its mirrors and its checksum ----------------------------------
//
// Both versions are read (.meta4 = version 4, .metalink = version 3 or 4). Each file in it becomes a download: the best
// address first, the others as mirrors, with the checksum, the size and the name the file gives.

type mlHash struct {
	Type string `xml:"type,attr"`
	Sum  string `xml:",chardata"`
}

type mlURL struct {
	Type       string `xml:"type,attr"`       // version 3: "http", "ftp", "bittorrent"...
	Priority   int    `xml:"priority,attr"`   // version 4: 1 is best
	Preference int    `xml:"preference,attr"` // version 3: 100 is best
	Addr       string `xml:",chardata"`
}

type mlFile struct {
	Name   string   `xml:"name,attr"`
	Size   int64    `xml:"size"`
	Hashes []mlHash `xml:"hash"`
	Verify struct {
		Hashes []mlHash `xml:"hash"`
	} `xml:"verification"`
	URLs []mlURL `xml:"url"`
	Res  struct {
		URLs []mlURL `xml:"url"`
	} `xml:"resources"`
}

type mlDoc struct {
	XMLName xml.Name
	Files   []mlFile `xml:"file"`
	Wrapped struct {
		Files []mlFile `xml:"file"`
	} `xml:"files"`
}

// metaEntry is one download taken from a Metalink file.
type metaEntry struct {
	URL     string
	Mirrors []string
	Name    string
	Hash    string
	Size    int64
}

func dlIsMetalink(link string) bool {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	return strings.HasSuffix(p, ".meta4") || strings.HasSuffix(p, ".metalink")
}

func parseMetalink(data []byte) ([]metaEntry, error) {
	var d mlDoc
	if err := xml.Unmarshal(data, &d); err != nil || !strings.EqualFold(d.XMLName.Local, "metalink") {
		return nil, errors.New("that is not a Metalink file")
	}
	files := append(d.Files, d.Wrapped.Files...)
	if len(files) == 0 {
		return nil, errors.New("the Metalink file lists no files")
	}
	if len(files) > 100 {
		return nil, errors.New("the Metalink file lists too many files (100 at most)")
	}
	var out []metaEntry
	for _, f := range files {
		type cand struct {
			addr  string
			score int // higher is better
		}
		var cs []cand
		for _, u := range append(append([]mlURL{}, f.URLs...), f.Res.URLs...) {
			a := strings.TrimSpace(u.Addr)
			low := strings.ToLower(a)
			if !(strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "ftp://")) {
				continue // torrents and other kinds are not used here
			}
			if u.Type != "" && !strings.EqualFold(u.Type, "http") && !strings.EqualFold(u.Type, "https") && !strings.EqualFold(u.Type, "ftp") {
				continue
			}
			if _, err := dlCleanURL(a); err != nil {
				continue // an address this program would refuse is left out, not the whole file
			}
			score := u.Preference
			if u.Priority > 0 {
				score = 1000 - u.Priority
			}
			cs = append(cs, cand{a, score})
		}
		if len(cs) == 0 {
			continue
		}
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].score > cs[j].score })
		e := metaEntry{URL: cs[0].addr, Size: f.Size}
		for _, c := range cs[1:] {
			if len(e.Mirrors) < 8 {
				e.Mirrors = append(e.Mirrors, c.addr)
			}
		}
		if n := dlSafeName(strings.TrimSpace(f.Name)); n != "" && n != "." && n != ".." {
			e.Name = n
		}
		best := -1
		for _, h := range append(append([]mlHash{}, f.Hashes...), f.Verify.Hashes...) {
			sum := strings.ToLower(strings.TrimSpace(h.Sum))
			rank := map[int]int{64: 3, 40: 2, 32: 1}[len(sum)] // sha-256, sha-1, md5
			if _, err := dlCleanHash(sum); err == nil && rank > best {
				best, e.Hash = rank, sum
			}
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, errors.New("the Metalink file has no web or FTP addresses to download from")
	}
	return out, nil
}

// fetchSmall reads a small text file from the web through the first usable line (a Metalink file, 2 MB at most).
func (m *dlManager) fetchSmall(link string, hdr map[string]string) ([]byte, error) {
	lines := m.lines()
	if len(lines) == 0 {
		return nil, errors.New("no internet connection is available right now")
	}
	private := dlLocalHost(dlHostOf(link))
	var last error
	for _, l := range lines {
		b, err := func() ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c := dlLineClient(l, private)
			defer c.CloseIdleConnections()
			req, err := dlRequest(ctx, link, dlWithSavedLogin(link, hdr))
			if err != nil {
				return nil, err
			}
			resp, err := c.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, dlHTTPErr{Code: resp.StatusCode, Server: resp.Header.Get("Server")}
			}
			b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
			if err == nil && len(b) > 2<<20 {
				err = errors.New("the Metalink file is too large")
			}
			return b, err
		}()
		if err == nil {
			return b, nil
		}
		last = err
	}
	msg, _ := dlFriendly(last)
	return nil, errors.New(msg)
}

// addMetalink downloads the Metalink file at link and adds every file in it. base carries what the person chose
// (folder, queue, connections, speed limit, ...); the addresses, mirrors, checksum and name come from the Metalink file.
func (m *dlManager) addMetalink(link string, base dlAdd) ([]*dlItem, int, error) {
	link, err := dlCleanURL(link)
	if err != nil {
		return nil, 0, err
	}
	data, err := m.fetchSmall(link, dlCleanHeader(base.Header))
	if err != nil {
		return nil, 0, err
	}
	entries, err := parseMetalink(data)
	if err != nil {
		return nil, 0, err
	}
	var added []*dlItem
	skipped := 0
	for _, e := range entries {
		a := base
		a.URL, a.Mirrors, a.Hash = e.URL, e.Mirrors, e.Hash
		if e.Name != "" {
			a.Name = e.Name
		}
		it, err := m.add(a)
		if err != nil {
			if errors.Is(err, errDlDuplicate) {
				skipped++
				continue
			}
			hLog(fmt.Sprintf("metalink: %s could not be added: %v", strconv.Quote(e.Name), err))
			continue
		}
		added = append(added, it)
	}
	if len(added) == 0 && skipped == 0 {
		return nil, 0, errors.New("none of the files in the Metalink file could be added")
	}
	return added, skipped, nil
}
