package dm

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Video streams (HLS, ".m3u8"): the playlist lists many small parts. NetMerge fetches the parts over all lines at once
// (a fast line simply finishes more of them), decrypts them when the stream uses plain AES-128, and joins them into
// one video file. Live streams, copy-protected (DRM) streams and streams that keep sound in a separate part are
// refused with a clear message. MPEG-DASH (".mpd") is not supported.

type dlStreamErr string

func (e dlStreamErr) Error() string { return string(e) }

const (
	hlsMaxSegs     = 100000
	hlsMaxSegBytes = 256 << 20
	hlsMaxTries    = 8
)

type hlsSeg struct {
	uri      string
	off, len int64 // len <= 0: the whole file
	keyURI   string
	iv       []byte // nil: the part number
	seq      int
	dur      float64
}

type hlsMedia struct {
	segs     []hlsSeg
	mapURI   string
	mapOff   int64
	mapLen   int64
	endList  bool
	duration float64
	fmp4     bool
	protectd bool // copy protection we cannot handle
}

type hlsVariant struct {
	uri       string
	bandwidth int64
	res       string
	audio     string // group id of a separate sound track, when it has its own playlist
}

type hlsMaster struct {
	variants []hlsVariant
	audioURI map[string]string // group id -> playlist of its (default) rendition, for sound that is kept apart
}

func dlHLSLike(info dlInfo) bool {
	if strings.Contains(info.Type, "mpegurl") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(dlPathOf(info.FinalURL)), ".m3u8")
}

func dlDASHLike(info dlInfo) bool {
	return strings.Contains(info.Type, "dash+xml") || strings.HasSuffix(strings.ToLower(dlPathOf(info.FinalURL)), ".mpd")
}

func dlPathOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return p.Path
}

func hlsAttrs(s string) map[string]string {
	out := map[string]string{}
	for len(s) > 0 {
		i := strings.IndexByte(s, '=')
		if i < 0 {
			break
		}
		k := strings.ToUpper(strings.TrimSpace(s[:i]))
		s = s[i+1:]
		var v string
		if strings.HasPrefix(s, "\"") {
			j := strings.IndexByte(s[1:], '"')
			if j < 0 {
				v, s = s[1:], ""
			} else {
				v, s = s[1:1+j], s[j+2:]
			}
		} else {
			j := strings.IndexByte(s, ',')
			if j < 0 {
				v, s = s, ""
			} else {
				v, s = s[:j], s[j:]
			}
		}
		s = strings.TrimPrefix(strings.TrimSpace(s), ",")
		out[k] = v
	}
	return out
}

func hlsResolve(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", err
	}
	u := b.ResolveReference(r)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", dlStreamErr("The stream points to a link that is not a web address.")
	}
	return u.String(), nil
}

func hlsByteRange(v string, prevEnd int64) (off, n int64) {
	parts := strings.SplitN(v, "@", 2)
	n, _ = strconv.ParseInt(parts[0], 10, 64)
	if len(parts) == 2 {
		off, _ = strconv.ParseInt(parts[1], 10, 64)
	} else {
		off = prevEnd
	}
	return
}

// hlsParse reads a playlist. Exactly one of the results is not nil.
func hlsParse(body, base string) (*hlsMaster, *hlsMedia, error) {
	body = strings.TrimPrefix(body, "\xef\xbb\xbf")
	lines := strings.Split(strings.ReplaceAll(body, "\r", ""), "\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "#EXTM3U") {
		return nil, nil, dlStreamErr("This is not a video playlist.")
	}
	isMaster := false
	for _, l := range lines {
		if strings.HasPrefix(l, "#EXT-X-STREAM-INF") {
			isMaster = true
			break
		}
	}
	if isMaster {
		m := &hlsMaster{audioURI: map[string]string{}}
		var pending *hlsVariant
		for _, raw := range lines {
			l := strings.TrimSpace(raw)
			switch {
			case strings.HasPrefix(l, "#EXT-X-MEDIA:"):
				a := hlsAttrs(strings.TrimPrefix(l, "#EXT-X-MEDIA:"))
				if strings.EqualFold(a["TYPE"], "AUDIO") && a["URI"] != "" {
					u, err := hlsResolve(base, a["URI"])
					if err == nil && (m.audioURI[a["GROUP-ID"]] == "" || strings.EqualFold(a["DEFAULT"], "YES")) {
						m.audioURI[a["GROUP-ID"]] = u
					}
				}
			case strings.HasPrefix(l, "#EXT-X-STREAM-INF:"):
				a := hlsAttrs(strings.TrimPrefix(l, "#EXT-X-STREAM-INF:"))
				bw, _ := strconv.ParseInt(a["BANDWIDTH"], 10, 64)
				pending = &hlsVariant{bandwidth: bw, res: a["RESOLUTION"], audio: a["AUDIO"]}
			case l == "" || strings.HasPrefix(l, "#"):
			default:
				if pending != nil {
					u, err := hlsResolve(base, l)
					if err != nil {
						return nil, nil, err
					}
					pending.uri = u
					m.variants = append(m.variants, *pending)
					pending = nil
				}
			}
		}
		if len(m.variants) == 0 {
			return nil, nil, dlStreamErr("The video playlist lists no video.")
		}
		return m, nil, nil
	}
	md := &hlsMedia{}
	seq := 0
	var curKey, curIV string
	var keyOK = true
	var dur float64
	var br string
	var prevEnd int64
	var prevURI string
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(l, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l, "#EXT-X-MEDIA-SEQUENCE:")))
		case strings.HasPrefix(l, "#EXTINF:"):
			f := strings.TrimPrefix(l, "#EXTINF:")
			if i := strings.IndexByte(f, ','); i >= 0 {
				f = f[:i]
			}
			dur, _ = strconv.ParseFloat(strings.TrimSpace(f), 64)
		case strings.HasPrefix(l, "#EXT-X-BYTERANGE:"):
			br = strings.TrimSpace(strings.TrimPrefix(l, "#EXT-X-BYTERANGE:"))
		case strings.HasPrefix(l, "#EXT-X-KEY:"):
			a := hlsAttrs(strings.TrimPrefix(l, "#EXT-X-KEY:"))
			switch strings.ToUpper(a["METHOD"]) {
			case "NONE":
				curKey, curIV, keyOK = "", "", true
			case "AES-128":
				if kf := a["KEYFORMAT"]; kf != "" && !strings.EqualFold(kf, "identity") {
					md.protectd = true
				}
				u, err := hlsResolve(base, a["URI"])
				if err != nil || a["URI"] == "" {
					md.protectd = true
				}
				curKey, curIV, keyOK = u, a["IV"], true
			default: // SAMPLE-AES and others
				md.protectd = true
				keyOK = false
			}
		case strings.HasPrefix(l, "#EXT-X-MAP:"):
			a := hlsAttrs(strings.TrimPrefix(l, "#EXT-X-MAP:"))
			u, err := hlsResolve(base, a["URI"])
			if err != nil {
				return nil, nil, err
			}
			md.mapURI, md.fmp4 = u, true
			if a["BYTERANGE"] != "" {
				md.mapOff, md.mapLen = hlsByteRange(a["BYTERANGE"], 0)
			}
		case l == "#EXT-X-ENDLIST":
			md.endList = true
		case l == "" || strings.HasPrefix(l, "#"):
		default:
			u, err := hlsResolve(base, l)
			if err != nil {
				return nil, nil, err
			}
			s := hlsSeg{uri: u, seq: seq, dur: dur, keyURI: curKey}
			if !keyOK {
				md.protectd = true
			}
			if curKey != "" && curIV != "" {
				h := strings.TrimPrefix(strings.TrimPrefix(curIV, "0x"), "0X")
				for len(h) < 32 {
					h = "0" + h
				}
				iv, err := hex.DecodeString(h)
				if err != nil || len(iv) != 16 {
					md.protectd = true
				}
				s.iv = iv
			}
			if br != "" {
				if prevURI != u {
					prevEnd = 0
				}
				s.off, s.len = hlsByteRange(br, prevEnd)
				prevEnd, prevURI = s.off+s.len, u
				br = ""
			} else {
				prevURI = ""
			}
			if strings.HasSuffix(strings.ToLower(dlPathOf(u)), ".m4s") || strings.HasSuffix(strings.ToLower(dlPathOf(u)), ".mp4") || strings.HasSuffix(strings.ToLower(dlPathOf(u)), ".cmfv") {
				md.fmp4 = true
			}
			md.segs = append(md.segs, s)
			md.duration += dur
			seq++
			dur = 0
			if len(md.segs) > hlsMaxSegs {
				return nil, nil, dlStreamErr("This stream has too many parts.")
			}
		}
	}
	return nil, md, nil
}

// dlHeightKey carries the wanted picture height (a cap; 0 = the best there is) down to the playlist readers.
type dlHeightKey struct{}

func dlWantHeight(ctx context.Context) int {
	h, _ := ctx.Value(dlHeightKey{}).(int)
	return h
}

// hlsHeight reads the height out of "1280x720".
func hlsHeight(res string) int {
	if i := strings.IndexAny(res, "xX"); i > 0 {
		h, _ := strconv.Atoi(strings.TrimSpace(res[i+1:]))
		return h
	}
	return 0
}

// dlPickHeight chooses among heights (any order): the largest not above the cap; if all are above it, the smallest.
// It returns the index. Zero cap means the largest.
func dlPickHeight(heights []int, bw []int64, cap int) int {
	best := -1
	for i, h := range heights {
		if cap > 0 && h > cap && h != 0 {
			continue
		}
		if best < 0 || h > heights[best] || (h == heights[best] && bw[i] > bw[best]) {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	best = 0
	for i, h := range heights {
		if h < heights[best] {
			best = i
		}
	}
	return best
}

func hlsHeights(m *hlsMaster) []int {
	seen := map[int]bool{}
	var out []int
	for _, v := range m.variants {
		if h := hlsHeight(v.res); h > 0 && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

func hlsBest(m *hlsMaster, cap int) hlsVariant {
	hs, bw := make([]int, len(m.variants)), make([]int64, len(m.variants))
	for i, v := range m.variants {
		hs[i], bw[i] = hlsHeight(v.res), v.bandwidth
	}
	if cap > 0 {
		any := false
		for _, h := range hs {
			any = any || h > 0
		}
		if any {
			return m.variants[dlPickHeight(hs, bw, cap)]
		}
		// no sizes given: use the bandwidth order instead (the cap is a height, so nothing to compare): best
	}
	best := m.variants[0]
	for _, v := range m.variants[1:] {
		if v.bandwidth > best.bandwidth {
			best = v
		}
	}
	return best
}

// hlsInfo is what the "add" dialog shows about a stream.
type hlsInfo struct {
	Segments int     `json:"segments"`
	Seconds  float64 `json:"seconds"`
	Quality  string  `json:"quality"`
	Kind     string  `json:"kind"`              // "ts" | "mp4"
	Audio    bool    `json:"audio"`             // the sound is a second track that is joined afterwards
	Heights  []int   `json:"heights,omitempty"` // picture heights on offer, largest first
	media    *hlsMedia
	audioMed *hlsMedia
	mediaURL string
}

func hlsGet(ctx context.Context, c *http.Client, link string, hdr map[string]string, off, n, limit int64) ([]byte, error) {
	req, err := dlRequest(ctx, link, hdr)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+n-1))
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return nil, dlHTTPErr{Code: resp.StatusCode, Retry: dlRetryAfter(resp)}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, dlStreamErr("A part of the stream is unusually large.")
	}
	if n > 0 && resp.StatusCode == 200 { // the server ignored the range: cut it out
		if off > int64(len(b)) {
			return nil, io.ErrUnexpectedEOF
		}
		b = b[off:]
		if int64(len(b)) > n {
			b = b[:n]
		}
	}
	if n > 0 && int64(len(b)) != n {
		return nil, io.ErrUnexpectedEOF
	}
	return b, nil
}

// dlHLSInspect reads the playlist (and, for a master playlist, the best video playlist). A nil error and a nil
// result means "this is not a playlist".
func dlHLSInspect(ctx context.Context, c *http.Client, link string, hdr map[string]string) (*hlsInfo, error) {
	b, err := hlsGet(ctx, c, link, hdr, 0, 0, 8<<20)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), []byte("#EXTM3U")) {
		return nil, nil
	}
	m, md, err := hlsParse(string(b), link)
	if err != nil {
		return nil, err
	}
	out := &hlsInfo{mediaURL: link}
	if m != nil {
		v := hlsBest(m, dlWantHeight(ctx))
		out.Heights = hlsHeights(m)
		audioURL := ""
		if v.audio != "" {
			audioURL = m.audioURI[v.audio]
		}
		out.mediaURL, out.Quality = v.uri, v.res
		if out.Quality == "" && v.bandwidth > 0 {
			out.Quality = fmt.Sprintf("%d kbit/s", v.bandwidth/1000)
		}
		b, err = hlsGet(ctx, c, v.uri, hdr, 0, 0, 8<<20)
		if err != nil {
			return nil, err
		}
		_, md, err = hlsParse(string(b), v.uri)
		if err != nil {
			return nil, err
		}
		if md == nil {
			return nil, dlStreamErr("The video playlist is not what it should be.")
		}
		if audioURL != "" {
			ab, err := hlsGet(ctx, c, audioURL, hdr, 0, 0, 8<<20)
			if err != nil {
				return nil, err
			}
			_, am, err := hlsParse(string(ab), audioURL)
			if err != nil {
				return nil, err
			}
			if am == nil || len(am.segs) == 0 {
				return nil, dlStreamErr("The sound playlist is not what it should be.")
			}
			if am.protectd {
				return nil, dlStreamErr("This stream is copy-protected. NetMerge cannot download it.")
			}
			if !am.endList {
				return nil, dlStreamErr("This is a live stream (it has no end). NetMerge only downloads recorded videos.")
			}
			out.audioMed, out.Audio = am, true
		}
	}
	switch {
	case md.protectd:
		return nil, dlStreamErr("This stream is copy-protected. NetMerge cannot download it.")
	case !md.endList:
		return nil, dlStreamErr("This is a live stream (it has no end). NetMerge only downloads recorded videos.")
	case len(md.segs) == 0:
		return nil, dlStreamErr("The video playlist has no parts.")
	}
	out.media, out.Segments, out.Seconds, out.Kind = md, len(md.segs), md.duration, "ts"
	if md.fmp4 {
		out.Kind = "mp4"
	}
	return out, nil
}

// ---- the download -------------------------------------------------------------------------------------------------

type hlsState struct {
	V      int    `json:"v"`
	Digest string `json:"digest"`
	N      int    `json:"n"`
}

func hlsDir(final string) string   { return final + ".nmseg" }
func hlsStatePath(f string) string { return f + ".nmhls" }
func hlsSegPath(dir string, i int) string {
	return fmt.Sprintf("%s%c%06d", dir, os.PathSeparator, i)
}

func hlsDigest(md *hlsMedia) string {
	h := sha256.New()
	cut := func(u string) string {
		if i := strings.IndexAny(u, "?#"); i >= 0 {
			return u[:i]
		}
		return u
	}
	fmt.Fprintf(h, "%d|%s|%d|%d\n", len(md.segs), cut(md.mapURI), md.mapOff, md.mapLen)
	for _, s := range md.segs {
		fmt.Fprintf(h, "%s|%d|%d|%d\n", cut(s.uri), s.off, s.len, s.seq)
	}
	return hex.EncodeToString(h.Sum(nil))
}

type hlsJob struct {
	t       *dlTask
	base    string // the file this track belongs to (its parts and state live next to it)
	md      *hlsMedia
	dir     string
	mu      sync.Mutex
	next    int
	retry   []int
	have    []bool
	tries   []int
	nHave   int
	keys    map[string][]byte
	keyMu   sync.Mutex
	bytes   int64 // atomic: size of finished parts
	baseSz  int64
	estSeen int
}

// runHLS returns handled=false when the address turned out not to be a playlist (the normal download goes on).
func (t *dlTask) runHLS() (bool, error) {
	var info *hlsInfo
	var err error
	for _, l := range t.usableLines() {
		pctx, c := context.WithTimeout(t.ctx, 40*time.Second)
		pc := dlLineClient(l, t.private)
		t.mu.Lock()
		t.probeClients = append(t.probeClients, pc)
		t.mu.Unlock()
		info, err = dlHLSInspect(context.WithValue(pctx, dlHeightKey{}, t.spec.MaxHeight), pc, t.info.FinalURL, t.hdr)
		c()
		if err == nil || t.ctx.Err() != nil {
			break
		}
		var se dlStreamErr
		if errors.As(err, &se) {
			break
		}
	}
	if t.ctx.Err() != nil {
		return true, t.ctx.Err()
	}
	if err != nil {
		return true, err
	}
	if info == nil {
		return false, nil
	}
	return true, t.startStream(info, ".m3u8")
}

// startStream names the file, asks where to save it and downloads the stream (HLS or DASH).
func (t *dlTask) startStream(info *hlsInfo, playlistExt string) error {
	name := t.info.Name
	if ext := filepath.Ext(name); strings.EqualFold(ext, playlistExt) || strings.EqualFold(ext, ".m3u") {
		name = name[:len(name)-len(ext)]
	}
	if name == "" || name == "download" {
		name = "video"
	}
	t.info.Name = name + "." + map[string]string{"ts": "ts", "mp4": "mp4", "m4a": "m4a"}[info.Kind]
	t.info.Size, t.info.Ranges = -1, false
	if t.spec.Path == "" {
		if t.spec.Pick == nil {
			return errors.New("no place to save the file")
		}
		p, err := t.spec.Pick(t.info)
		if err != nil {
			return err
		}
		t.spec.Path = p
	}
	if t.spec.OnInfo != nil {
		t.spec.OnInfo(t.info, t.spec.Path)
	}
	return t.downloadStream(info)
}

func (t *dlTask) downloadStream(info *hlsInfo) error {
	audioPath := ""
	if info.audioMed != nil {
		ext := ".aac"
		if info.audioMed.fmp4 {
			ext = ".m4a"
		}
		audioPath = t.spec.Path + ".audio" + ext
		if fi, err := os.Stat(audioPath); err != nil || fi.Size() == 0 {
			t.setNote("Downloading the sound track")
			aj, err := t.fetchTrack(info.audioMed, t.spec.Path+".audio")
			if err != nil {
				return err
			}
			n, err := aj.joinFile(audioPath)
			if err != nil {
				return err
			}
			atomic.AddInt64(&t.hlsPrev, n)
		}
	}
	j, err := t.fetchTrack(info.media, t.spec.Path)
	if err != nil {
		return err
	}
	if err := j.join(); err != nil {
		return err
	}
	if audioPath != "" {
		t.mux(audioPath)
	}
	return nil
}

// fetchTrack downloads all parts of one track over all lines (no joining yet).
func (t *dlTask) fetchTrack(md *hlsMedia, base string) (*hlsJob, error) {
	j := &hlsJob{t: t, md: md, base: base, dir: hlsDir(base), have: make([]bool, len(md.segs)), tries: make([]int, len(md.segs)), keys: map[string][]byte{}}
	// earlier parts on the disk are kept when the playlist is the same
	dig := hlsDigest(md)
	var st hlsState
	same := false
	if b, e := os.ReadFile(hlsStatePath(base)); e == nil && json.Unmarshal(b, &st) == nil && st.V == 1 && st.Digest == dig && st.N == len(md.segs) {
		same = true
	}
	if !same {
		_ = os.RemoveAll(j.dir)
	}
	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot write to the download folder: %v", err)
	}
	b, _ := json.Marshal(hlsState{V: 1, Digest: dig, N: len(md.segs)})
	_ = dlWriteFileAtomic(hlsStatePath(base), b)
	for i := range md.segs {
		if fi, e := os.Stat(hlsSegPath(j.dir, i)); e == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			j.have[i] = true
			j.nHave++
			j.baseSz += fi.Size()
		}
	}
	// remove half-written leftovers
	if ents, e := os.ReadDir(j.dir); e == nil {
		for _, en := range ents {
			if strings.HasSuffix(en.Name(), ".tmp") {
				_ = os.Remove(j.dir + string(os.PathSeparator) + en.Name())
			}
		}
	}
	atomic.StoreInt64(&j.bytes, j.baseSz)
	t.mu.Lock()
	t.hls = j
	t.mu.Unlock()
	if base != t.spec.Path {
		t.setNote(fmt.Sprintf("Sound track in %d parts, downloaded over all connections", len(md.segs)))
	} else if t.hlsPrev > 0 {
		t.setNote(fmt.Sprintf("Video in %d parts, downloaded over all connections (the sound track is done)", len(md.segs)))
	} else {
		t.setNote(fmt.Sprintf("Video stream in %d parts, downloaded over all connections", len(md.segs)))
	}

	if md.mapURI != "" {
		if err := j.fetchInit(); err != nil {
			return nil, err
		}
	}
	if j.nHave < len(md.segs) {
		j.supervise()
	}
	t.mu.Lock()
	fatal := t.fatal
	t.mu.Unlock()
	if fatal != nil {
		return nil, fatal
	}
	if t.ctx.Err() != nil {
		return nil, t.ctx.Err()
	}
	if j.nHave < len(md.segs) {
		return nil, errors.New("download stopped before the video was complete")
	}
	return j, nil
}

func (j *hlsJob) fetchInit() error {
	p := hlsSegPath(j.dir, -1)
	if fi, e := os.Stat(p); e == nil && fi.Size() > 0 {
		return nil
	}
	var last error
	for try := 0; try < 4; try++ {
		ls := j.t.usableLines()
		if len(ls) == 0 {
			return errors.New("no connection is available right now")
		}
		l := ls[try%len(ls)]
		c := dlLineClient(l, j.t.private)
		ctx, cancel := context.WithTimeout(j.t.ctx, 60*time.Second)
		b, err := hlsGet(ctx, c, j.md.mapURI, j.t.hdr, j.md.mapOff, j.md.mapLen, 64<<20)
		cancel()
		c.CloseIdleConnections()
		if err == nil {
			return dlWriteFileAtomic(p, b)
		}
		if j.t.ctx.Err() != nil {
			return j.t.ctx.Err()
		}
		last = err
	}
	return last
}

// supervise starts workers for every usable line and keeps adding them for lines that appear later.
func (j *hlsJob) supervise() {
	t := j.t
	var wg sync.WaitGroup
	started := map[string]bool{}
	noLine := time.Time{}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		j.mu.Lock()
		all := j.nHave >= len(j.md.segs)
		j.mu.Unlock()
		if all || t.ctx.Err() != nil {
			break
		}
		lines := t.usableLines()
		if len(lines) == 0 {
			if noLine.IsZero() {
				noLine = time.Now()
			} else if time.Since(noLine) > dlNoLineGrace {
				t.fail(errors.New("no connection is available right now"))
				break
			}
		} else {
			noLine = time.Time{}
		}
		for _, l := range lines {
			if started[l.Name] {
				continue
			}
			started[l.Name] = true
			n := t.spec.MaxPerLine
			if n > 6 {
				n = 6
			}
			if n > 4 {
				n = 4
			}
			if n < 1 {
				n = 1
			}
			r := &dlRun{line: l, client: dlLineClient(l, t.private), max: n, target: n, ctx: t.ctx}
			t.mu.Lock()
			t.runs[l.Name] = r
			t.order = append(t.order, l.Name)
			r.active = n
			t.mu.Unlock()
			goSafe("speed", func() { t.speedLoop(r) })
			for k := 0; k < n; k++ {
				wg.Add(1)
				goSafe("segment", func() { defer wg.Done(); j.worker(r) })
			}
		}
		select {
		case <-t.ctx.Done():
		case <-tick.C:
		}
	}
	wg.Wait()
}

// take gives the number of a part to fetch: -1 = nothing is left, -2 = wait (parts are being fetched by others).
func (j *hlsJob) take() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	if n := len(j.retry); n > 0 {
		i := j.retry[n-1]
		j.retry = j.retry[:n-1]
		return i
	}
	for j.next < len(j.md.segs) {
		i := j.next
		j.next++
		if !j.have[i] {
			return i
		}
	}
	if j.nHave >= len(j.md.segs) {
		return -1
	}
	return -2
}

func (j *hlsJob) worker(r *dlRun) {
	t := j.t
	defer func() {
		t.mu.Lock()
		if r.active > 0 {
			r.active--
		}
		t.mu.Unlock()
	}()
	fails := 0
	for t.ctx.Err() == nil {
		if !t.allowed(r.line.Name) || (r.line.Alive != nil && !r.line.Alive()) || !j.lineKnown(r.line.Name) {
			select {
			case <-t.ctx.Done():
				return
			case <-time.After(time.Second):
			}
			if !j.lineKnown(r.line.Name) || j.finished() {
				return
			}
			continue
		}
		i := j.take()
		switch i {
		case -1:
			return
		case -2:
			select {
			case <-t.ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
			continue
		}
		err := j.fetchSeg(r, i)
		if err == nil {
			fails = 0
			continue
		}
		if t.ctx.Err() != nil {
			return
		}
		fails++
		j.mu.Lock()
		j.tries[i]++
		giveUp := j.tries[i] >= hlsMaxTries
		if !giveUp {
			j.retry = append(j.retry, i)
		}
		j.mu.Unlock()
		if giveUp {
			t.fail(err)
			return
		}
		// back off so another line gets a chance to take the part
		d := time.Duration(minInt(fails, 5)) * 700 * time.Millisecond
		var he dlHTTPErr
		if errors.As(err, &he) && he.Retry > 0 && he.Retry < 30*time.Second {
			d = he.Retry
		}
		select {
		case <-t.ctx.Done():
			return
		case <-time.After(d):
		}
	}
}

func (j *hlsJob) finished() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.nHave >= len(j.md.segs)
}

func (j *hlsJob) lineKnown(name string) bool {
	for _, l := range j.t.env.Lines() {
		if l.Name == name {
			return true
		}
	}
	return false
}

func (j *hlsJob) key(r *dlRun, uri string) ([]byte, error) {
	j.keyMu.Lock()
	defer j.keyMu.Unlock()
	if k, ok := j.keys[uri]; ok {
		return k, nil
	}
	ctx, cancel := context.WithTimeout(j.t.ctx, 30*time.Second)
	defer cancel()
	b, err := hlsGet(ctx, r.client, uri, j.t.hdr, 0, 0, 4096)
	if err != nil {
		return nil, err
	}
	if len(b) != 16 {
		return nil, dlStreamErr("This stream is copy-protected. NetMerge cannot download it.")
	}
	j.keys[uri] = b
	return b, nil
}

func (j *hlsJob) fetchSeg(r *dlRun, i int) error {
	t := j.t
	s := j.md.segs[i]
	rctx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	var stalled int32
	wd := time.AfterFunc(dlStall, func() { atomic.StoreInt32(&stalled, 1); cancel() })
	defer wd.Stop()
	req, err := dlRequest(rctx, s.uri, t.hdr)
	if err != nil {
		return err
	}
	want := int64(-1)
	if s.len > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", s.off, s.off+s.len-1))
		want = s.len
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return dlHTTPErr{Code: resp.StatusCode, Retry: dlRetryAfter(resp)}
	}
	skip := int64(0)
	if s.len > 0 && resp.StatusCode == 200 { // range ignored: take the whole file and cut
		skip = s.off
	}
	var buf bytes.Buffer
	chunk := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(chunk)
		if n > 0 {
			wd.Reset(dlStall)
			if err := t.limit(rctx, r, n); err != nil {
				return err
			}
			buf.Write(chunk[:n])
			atomic.AddInt64(&r.bytes, int64(n))
			if buf.Len() > hlsMaxSegBytes {
				return dlStreamErr("A part of the stream is unusually large.")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if atomic.LoadInt32(&stalled) == 1 {
				return errors.New("the connection stopped sending data")
			}
			return rerr
		}
	}
	data := buf.Bytes()
	if skip > 0 {
		if skip > int64(len(data)) {
			return io.ErrUnexpectedEOF
		}
		data = data[skip:]
		if int64(len(data)) > want {
			data = data[:want]
		}
	}
	if want > 0 && int64(len(data)) != want {
		return io.ErrUnexpectedEOF
	}
	if resp.ContentLength >= 0 && want < 0 && int64(buf.Len()) != resp.ContentLength {
		return io.ErrUnexpectedEOF
	}
	if s.keyURI != "" {
		k, err := j.key(r, s.keyURI)
		if err != nil {
			return err
		}
		data, err = hlsDecrypt(data, k, s.iv, s.seq)
		if err != nil {
			return err
		}
	}
	if len(data) == 0 {
		return io.ErrUnexpectedEOF
	}
	p := hlsSegPath(j.dir, i)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.fail(fmt.Errorf("could not write to disk: %v", err))
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		t.fail(fmt.Errorf("could not write to disk: %v", err))
		return err
	}
	j.mu.Lock()
	if !j.have[i] {
		j.have[i] = true
		j.nHave++
		atomic.AddInt64(&j.bytes, int64(len(data)))
		j.estSeen++
	}
	j.mu.Unlock()
	return nil
}

func hlsDecrypt(data, key, iv []byte, seq int) ([]byte, error) {
	if iv == nil {
		iv = make([]byte, 16)
		for k := 0; k < 8; k++ {
			iv[15-k] = byte(uint64(seq) >> (8 * uint(k)))
		}
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, errors.New("a part of the stream is damaged")
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(blk, iv).CryptBlocks(out, data)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return nil, errors.New("a part of the stream is damaged")
	}
	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return nil, errors.New("a part of the stream is damaged")
		}
	}
	return out[:len(out)-pad], nil
}

// join writes the parts, in order, into the partial file and finishes it.
func (j *hlsJob) join() error {
	t := j.t
	part := dlPartPath(t.spec.Path)
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("cannot write to the download folder: %v", err)
	}
	t.file = f
	var total int64
	copyIn := func(p string) error {
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		n, err := io.Copy(f, in)
		total += n
		return err
	}
	if j.md.mapURI != "" {
		if err := copyIn(hlsSegPath(j.dir, -1)); err != nil {
			return fmt.Errorf("could not join the video: %v", err)
		}
	}
	for i := range j.md.segs {
		if err := copyIn(hlsSegPath(j.dir, i)); err != nil {
			return fmt.Errorf("could not join the video: %v", err)
		}
	}
	t.info.Size = total
	if err := t.finish(); err != nil {
		return err
	}
	_ = os.RemoveAll(j.dir)
	_ = os.Remove(hlsStatePath(j.base))
	return nil
}

// joinFile writes the parts of a track into its own file (the sound track of a stream) and clears the parts.
func (j *hlsJob) joinFile(dst string) (int64, error) {
	part := dst + ".part"
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("cannot write to the download folder: %v", err)
	}
	var total int64
	copyIn := func(p string) error {
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		n, err := io.Copy(f, in)
		total += n
		return err
	}
	var jerr error
	if j.md.mapURI != "" {
		jerr = copyIn(hlsSegPath(j.dir, -1))
	}
	for i := 0; jerr == nil && i < len(j.md.segs); i++ {
		jerr = copyIn(hlsSegPath(j.dir, i))
	}
	if jerr == nil {
		jerr = f.Sync()
	}
	f.Close()
	if jerr != nil {
		return 0, fmt.Errorf("could not join the sound track: %v", jerr)
	}
	if err := os.Rename(part, dst); err != nil {
		return 0, fmt.Errorf("could not finish the sound track: %v", err)
	}
	_ = os.RemoveAll(j.dir)
	_ = os.Remove(hlsStatePath(j.base))
	return total, nil
}

// progress of a stream download: done bytes and an estimate of the total from the parts finished so far.
func (j *hlsJob) progress() (done, total int64) {
	j.mu.Lock()
	nHave, n := j.nHave, len(j.md.segs)
	j.mu.Unlock()
	done = atomic.LoadInt64(&j.bytes)
	if nHave > 0 {
		total = done * int64(n) / int64(nHave)
		if total < done {
			total = done
		}
	}
	return
}
