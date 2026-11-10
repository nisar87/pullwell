#!/usr/bin/env python3
"""Torrent fixture for the NetMerge download manager: makes a torrent, and runs an HTTP tracker and a seeder (stdlib only).

    python torrent_fixture.py --host 192.168.1.20 [--out fixtures_out] [--private] [--corrupt-piece N] [--upload-test]

IMPORTANT: use this PC's LAN address for --host (not 127.0.0.1). NetMerge refuses peers and trackers on 127.0.0.1 in
normal builds, and refuses LAN addresses too for torrents that a web page (the extension) added; add with the app's own page.

Writes <out>/demo.torrent (and <out>/demo-magnet.txt), serves the tracker on port 6969 and the seeder on port 6881.
Content: Demo/one.bin 300000 bytes, Demo/sub/two.bin 300000 bytes, Demo/three.bin 2000000 bytes, byte i = (i*7+file) % 251,
piece length 65536. Expected hashes are written to <out>/demo-expected.json.
  --private          sets the private flag
  --corrupt-piece N  the seeder sends garbage for piece N (the client must reject it; with a second good seeder it still completes)
  --upload-test      the seeder also asks NetMerge for pieces NetMerge already has (NetMerge has no listening port, so this uses the same
                     connection it opened to us). Prints CORRECT/WRONG when NetMerge uploads, or "choked"/nothing when it does not (T14, T15)
  --throttle BPS     limit the seeder's total send speed
Stop with Ctrl+C. The log shows every request, so you can read the tracker events (started / stopped / completed)."""
import argparse, hashlib, http.server, json, os, socket, socketserver, struct, sys, threading, time, urllib.parse

def be(x):
    if isinstance(x, int): return b"i%de" % x
    if isinstance(x, bytes): return b"%d:%s" % (len(x), x)
    if isinstance(x, str): return be(x.encode())
    if isinstance(x, list): return b"l" + b"".join(be(i) for i in x) + b"e"
    return b"d" + b"".join(be(k) + be(v) for k, v in sorted(x.items())) + b"e"

FILES = [("one.bin", 300000), ("sub/two.bin", 300000), ("three.bin", 2000000)]
PL = 65536
def content():
    return b"".join(bytes((i * 7 + n) % 251 for i in range(sz)) for n, (_, sz) in enumerate(FILES))

def main():
    ap = argparse.ArgumentParser(); ap.add_argument("--host", required=True); ap.add_argument("--out", default="fixtures_out")
    ap.add_argument("--private", action="store_true"); ap.add_argument("--corrupt-piece", type=int, default=-1)
    ap.add_argument("--upload-test", action="store_true"); ap.add_argument("--throttle", type=int, default=0)
    ap.add_argument("--tracker-port", type=int, default=6969); ap.add_argument("--seed-port", type=int, default=6881)
    ap.add_argument("--bad-seed-port", type=int, default=0, help="also run a second seeder on this port that corrupts EVERY piece it sends (T13); the tracker lists both seeders")
    a = ap.parse_args(); os.makedirs(a.out, exist_ok=True)
    data = content(); pieces = [data[i:i + PL] for i in range(0, len(data), PL)]
    info = {"name": "Demo", "piece length": PL, "pieces": b"".join(hashlib.sha1(p).digest() for p in pieces),
            "files": [{"length": sz, "path": n.split("/")} for n, sz in FILES]}
    if a.private: info["private"] = 1
    ann = "http://%s:%d/announce" % (a.host, a.tracker_port)
    open(os.path.join(a.out, "demo.torrent"), "wb").write(be({"announce": ann, "info": info}))
    ih = hashlib.sha1(be(info)).digest()
    open(os.path.join(a.out, "demo-magnet.txt"), "w").write("magnet:?xt=urn:btih:%s&dn=Demo&tr=%s\n" % (ih.hex(), urllib.parse.quote(ann, safe="")))
    off, exp = 0, {}
    for n, sz in FILES:
        exp[n] = hashlib.sha256(data[off:off + sz]).hexdigest(); off += sz
    json.dump({"files": exp, "infohash": ih.hex(), "pieces": len(pieces)}, open(os.path.join(a.out, "demo-expected.json"), "w"), indent=1)
    print("torrent:", os.path.join(a.out, "demo.torrent"), "infohash", ih.hex(), "pieces", len(pieces))

    class T(http.server.BaseHTTPRequestHandler):
        def log_message(self, f, *x): pass
        def do_GET(self):
            q = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
            print("[tracker] announce event=%s left=%s port=%s uploaded=%s" % (q.get("event", ["-"])[0], q.get("left", ["?"])[0], q.get("port", ["?"])[0], q.get("uploaded", ["?"])[0]), flush=True)
            peers = socket.inet_aton(a.host) + struct.pack(">H", a.seed_port)
            if a.bad_seed_port: peers += socket.inet_aton(a.host) + struct.pack(">H", a.bad_seed_port)
            body = be({"interval": 60, "complete": 1, "incomplete": 0, "peers": peers})
            self.send_response(200); self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    class TS(socketserver.ThreadingMixIn, http.server.HTTPServer): daemon_threads = True; allow_reuse_address = True
    threading.Thread(target=TS((a.host, a.tracker_port), T).serve_forever, daemon=True).start()

    def recvn(c, n):
        b = b""
        while len(b) < n:
            x = c.recv(n - len(b))
            if not x: raise EOFError
            b += x
        return b
    def msg(c, i, payload=b""): c.sendall(struct.pack(">IB", 1 + len(payload), i) + payload)
    bitfield = bytearray((len(pieces) + 7) // 8)
    for i in range(len(pieces)): bitfield[i // 8] |= 0x80 >> (i % 8)
    def serve(c, addr, bad=False):
        try:
            hs = recvn(c, 68)
            if hs[28:48] != ih: return
            c.sendall(b"\x13BitTorrent protocol" + b"\0" * 8 + ih + b"-FX0001-" + os.urandom(12))
            msg(c, 5, bytes(bitfield)); print("[seeder%s] peer" % (" BAD" if bad else ""), addr, flush=True)
            theirs, asked = set(), [False]
            def want():
                # upload test: ask NetMerge for a piece it says it has
                if a.upload_test and theirs:
                    if not asked[0]: msg(c, 2); asked[0] = True
            while True:
                n = struct.unpack(">I", recvn(c, 4))[0]
                if n == 0: continue
                b = recvn(c, n); t = b[0]
                if t == 2: msg(c, 1)
                elif t == 5:
                    theirs = {i for i in range(len(pieces)) if b[1 + i // 8] & (0x80 >> (i % 8))}; want()
                elif t == 4:
                    theirs.add(struct.unpack(">I", b[1:5])[0]); want()
                elif t == 1 and theirs:
                    idx = min(theirs); msg(c, 6, struct.pack(">III", idx, 0, 16384)); print("[upload-test] asked NetMerge for piece %d" % idx, flush=True)
                elif t == 0 and a.upload_test: print("[upload-test] NetMerge choked us (no upload)", flush=True)
                elif t == 7 and a.upload_test:
                    idx, begin = struct.unpack(">II", b[1:9]); ok = b[9:] == pieces[idx][begin:begin + len(b) - 9]
                    print("[upload-test] got %d bytes of piece %d from NetMerge: %s" % (len(b) - 9, idx, "CORRECT" if ok else "WRONG"), flush=True)
                elif t == 6:
                    idx, begin, ln = struct.unpack(">III", b[1:13])
                    blk = pieces[idx][begin:begin + ln]
                    if begin == 0: print("[request%s] piece %d" % (" BAD-seeder" if bad else "", idx), flush=True)
                    if bad or idx == a.corrupt_piece: blk = bytes(x ^ 0xFF for x in blk)
                    if a.throttle: time.sleep(len(blk) / a.throttle)
                    msg(c, 7, struct.pack(">II", idx, begin) + blk)
        except Exception: pass
        finally: c.close()
    def listen(port, bad):
        ls = socket.socket(); ls.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); ls.bind((a.host, port)); ls.listen(50)
        def acc():
            while True:
                c, ad = ls.accept(); threading.Thread(target=serve, args=(c, ad, bad), daemon=True).start()
        threading.Thread(target=acc, daemon=True).start()
    listen(a.seed_port, False)
    if a.bad_seed_port: listen(a.bad_seed_port, True)
    print("tracker %s  seeder %s:%d  (Ctrl+C to stop)" % (ann, a.host, a.seed_port), flush=True)
    try:
        while True: time.sleep(3600)
    except KeyboardInterrupt: pass

if __name__ == "__main__": main()
