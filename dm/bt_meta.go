package dm

// BitTorrent: bencoding, .torrent files and magnet links.

import (
	"bytes"
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
)

// ---- bencode ---------------------------------------------------------------------------------------------------------

type bdec struct {
	b     []byte
	i     int
	depth int
}

var errBencode = errors.New("the torrent data is damaged")

func bdecode(b []byte) (any, error) {
	d := &bdec{b: b}
	v, err := d.value()
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (d *bdec) value() (any, error) {
	if d.i >= len(d.b) {
		return nil, errBencode
	}
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > 40 {
		return nil, errBencode
	}
	switch c := d.b[d.i]; {
	case c == 'i':
		d.i++
		j := bytes.IndexByte(d.b[d.i:], 'e')
		if j < 1 || j > 24 {
			return nil, errBencode
		}
		n, err := strconv.ParseInt(string(d.b[d.i:d.i+j]), 10, 64)
		if err != nil {
			return nil, errBencode
		}
		d.i += j + 1
		return n, nil
	case c >= '0' && c <= '9':
		j := bytes.IndexByte(d.b[d.i:], ':')
		if j < 1 || j > 12 {
			return nil, errBencode
		}
		n, err := strconv.Atoi(string(d.b[d.i : d.i+j]))
		if err != nil || n < 0 || d.i+j+1+n > len(d.b) {
			return nil, errBencode
		}
		s := d.b[d.i+j+1 : d.i+j+1+n]
		d.i += j + 1 + n
		return string(s), nil
	case c == 'l':
		d.i++
		var out []any
		for {
			if d.i >= len(d.b) {
				return nil, errBencode
			}
			if d.b[d.i] == 'e' {
				d.i++
				return out, nil
			}
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			if len(out) > 200000 {
				return nil, errBencode
			}
		}
	case c == 'd':
		d.i++
		out := map[string]any{}
		for {
			if d.i >= len(d.b) {
				return nil, errBencode
			}
			if d.b[d.i] == 'e' {
				d.i++
				return out, nil
			}
			k, err := d.value()
			ks, ok := k.(string)
			if err != nil || !ok {
				return nil, errBencode
			}
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			out[ks] = v
			if len(out) > 100000 {
				return nil, errBencode
			}
		}
	}
	return nil, errBencode
}

// bdecodePrefix decodes the first value of b and says how many bytes it used (peer messages carry data after it).
func bdecodePrefix(b []byte) (any, int, error) {
	d := &bdec{b: b}
	v, err := d.value()
	return v, d.i, err
}

func bencode(v any) []byte {
	var buf bytes.Buffer
	benc(&buf, v)
	return buf.Bytes()
}

func benc(buf *bytes.Buffer, v any) {
	switch x := v.(type) {
	case int:
		fmt.Fprintf(buf, "i%de", x)
	case int64:
		fmt.Fprintf(buf, "i%de", x)
	case string:
		fmt.Fprintf(buf, "%d:%s", len(x), x)
	case []byte:
		fmt.Fprintf(buf, "%d:", len(x))
		buf.Write(x)
	case []any:
		buf.WriteByte('l')
		for _, e := range x {
			benc(buf, e)
		}
		buf.WriteByte('e')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('d')
		for _, k := range keys {
			benc(buf, k)
			benc(buf, x[k])
		}
		buf.WriteByte('e')
	}
}

// topInfoRaw finds the raw bytes of the "info" value in a torrent file, so the info hash is exact.
func topInfoRaw(b []byte) ([]byte, error) {
	if len(b) < 2 || b[0] != 'd' {
		return nil, errBencode
	}
	d := &bdec{b: b, i: 1}
	for d.i < len(b) && b[d.i] != 'e' {
		k, err := d.value()
		ks, ok := k.(string)
		if err != nil || !ok {
			return nil, errBencode
		}
		start := d.i
		if _, err := d.value(); err != nil {
			return nil, err
		}
		if ks == "info" {
			return b[start:d.i], nil
		}
	}
	return nil, errors.New("this is not a torrent file (no info section)")
}

// ---- torrent files -----------------------------------------------------------------------------------------------------

type btFile struct {
	Path   []string
	Length int64
	off    int64 // where the file starts in the whole torrent
}

func (f btFile) name() string { return strings.Join(f.Path, "/") }

type btMeta struct {
	Name     string
	PieceLen int64
	Hashes   []byte // 20 bytes per piece
	Files    []btFile
	Total    int64
	InfoHash [20]byte
	Trackers []string
	Private  bool
	Raw      []byte // the info dictionary as found in the file
	Multi    bool
}

func (m *btMeta) pieces() int { return len(m.Hashes) / 20 }
func (m *btMeta) pieceSize(i int) int64 {
	if int64(i+1)*m.PieceLen <= m.Total {
		return m.PieceLen
	}
	return m.Total - int64(i)*m.PieceLen
}
func (m *btMeta) hashOf(i int) []byte { return m.Hashes[i*20 : i*20+20] }

func btSafePart(s string) (string, bool) {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\\x00") || strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") {
		return "", false
	}
	return dlSafeName(s), true
}

func btStr(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func btInt(m map[string]any, k string) int64  { n, _ := m[k].(int64); return n }

// btParseInfo builds the description from the info dictionary (also used for metadata fetched from peers).
func btParseInfo(raw []byte) (*btMeta, error) {
	v, err := bdecode(raw)
	if err != nil {
		return nil, err
	}
	info, ok := v.(map[string]any)
	if !ok {
		return nil, errBencode
	}
	m := &btMeta{Raw: raw, InfoHash: sha1.Sum(raw), PieceLen: btInt(info, "piece length"), Hashes: []byte(btStr(info, "pieces")), Private: btInt(info, "private") == 1}
	if m.PieceLen < 16<<10 || m.PieceLen > 128<<20 || len(m.Hashes) == 0 || len(m.Hashes)%20 != 0 || len(m.Hashes)/20 > 1<<21 {
		return nil, errors.New("the torrent has an unusable piece size")
	}
	name := btStr(info, "name.utf-8")
	if name == "" {
		name = btStr(info, "name")
	}
	if n, ok := btSafePart(name); ok {
		m.Name = n
	} else {
		m.Name = "torrent-" + hex.EncodeToString(m.InfoHash[:4])
	}
	if fl, ok := info["files"].([]any); ok {
		m.Multi = true
		if len(fl) == 0 || len(fl) > 50000 {
			return nil, errors.New("the torrent has no files or too many")
		}
		var off int64
		for _, e := range fl {
			fm, ok := e.(map[string]any)
			if !ok {
				return nil, errBencode
			}
			pl := fm["path.utf-8"]
			if pl == nil {
				pl = fm["path"]
			}
			pp, ok := pl.([]any)
			if !ok || len(pp) == 0 {
				return nil, errBencode
			}
			var parts []string
			for _, p := range pp {
				s, _ := p.(string)
				c, ok := btSafePart(s)
				if !ok {
					return nil, errors.New("the torrent has a file with an unsafe name")
				}
				parts = append(parts, c)
			}
			ln := btInt(fm, "length")
			if ln < 0 || off+ln < off {
				return nil, errBencode
			}
			m.Files = append(m.Files, btFile{Path: parts, Length: ln, off: off})
			off += ln
		}
		m.Total = off
		if err := btNoCollisions(m.Files); err != nil {
			return nil, err
		}
	} else {
		ln := btInt(info, "length")
		if ln <= 0 {
			return nil, errors.New("the torrent has no length")
		}
		m.Files = []btFile{{Path: []string{m.Name}, Length: ln}}
		m.Total = ln
	}
	if m.Total <= 0 || (m.Total+m.PieceLen-1)/m.PieceLen != int64(m.pieces()) {
		return nil, errors.New("the torrent's size and piece list do not match")
	}
	return m, nil
}

func btParseTorrent(b []byte) (*btMeta, error) {
	raw, err := topInfoRaw(b)
	if err != nil {
		return nil, err
	}
	m, err := btParseInfo(raw)
	if err != nil {
		return nil, err
	}
	top, _ := bdecode(b)
	tm, _ := top.(map[string]any)
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] && len(m.Trackers) < 24 {
			seen[s] = true
			m.Trackers = append(m.Trackers, s)
		}
	}
	if tiers, ok := tm["announce-list"].([]any); ok {
		for _, t := range tiers {
			if l, ok := t.([]any); ok {
				for _, u := range l {
					if s, ok := u.(string); ok {
						add(s)
					}
				}
			}
		}
	}
	add(btStr(tm, "announce"))
	return m, nil
}

// btTorrentFile makes a small torrent file from an info dictionary (kept on disk to resume a magnet link).
func btTorrentFile(m *btMeta) []byte {
	var b bytes.Buffer
	b.WriteString("d")
	if len(m.Trackers) > 0 {
		var l []any
		for _, t := range m.Trackers {
			l = append(l, t)
		}
		b.Write(bencode("announce-list"))
		b.Write(bencode([]any{l}))
	}
	b.Write(bencode("info"))
	b.Write(m.Raw)
	b.WriteString("e")
	return b.Bytes()
}

// ---- magnet links ------------------------------------------------------------------------------------------------------

type btMagnet struct {
	Hash     [20]byte
	Name     string
	Trackers []string
	Peers    []string
}

func btParseMagnet(s string) (*btMagnet, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || !strings.EqualFold(u.Scheme, "magnet") {
		return nil, errors.New("that is not a magnet link")
	}
	q := u.Query()
	m := &btMagnet{Name: q.Get("dn")}
	found := false
	for _, xt := range q["xt"] {
		const p = "urn:btih:"
		if !strings.HasPrefix(strings.ToLower(xt), p) {
			continue
		}
		h := xt[len(p):]
		switch len(h) {
		case 40:
			b, err := hex.DecodeString(h)
			if err != nil {
				return nil, errors.New("the magnet link has a damaged hash")
			}
			copy(m.Hash[:], b)
			found = true
		case 32:
			b, err := base32.StdEncoding.DecodeString(strings.ToUpper(h))
			if err != nil || len(b) != 20 {
				return nil, errors.New("the magnet link has a damaged hash")
			}
			copy(m.Hash[:], b)
			found = true
		}
	}
	if !found {
		return nil, errors.New("the magnet link has no torrent hash (BitTorrent v1)")
	}
	for _, t := range q["tr"] {
		if len(m.Trackers) < 24 && t != "" {
			m.Trackers = append(m.Trackers, t)
		}
	}
	for _, p := range q["x.pe"] {
		if len(m.Peers) < 50 {
			m.Peers = append(m.Peers, p)
		}
	}
	if n, ok := btSafePart(m.Name); ok {
		m.Name = n
	} else {
		m.Name = ""
	}
	return m, nil
}

// btMagnetLink is the canonical address of a torrent in the list (also used to spot duplicates).
func btMagnetLink(h [20]byte, name string, trackers []string) string {
	v := url.Values{}
	v.Set("xt", "urn:btih:"+hex.EncodeToString(h[:]))
	if name != "" {
		v.Set("dn", name)
	}
	s := "magnet:?" + strings.ReplaceAll(v.Encode(), "%3A", ":")
	for i, t := range trackers {
		if i >= 8 {
			break
		}
		s += "&tr=" + url.QueryEscape(t)
	}
	return s
}

func btLooksLikeTorrentURL(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(path.Base(u.Path)), ".torrent")
}

// btNoCollisions refuses a torrent in which two files would end up in the same place: the same path twice, a file that is
// also a folder of another file ("a" and "a/b"), or names that differ only in capital letters (Windows and macOS see
// them as one). The names were already cleaned (reserved names, forbidden letters), so cleaned-up clashes show here too.
func btNoCollisions(files []btFile) error {
	paths := map[string]bool{}
	dirs := map[string]bool{}
	for _, f := range files {
		key := strings.ToLower(strings.Join(f.Path, "/"))
		if paths[key] || dirs[key] {
			return errors.New("the torrent has two files with the same name or place: " + strings.Join(f.Path, "/"))
		}
		paths[key] = true
		for i := 1; i < len(f.Path); i++ {
			d := strings.ToLower(strings.Join(f.Path[:i], "/"))
			if paths[d] {
				return errors.New("the torrent has a file that is also a folder: " + strings.Join(f.Path[:i], "/"))
			}
			dirs[d] = true
		}
	}
	return nil
}
