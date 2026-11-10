#!/usr/bin/env python3
"""Test fixtures for the NetMerge download manager (Python 3.8+, standard library only).

    python fixtures.py [--host 127.0.0.1] [--port 8765] [--out fixtures_out] [--ffmpeg]

Content is deterministic (byte i = i % 251) so a download can be checked against GET /sha256/<name>.
<name> is a size such as 1KB, 5MB, 200MB (any number + KB/MB/GB), optionally followed by ".bin".

  /file/<name>            ordinary file, supports Range, ETag, Content-Disposition
  /norange/<name>         ignores Range, chunked, no Content-Length
  /flaky/<name>?drop=N    the FIRST request for that name is cut after N bytes; later ones are normal
  /status503/<name>       503 twice, then normal            (reset with /reset)
  /status403/<name>       403 first, then normal
  /redirect/<name>        302 -> 302 -> 302 -> /file/<name>
  /changing/<name>        a different ETag and different content from the 2nd request on
  /slow/<name>?bps=N      throttled (default 200000 bytes/s), supports Range
  /auth/<name>            Basic auth, user "user", password "pass"
  /disp?name=<text>       tiny file whose Content-Disposition filename is <text> (try ../x, CON, long names, unicode)
  /nolength/<name>        HTTP/1.0 style: no Content-Length, connection close
  /page                   HTML page with .zip, .pdf, .torrent, magnet and page links (link grabber)
  /sha256/<name>          expected SHA-256 of /file/<name>
  /reset                  clear the per-name counters
  /static/...             files in --out (HLS/DASH made with --ffmpeg, see below)

With --ffmpeg (needs ffmpeg on PATH) the --out folder gets:
  hls/master.m3u8        3 variants 640x360, 1280x720, 1920x1080 (all tiny, 6 s)
  hls/aes/index.m3u8     AES-128 encrypted
  hls/fmp4/index.m3u8    fMP4 segments
  hls/live/index.m3u8    playlist without #EXT-X-ENDLIST
  hls/sep/master.m3u8    separate audio rendition
  dash/video_audio.mpd   video 360p + 1080p and audio (separate files, SegmentBase)
  dash/audio_only.mpd    audio only
"""
import argparse, hashlib, http.server, os, re, shutil, socketserver, subprocess, sys, threading, time, urllib.parse

COUNT, LOCK = {}, threading.Lock()
UNITS = {"KB": 1024, "MB": 1024 ** 2, "GB": 1024 ** 3, "B": 1}

def size_of(name):
    m = re.match(r"(\d+)\s*(KB|MB|GB|B)?(\.bin)?$", name, re.I)
    if not m: return None
    return int(m.group(1)) * UNITS[(m.group(2) or "B").upper()]

def byte_block(off, n, salt=0):
    base = bytes((i + salt) % 251 for i in range(251))
    start = off % 251
    rep = (base[start:] + base * ((n // 251) + 2))
    return rep[:n]

def sha_of(size):
    h, off = hashlib.sha256(), 0
    while off < size:
        n = min(1 << 20, size - off); h.update(byte_block(off, n)); off += n
    return h.hexdigest()

def hit(key):
    with LOCK:
        COUNT[key] = COUNT.get(key, 0) + 1
        return COUNT[key]

class H(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a): pass
    def send_body(self, size, salt=0, bps=0, ranged=True, drop=None, etag=None, name="file.bin", extra=None, length=True):
        start, end, code = 0, size - 1, 200
        rng = self.headers.get("Range")
        if ranged and rng:
            m = re.match(r"bytes=(\d*)-(\d*)$", rng.strip())
            if m:
                a, b = m.groups()
                if a == "" and b: start = max(0, size - int(b))
                else:
                    start = int(a or 0); end = int(b) if b else size - 1
                end = min(end, size - 1)
                if start > end or start >= size:
                    self.send_response(416); self.send_header("Content-Range", "bytes */%d" % size); self.send_header("Content-Length", "0"); self.end_headers(); return
                code = 206
        n = end - start + 1
        self.send_response(code)
        self.send_header("Content-Type", "application/octet-stream")
        if ranged: self.send_header("Accept-Ranges", "bytes")
        if code == 206: self.send_header("Content-Range", "bytes %d-%d/%d" % (start, end, size))
        self.send_header("ETag", etag or '"%s-%d"' % (name, size))
        self.send_header("Content-Disposition", 'attachment; filename="%s"' % name)
        for k, v in (extra or {}).items(): self.send_header(k, v)
        chunked = not length and self.request_version == "HTTP/1.1"
        if length: self.send_header("Content-Length", str(n))
        elif chunked: self.send_header("Transfer-Encoding", "chunked")
        else: self.send_header("Connection", "close"); self.close_connection = True
        self.end_headers()
        sent, off = 0, start
        try:
            while sent < n:
                c = min(65536, n - sent)
                if drop is not None and sent + c > drop:
                    self.wfile.write(byte_block(off, drop - sent, salt)); self.wfile.flush()
                    self.close_connection = True; self.connection.close(); return
                blk = byte_block(off, c, salt)
                if chunked: self.wfile.write(b"%x\r\n" % len(blk) + blk + b"\r\n")
                else: self.wfile.write(blk)
                sent += c; off += c
                if bps: time.sleep(c / bps)
            if chunked: self.wfile.write(b"0\r\n\r\n")
        except Exception:
            self.close_connection = True

    def text(self, body, ctype="text/plain", code=200, extra=None):
        b = body.encode() if isinstance(body, str) else body
        self.send_response(code); self.send_header("Content-Type", ctype); self.send_header("Content-Length", str(len(b)))
        for k, v in (extra or {}).items(): self.send_header(k, v)
        self.end_headers(); self.wfile.write(b)

    def do_HEAD(self): self.do_GET()
    def do_GET(self):
        u = urllib.parse.urlparse(self.path); q = urllib.parse.parse_qs(u.query)
        parts = u.path.strip("/").split("/", 1); kind = parts[0]; name = urllib.parse.unquote(parts[1]) if len(parts) > 1 else ""
        sz = size_of(name) if name else None
        if kind == "reset":
            with LOCK: COUNT.clear()
            return self.text("ok")
        if kind == "page":
            base = "http://%s" % self.headers.get("Host")
            return self.text('<html><body><a href="/file/1MB.zip">zip</a><a href="/file/2MB.pdf">pdf</a><a href="/t/demo.torrent">torrent</a>'
                '<a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&amp;dn=Cool+Film">magnet</a><a href="/about">about</a></body></html>', "text/html")
        if kind == "t" and name == "demo.torrent":
            p = os.path.join(ARGS.out, "demo.torrent")
            if os.path.exists(p): return self.text(open(p, "rb").read(), "application/x-bittorrent")
            return self.text("no torrent (run torrent_fixture.py first)", code=404)
        if kind == "static":
            p = os.path.normpath(os.path.join(ARGS.out, urllib.parse.unquote(u.path[len("/static/"):])))
            if not p.startswith(os.path.abspath(ARGS.out)) or not os.path.isfile(p): return self.text("not found", code=404)
            ct = {"m3u8": "application/vnd.apple.mpegurl", "mpd": "application/dash+xml", "ts": "video/mp2t", "mp4": "video/mp4", "m4s": "video/iso.segment"}.get(p.rsplit(".", 1)[-1], "application/octet-stream")
            data = open(p, "rb").read()
            return self.text(data, ct)
        if kind == "disp":
            nm = q.get("name", ["x.bin"])[0]
            return self.send_body(1024, name=nm.encode("utf-8", "replace").decode("latin-1", "replace") if False else nm)
        if sz is None: return self.text("unknown fixture", code=404)
        fn = name if "." in name else name + ".bin"
        if kind == "sha256": return self.text(sha_of(sz))
        if kind == "file": return self.send_body(sz, name=fn)
        if kind == "norange": return self.send_body(sz, ranged=False, length=False, name=fn)
        if kind == "nolength": return self.send_body(sz, ranged=False, length=False, name=fn)
        if kind == "slow": return self.send_body(sz, bps=int(q.get("bps", ["200000"])[0]), name=fn)
        if kind == "flaky":
            n = hit("flaky" + name); drop = int(q.get("drop", [str(sz // 2)])[0])
            return self.send_body(sz, drop=drop if n == 1 else None, name=fn)
        if kind == "status503":
            if hit("503" + name) <= 2: return self.text("busy", code=503, extra={"Retry-After": "1"})
            return self.send_body(sz, name=fn)
        if kind == "status403":
            if hit("403" + name) <= 1: return self.text("denied", code=403)
            return self.send_body(sz, name=fn)
        if kind == "redirect":
            step = int(q.get("s", ["0"])[0])
            loc = "/redirect/%s?s=%d" % (name, step + 1) if step < 3 else "/file/%s" % name
            return self.text("", code=302, extra={"Location": loc})
        if kind == "changing":
            n = hit("chg" + name)
            return self.send_body(sz, salt=0 if n == 1 else 7, etag='"v%d"' % (1 if n == 1 else 2), name=fn)
        if kind == "auth":
            import base64
            if self.headers.get("Authorization") != "Basic " + base64.b64encode(b"user:pass").decode():
                return self.text("login", code=401, extra={"WWW-Authenticate": 'Basic realm="t"'})
            return self.send_body(sz, name=fn)
        return self.text("unknown fixture", code=404)

class S(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True

def sh(*a, cwd=None):
    r = subprocess.run(a, cwd=cwd, capture_output=True, text=True)
    if r.returncode: sys.exit("command failed: %s\n%s" % (" ".join(a), r.stderr[-800:]))

def make_media(out):
    ff = shutil.which("ffmpeg")
    if not ff: sys.exit("ffmpeg not found on PATH")
    src = ["-f", "lavfi", "-i", "testsrc=size=%s:rate=25:duration=6", "-f", "lavfi", "-i", "sine=frequency=440:duration=6"]
    def gen(sub, size, *extra, name="index.m3u8"):
        d = os.path.join(out, sub); os.makedirs(d, exist_ok=True)
        s = src[:]; s[3] = s[3] % size
        sh(ff, "-y", "-v", "error", *s, "-c:v", "libx264", "-preset", "ultrafast", "-g", "25", "-c:a", "aac", "-shortest", "-f", "hls", "-hls_time", "2", *extra, name, cwd=d)
    hls = os.path.join(out, "hls")
    for h, w in ((360, 640), (720, 1280), (1080, 1920)):
        gen("hls/v%d" % h, "%dx%d" % (w, h), "-hls_playlist_type", "vod")
    open(os.path.join(hls, "master.m3u8"), "w").write("#EXTM3U\n" + "".join(
        "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\nv%d/index.m3u8\n" % (bw, w, h, h) for h, w, bw in ((360, 640, 800000), (720, 1280, 2500000), (1080, 1920, 6000000))))
    key = os.path.join(hls, "aes"); os.makedirs(key, exist_ok=True)
    open(os.path.join(key, "enc.key"), "wb").write(b"0123456789abcdef")
    open(os.path.join(key, "keyinfo"), "w").write("enc.key\nenc.key\n00000000000000000000000000000001\n")
    gen("hls/aes", "320x240", "-hls_playlist_type", "vod", "-hls_key_info_file", "keyinfo")
    gen("hls/fmp4", "320x240", "-hls_playlist_type", "vod", "-hls_segment_type", "fmp4")
    gen("hls/live", "320x240", "-hls_list_size", "0")
    live = os.path.join(hls, "live", "index.m3u8")
    open(live, "w").write(open(live).read().replace("#EXT-X-ENDLIST\n", ""))
    sep = os.path.join(hls, "sep"); os.makedirs(sep, exist_ok=True)
    sh(ff, "-y", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=6", "-c:v", "libx264", "-preset", "ultrafast", "-g", "25", "-an", "-f", "hls", "-hls_time", "2", "-hls_playlist_type", "vod", "video.m3u8", cwd=sep)
    sh(ff, "-y", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-c:a", "aac", "-f", "hls", "-hls_time", "2", "-hls_playlist_type", "vod", "audio.m3u8", cwd=sep)
    open(os.path.join(sep, "master.m3u8"), "w").write('#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="a",NAME="en",DEFAULT=YES,URI="audio.m3u8"\n#EXT-X-STREAM-INF:BANDWIDTH=900000,RESOLUTION=320x240,AUDIO="a"\nvideo.m3u8\n')
    dash = os.path.join(out, "dash"); os.makedirs(dash, exist_ok=True)
    for h, w in ((360, 640), (1080, 1920)):
        sh(ff, "-y", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=%dx%d:rate=25:duration=6" % (w, h), "-c:v", "libx264", "-preset", "ultrafast", "-an", "-movflags", "+faststart", "v%d.mp4" % h, cwd=dash)
    sh(ff, "-y", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-c:a", "aac", "-movflags", "+faststart", "a.m4a", cwd=dash)
    def mpd(reps, name):
        open(os.path.join(dash, name), "w").write('<?xml version="1.0"?>\n<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT6S" minBufferTime="PT2S"><Period>' + reps + '</Period></MPD>')
    vid = '<AdaptationSet contentType="video" mimeType="video/mp4">%s</AdaptationSet>' % "".join(
        '<Representation id="v%d" bandwidth="%d" width="%d" height="%d" codecs="avc1.64001f"><BaseURL>v%d.mp4</BaseURL><SegmentBase indexRange="0-0"/></Representation>' % (h, bw, w, h, h) for h, w, bw in ((360, 640, 500000), (1080, 1920, 4000000)))
    aud = '<AdaptationSet contentType="audio" mimeType="audio/mp4"><Representation id="a" bandwidth="128000" codecs="mp4a.40.2"><BaseURL>a.m4a</BaseURL><SegmentBase indexRange="0-0"/></Representation></AdaptationSet>'
    mpd(vid + aud, "video_audio.mpd"); mpd(aud, "audio_only.mpd")
    print("media written to", out)

if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default="127.0.0.1"); ap.add_argument("--port", type=int, default=8765)
    ap.add_argument("--out", default="fixtures_out"); ap.add_argument("--ffmpeg", action="store_true", help="generate HLS/DASH media with ffmpeg, then serve")
    ARGS = ap.parse_args(); os.makedirs(ARGS.out, exist_ok=True); ARGS.out = os.path.abspath(ARGS.out)
    if ARGS.ffmpeg: make_media(ARGS.out)
    print("serving on http://%s:%d  (static files from %s)" % (ARGS.host, ARGS.port, ARGS.out)); S((ARGS.host, ARGS.port), H).serve_forever()
