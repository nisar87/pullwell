package dm

// DASH (.mpd) streams, and joining a separate picture and sound track. The parts of a track are downloaded by the
// same code as HLS parts (over all lines); a DASH track is just a list of parts and an initialisation part.

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type mpdDoc struct {
	Type     string      `xml:"type,attr"`
	Duration string      `xml:"mediaPresentationDuration,attr"`
	BaseURL  []string    `xml:"BaseURL"`
	Periods  []mpdPeriod `xml:"Period"`
}

type mpdPeriod struct {
	Duration string   `xml:"duration,attr"`
	BaseURL  []string `xml:"BaseURL"`
	Sets     []mpdSet `xml:"AdaptationSet"`
}

type mpdSet struct {
	ContentType string       `xml:"contentType,attr"`
	MimeType    string       `xml:"mimeType,attr"`
	Lang        string       `xml:"lang,attr"`
	BaseURL     []string     `xml:"BaseURL"`
	Template    *mpdTemplate `xml:"SegmentTemplate"`
	Protection  []struct{}   `xml:"ContentProtection"`
	Reps        []mpdRep     `xml:"Representation"`
}

type mpdRep struct {
	ID         string       `xml:"id,attr"`
	Bandwidth  int64        `xml:"bandwidth,attr"`
	Width      int          `xml:"width,attr"`
	Height     int          `xml:"height,attr"`
	MimeType   string       `xml:"mimeType,attr"`
	BaseURL    []string     `xml:"BaseURL"`
	Template   *mpdTemplate `xml:"SegmentTemplate"`
	List       *mpdList     `xml:"SegmentList"`
	Protection []struct{}   `xml:"ContentProtection"`
}

type mpdTemplate struct {
	Timescale   int64  `xml:"timescale,attr"`
	Duration    int64  `xml:"duration,attr"`
	StartNumber *int64 `xml:"startNumber,attr"`
	Init        string `xml:"initialization,attr"`
	Media       string `xml:"media,attr"`
	Timeline    *struct {
		S []struct {
			T int64 `xml:"t,attr"`
			D int64 `xml:"d,attr"`
			R int64 `xml:"r,attr"`
		} `xml:"S"`
	} `xml:"SegmentTimeline"`
}

type mpdList struct {
	Init *struct {
		URL string `xml:"sourceURL,attr"`
	} `xml:"Initialization"`
	URLs []struct {
		Media string `xml:"media,attr"`
	} `xml:"SegmentURL"`
}

var isoDur = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:([\d.]+)S)?)?$`)

func mpdSeconds(s string) float64 {
	m := isoDur.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	f := func(x string) float64 { v, _ := strconv.ParseFloat(x, 64); return v }
	return f(m[1])*86400 + f(m[2])*3600 + f(m[3])*60 + f(m[4])
}

var tmplVar = regexp.MustCompile(`\$(RepresentationID|Bandwidth|Number|Time)(%0(\d+)d)?\$|\$\$`)

func mpdExpand(t string, id string, bw int64, number, tm int64) string {
	return tmplVar.ReplaceAllStringFunc(t, func(m string) string {
		if m == "$$" {
			return "$"
		}
		sub := tmplVar.FindStringSubmatch(m)
		pad := 1
		if sub[3] != "" {
			pad, _ = strconv.Atoi(sub[3])
		}
		num := func(v int64) string { return fmt.Sprintf("%0*d", pad, v) }
		switch sub[1] {
		case "RepresentationID":
			return id
		case "Bandwidth":
			return num(bw)
		case "Number":
			return num(number)
		}
		return num(tm)
	})
}

func firstBase(l []string) string {
	if len(l) > 0 {
		return strings.TrimSpace(l[0])
	}
	return ""
}

func mpdJoin(base string, parts ...string) (string, error) {
	cur := base
	for _, p := range parts {
		if p == "" {
			continue
		}
		u, err := hlsResolve(cur, p)
		if err != nil {
			return "", err
		}
		cur = u
	}
	return cur, nil
}

// dlDASHInspect reads a .mpd and picks the best picture and sound.
func dlDASHInspect(ctx context.Context, c *http.Client, link string, hdr map[string]string) (*hlsInfo, error) {
	b, err := hlsGet(ctx, c, link, hdr, 0, 0, 8<<20)
	if err != nil {
		return nil, err
	}
	var doc mpdDoc
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, nil // not a DASH file: the normal download goes on
	}
	switch {
	case strings.EqualFold(doc.Type, "dynamic"):
		return nil, dlStreamErr("This is a live stream (it has no end). NetMerge only downloads recorded videos.")
	case len(doc.Periods) == 0:
		return nil, dlStreamErr("The video description has no content.")
	case len(doc.Periods) > 1:
		return nil, dlStreamErr("This stream has several periods (for example ads in between). NetMerge cannot download that yet.")
	}
	per := doc.Periods[0]
	total := mpdSeconds(per.Duration)
	if total == 0 {
		total = mpdSeconds(doc.Duration)
	}
	type pick struct {
		set mpdSet
		rep mpdRep
	}
	var video, audio *pick
	heightSeen, heights := map[int]bool{}, []int(nil)
	for _, set := range per.Sets {
		kind := strings.ToLower(set.ContentType)
		mt := strings.ToLower(set.MimeType)
		for _, r := range set.Reps {
			if r.MimeType != "" {
				mt = strings.ToLower(r.MimeType)
			}
			k := kind
			if k == "" {
				k = strings.SplitN(mt, "/", 2)[0]
			}
			p := &pick{set, r}
			switch k {
			case "video":
				if r.Height > 0 && !heightSeen[r.Height] {
					heightSeen[r.Height] = true
					heights = append(heights, r.Height)
				}
				want := dlWantHeight(ctx)
				fits := func(q *pick) bool { return want == 0 || q.rep.Height == 0 || q.rep.Height <= want }
				switch {
				case video == nil:
					video = p
				case fits(p) && !fits(video):
					video = p
				case fits(p) == fits(video) && fits(p) && (p.rep.Height > video.rep.Height || (p.rep.Height == video.rep.Height && r.Bandwidth > video.rep.Bandwidth)):
					video = p
				case !fits(p) && !fits(video) && (p.rep.Height < video.rep.Height || (p.rep.Height == video.rep.Height && r.Bandwidth > video.rep.Bandwidth)):
					video = p
				}
			case "audio":
				if audio == nil || r.Bandwidth > audio.rep.Bandwidth {
					audio = p
				}
			}
		}
	}
	if video == nil && audio == nil {
		return nil, dlStreamErr("The video description lists no picture or sound that NetMerge can download.")
	}
	track := func(p *pick) (*hlsMedia, error) {
		if len(p.set.Protection) > 0 || len(p.rep.Protection) > 0 {
			return nil, dlStreamErr("This stream is copy-protected. NetMerge cannot download it.")
		}
		base, err := mpdJoin(link, firstBase(doc.BaseURL), firstBase(per.BaseURL), firstBase(p.set.BaseURL), firstBase(p.rep.BaseURL))
		if err != nil {
			return nil, err
		}
		md := &hlsMedia{endList: true, fmp4: true, duration: total}
		tpl := p.rep.Template
		if tpl == nil {
			tpl = p.set.Template
		}
		switch {
		case tpl != nil && tpl.Media != "":
			ts := tpl.Timescale
			if ts <= 0 {
				ts = 1
			}
			start := int64(1)
			if tpl.StartNumber != nil {
				start = *tpl.StartNumber
			}
			if tpl.Init != "" {
				u, err := mpdJoin(base, mpdExpand(tpl.Init, p.rep.ID, p.rep.Bandwidth, 0, 0))
				if err != nil {
					return nil, err
				}
				md.mapURI = u
			}
			add := func(num, tm int64, d float64) error {
				u, err := mpdJoin(base, mpdExpand(tpl.Media, p.rep.ID, p.rep.Bandwidth, num, tm))
				if err != nil {
					return err
				}
				md.segs = append(md.segs, hlsSeg{uri: u, seq: int(num), dur: d})
				if len(md.segs) > hlsMaxSegs {
					return dlStreamErr("The stream has too many parts.")
				}
				return nil
			}
			if tpl.Timeline != nil && len(tpl.Timeline.S) > 0 {
				var tm int64
				num := start
				for i, s := range tpl.Timeline.S {
					if s.T != 0 || i == 0 {
						if s.T != 0 {
							tm = s.T
						}
					}
					rep := s.R
					if rep < 0 {
						if total <= 0 || s.D <= 0 {
							return nil, dlStreamErr("The stream's part list cannot be read.")
						}
						rep = int64(math.Ceil(total*float64(ts)/float64(s.D))) - 1 - int64(len(md.segs))
						if rep < 0 {
							rep = 0
						}
					}
					for k := int64(0); k <= rep; k++ {
						if err := add(num, tm, float64(s.D)/float64(ts)); err != nil {
							return nil, err
						}
						tm += s.D
						num++
					}
				}
			} else {
				if tpl.Duration <= 0 || total <= 0 {
					return nil, dlStreamErr("The stream's part list cannot be read.")
				}
				n := int64(math.Ceil(total * float64(ts) / float64(tpl.Duration)))
				for k := int64(0); k < n; k++ {
					if err := add(start+k, k*tpl.Duration, float64(tpl.Duration)/float64(ts)); err != nil {
						return nil, err
					}
				}
			}
		case p.rep.List != nil && len(p.rep.List.URLs) > 0:
			if p.rep.List.Init != nil && p.rep.List.Init.URL != "" {
				u, err := mpdJoin(base, p.rep.List.Init.URL)
				if err != nil {
					return nil, err
				}
				md.mapURI = u
			}
			for i, s := range p.rep.List.URLs {
				u, err := mpdJoin(base, s.Media)
				if err != nil {
					return nil, err
				}
				md.segs = append(md.segs, hlsSeg{uri: u, seq: i})
			}
		default: // one file for the whole track
			if len(p.rep.BaseURL) == 0 && len(p.set.BaseURL) == 0 {
				return nil, dlStreamErr("The stream's part list cannot be read.")
			}
			md.segs = []hlsSeg{{uri: base}}
		}
		if len(md.segs) == 0 {
			return nil, dlStreamErr("The video description has no parts.")
		}
		return md, nil
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))
	out := &hlsInfo{mediaURL: link, Kind: "mp4", Heights: heights}
	first, second := video, audio
	if video == nil {
		first, second = audio, nil
	}
	md, err := track(first)
	if err != nil {
		return nil, err
	}
	out.media = md
	if video == nil {
		out.Kind = "m4a"
	}
	if second != nil {
		am, err := track(second)
		if err != nil {
			return nil, err
		}
		out.audioMed, out.Audio = am, true
	}
	out.Segments, out.Seconds = len(md.segs), total
	if video != nil && video.rep.Height > 0 {
		out.Quality = fmt.Sprintf("%dx%d", video.rep.Width, video.rep.Height)
	} else if first.rep.Bandwidth > 0 {
		out.Quality = fmt.Sprintf("%d kbit/s", first.rep.Bandwidth/1000)
	}
	return out, nil
}

var errNoMPD = errors.New("not a DASH description")

func (t *dlTask) runDASH() (bool, error) {
	var info *hlsInfo
	var err error
	for _, l := range t.usableLines() {
		pctx, c := context.WithTimeout(t.ctx, 40*time.Second)
		pc := dlLineClient(l, t.private)
		t.mu.Lock()
		t.probeClients = append(t.probeClients, pc)
		t.mu.Unlock()
		info, err = dlDASHInspect(context.WithValue(pctx, dlHeightKey{}, t.spec.MaxHeight), pc, t.info.FinalURL, t.hdr)
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
	return true, t.startStream(info, ".mpd")
}

// ---- joining picture and sound -------------------------------------------------------------------------------------------

var dlRunProgram = func(args []string) error {
	if H.Run != nil {
		return H.Run(args)
	}
	return exec.Command(args[0], args[1:]...).Run()
}

// dlFindFFmpeg looks for ffmpeg in the PATH and next to NetMerge. NetMerge does not install it.
func dlFindFFmpeg() string {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	exe := "ffmpeg"
	if runtime.GOOS == "windows" {
		exe = "ffmpeg.exe"
	}
	for _, d := range []string{hRoot(), filepath.Join(hRoot(), "tools"), filepath.Join(hRoot(), "ffmpeg", "bin")} {
		p := filepath.Join(d, exe)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// mux joins the finished picture file with the sound file into one .mp4. Without ffmpeg both files are kept.
func (t *dlTask) mux(audio string) {
	t.mu.Lock()
	video := t.finalPath
	t.mu.Unlock()
	ff := dlFindFFmpeg()
	if ff == "" {
		t.setNote("Picture and sound are two files: " + filepath.Base(video) + " and " + filepath.Base(audio) + ". Install ffmpeg (put it in the PATH or next to NetMerge) and NetMerge joins them into one file.")
		return
	}
	t.setNote("Joining picture and sound")
	tmp := video + ".mux.mp4"
	_ = os.Remove(tmp)
	err := dlRunProgram([]string{ff, "-y", "-v", "error", "-i", video, "-i", audio, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-movflags", "+faststart", tmp})
	if fi, e := os.Stat(tmp); err != nil || e != nil || fi.Size() == 0 {
		_ = os.Remove(tmp)
		t.setNote("Picture and sound could not be joined, so they are two files: " + filepath.Base(video) + " and " + filepath.Base(audio))
		return
	}
	final := strings.TrimSuffix(video, filepath.Ext(video)) + ".mp4"
	if strings.EqualFold(final, video) {
		final = video
	}
	if final != video {
		if _, err := os.Stat(final); err == nil {
			final = dlUniquePath(filepath.Dir(final), filepath.Base(final), func(p string) bool { _, e := os.Stat(p); return e == nil })
		}
	}
	_ = os.Remove(video)
	if err := os.Rename(tmp, final); err != nil {
		t.setNote("Joined file could not be saved: " + err.Error())
		return
	}
	_ = os.Remove(audio)
	t.mu.Lock()
	t.finalPath = final
	t.mu.Unlock()
	t.setNote("")
}
