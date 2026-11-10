package dm

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// a small site with a DASH description, an HLS master with a separate sound track, and the parts of both
func streamSite(t *testing.T) (*httptest.Server, map[string][]byte) {
	files := map[string][]byte{}
	part := func(name string, n int, seed byte) {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i) ^ seed
		}
		files[name] = b
	}
	part("/v/init.mp4", 400, 1)
	for i := 0; i < 4; i++ {
		part(fmt.Sprintf("/v/vid-%d.m4s", i), 3000+i, byte(10+i))
	}
	part("/v/ainit.mp4", 200, 2)
	for i := 0; i < 3; i++ {
		part(fmt.Sprintf("/v/aud-%d.m4s", i), 1000+i, byte(30+i))
	}
	part("/v/whole.mp4", 7000, 9)
	for i := 0; i < 3; i++ {
		part(fmt.Sprintf("/h/v%d.ts", i), 2000+i, byte(50+i))
		part(fmt.Sprintf("/h/a%d.aac", i), 500+i, byte(70+i))
	}
	mux := http.NewServeMux()
	text := func(path, ct, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.Write([]byte(body))
		})
	}
	text("/v/timeline.mpd", "application/dash+xml", `<?xml version="1.0"?>
<MPD type="static" mediaPresentationDuration="PT8S"><Period>
 <AdaptationSet contentType="video" mimeType="video/mp4">
  <SegmentTemplate timescale="1000" initialization="init.mp4" media="vid-$Number$.m4s" startNumber="0"><SegmentTimeline><S t="0" d="2000" r="3"/></SegmentTimeline></SegmentTemplate>
  <Representation id="lo" bandwidth="100000" width="320" height="180"/>
  <Representation id="hi" bandwidth="900000" width="1280" height="720"/>
 </AdaptationSet>
 <AdaptationSet contentType="audio" mimeType="audio/mp4" lang="en">
  <SegmentTemplate timescale="1000" duration="3000" initialization="ainit.mp4" media="aud-$Number%01d$.m4s" startNumber="0"/>
  <Representation id="a1" bandwidth="64000"/>
 </AdaptationSet></Period></MPD>`)
	text("/v/whole.mpd", "application/dash+xml", `<MPD type="static" mediaPresentationDuration="PT5S"><Period><AdaptationSet mimeType="video/mp4"><Representation id="v" bandwidth="1" width="10" height="10"><BaseURL>whole.mp4</BaseURL></Representation></AdaptationSet></Period></MPD>`)
	text("/v/drm.mpd", "application/dash+xml", `<MPD type="static" mediaPresentationDuration="PT5S"><Period><AdaptationSet mimeType="video/mp4"><ContentProtection schemeIdUri="urn:uuid:x"/><SegmentTemplate duration="1" media="a.m4s"/><Representation id="v" bandwidth="1"/></AdaptationSet></Period></MPD>`)
	text("/v/live.mpd", "application/dash+xml", `<MPD type="dynamic"><Period/></MPD>`)
	text("/v/two.mpd", "application/dash+xml", `<MPD type="static"><Period/><Period/></MPD>`)
	text("/h/master.m3u8", "application/vnd.apple.mpegurl", "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aud\",NAME=\"en\",DEFAULT=YES,URI=\"audio.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=1280x720,AUDIO=\"aud\"\nvideo.m3u8\n")
	text("/h/video.m3u8", "application/x-mpegURL", "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\nv0.ts\n#EXTINF:2,\nv1.ts\n#EXTINF:2,\nv2.ts\n#EXT-X-ENDLIST\n")
	text("/h/audio.m3u8", "application/x-mpegURL", "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\na0.aac\n#EXTINF:2,\na1.aac\n#EXTINF:2,\na2.aac\n#EXT-X-ENDLIST\n")
	for p, b := range files {
		b := b
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(b)
		})
	}
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s, files
}

func cat(files map[string][]byte, names ...string) []byte {
	var out []byte
	for _, n := range names {
		out = append(out, files[n]...)
	}
	return out
}

// a stand-in for ffmpeg: it writes video + sound into the last argument
func fakeFFmpeg(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in ffmpeg is a /bin/sh script; the real-ffmpeg tests cover Windows")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nv=''; a=''; out=''\nwhile [ $# -gt 0 ]; do case \"$1\" in -i) if [ -z \"$v\" ]; then v=\"$2\"; else a=\"$2\"; fi; shift;; esac; out=\"$1\"; shift; done\ncat \"$v\" \"$a\" > \"$out\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func noFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	old := H.Root
	H.Root = t.TempDir()
	t.Cleanup(func() { H.Root = old })
}

func TestDASHTimelineWithSoundJoinedByFFmpeg(t *testing.T) {
	fakeFFmpeg(t)
	s, f := streamSite(t)
	env, _, _ := twoLines()
	out := filepath.Join(t.TempDir(), "clip.mp4")
	tk, err := hlsRun(t, env, s.URL+"/v/timeline.mpd", out)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(tk.FinalPath())
	want := append(cat(f, "/v/init.mp4", "/v/vid-0.m4s", "/v/vid-1.m4s", "/v/vid-2.m4s", "/v/vid-3.m4s"), cat(f, "/v/ainit.mp4", "/v/aud-0.m4s", "/v/aud-1.m4s", "/v/aud-2.m4s")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("joined file differs (%d vs %d bytes)", len(got), len(want))
	}
	if left, _ := filepath.Glob(out + "*"); len(left) != 1 {
		t.Fatalf("leftovers: %v", left)
	}
}

func TestDASHWithoutFFmpegKeepsTwoFiles(t *testing.T) {
	noFFmpeg(t)
	s, f := streamSite(t)
	env, _, _ := twoLines()
	out := filepath.Join(t.TempDir(), "clip.mp4")
	tk, err := hlsRun(t, env, s.URL+"/v/timeline.mpd", out)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(tk.FinalPath())
	if !bytes.Equal(got, cat(f, "/v/init.mp4", "/v/vid-0.m4s", "/v/vid-1.m4s", "/v/vid-2.m4s", "/v/vid-3.m4s")) {
		t.Fatal("picture differs")
	}
	au, err := os.ReadFile(out + ".audio.m4a")
	if err != nil || !bytes.Equal(au, cat(f, "/v/ainit.mp4", "/v/aud-0.m4s", "/v/aud-1.m4s", "/v/aud-2.m4s")) {
		t.Fatalf("sound file: %v", err)
	}
	if !strings.Contains(tk.Progress().Note, "ffmpeg") {
		t.Fatalf("note: %q", tk.Progress().Note)
	}
}

func TestDASHSingleFileRepresentation(t *testing.T) {
	s, f := streamSite(t)
	env, _, _ := twoLines()
	tk, err := hlsRun(t, env, s.URL+"/v/whole.mpd", filepath.Join(t.TempDir(), "w.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(tk.FinalPath())
	if !bytes.Equal(got, f["/v/whole.mp4"]) {
		t.Fatal("content")
	}
}

func TestHLSSeparateSoundJoined(t *testing.T) {
	fakeFFmpeg(t)
	s, f := streamSite(t)
	env, _, _ := twoLines()
	tk, err := hlsRun(t, env, s.URL+"/h/master.m3u8", filepath.Join(t.TempDir(), "h.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tk.FinalPath(), ".mp4") {
		t.Fatalf("final: %s", tk.FinalPath())
	}
	got, _ := os.ReadFile(tk.FinalPath())
	want := append(cat(f, "/h/v0.ts", "/h/v1.ts", "/h/v2.ts"), cat(f, "/h/a0.aac", "/h/a1.aac", "/h/a2.aac")...)
	if !bytes.Equal(got, want) {
		t.Fatal("joined content differs")
	}
}

func TestDASHRefusals(t *testing.T) {
	s, _ := streamSite(t)
	for name, want := range map[string]string{"drm": "copy-protected", "live": "live stream", "two": "several periods"} {
		env, _, _ := twoLines()
		_, err := hlsRun(t, env, s.URL+"/v/"+name+".mpd", filepath.Join(t.TempDir(), "x.mp4"))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDASHTemplateExpansion(t *testing.T) {
	if got := mpdExpand("v-$RepresentationID$-$Bandwidth$-$Number%05d$-$Time$-$$.m4s", "x", 99, 7, 123); got != "v-x-99-00007-123-$.m4s" {
		t.Fatal(got)
	}
	if mpdSeconds("PT1H2M3.5S") != 3723.5 || mpdSeconds("P1DT1S") != 86401 {
		t.Fatal("durations")
	}
}

func TestDASHAudioOnlyIsM4A(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/s/a.mpd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dash+xml")
		w.Write([]byte(`<MPD type="static" mediaPresentationDuration="PT4S"><Period><AdaptationSet contentType="audio" mimeType="audio/mp4"><SegmentTemplate timescale="1" duration="2" initialization="i.mp4" media="s$Number$.m4s" startNumber="1"/><Representation id="a" bandwidth="64000"/></AdaptationSet></Period></MPD>`))
	})
	for _, n := range []string{"i.mp4", "s1.m4s", "s2.m4s"} {
		mux.HandleFunc("/s/"+n, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("DATA")) })
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	id := addOne(t, m, srv.URL+"/s/a.mpd")
	v := waitState(t, m, id, "done", 20*time.Second)
	if !strings.HasSuffix(v.Name, ".m4a") || v.Category != "Music" {
		t.Fatalf("%s %s", v.Name, v.Category)
	}
}

func TestRemovingAFinishedStreamAlsoRemovesTheSoundFile(t *testing.T) {
	noFFmpeg(t)
	s, _ := streamSite(t)
	m := newTestMgr(t, newSimEnv(newSimLine("A", "127.0.0.1", 0)))
	id := addOne(t, m, s.URL+"/v/timeline.mpd")
	v := waitState(t, m, id, "done", 30*time.Second)
	if _, err := os.Stat(v.Path + ".audio.m4a"); err != nil {
		t.Fatalf("sound file missing: %v", err)
	}
	if _, err := m.handle(dlReq{Action: "remove", ID: id, DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.Path + ".audio.m4a"); err == nil {
		t.Fatal("sound file left behind")
	}
}
