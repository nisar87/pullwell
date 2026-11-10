#!/usr/bin/env python3
"""Build the extension packages: dist/<Name>-extension-chrome-edge.zip (Chrome, Edge, Brave) and
dist/<Name>-extension-firefox.zip. The product name comes from appname.go and the port list from server.go, so the
extension always matches the program. Also writes the icons (plain PNGs, no libraries needed).
Usage: python3 extension/build.py"""
import json, os, re, struct, zlib, zipfile

here = os.path.dirname(os.path.abspath(__file__))
root = os.path.dirname(here)

def source(name):
    return open(os.path.join(root, name), encoding="utf-8").read()

NAME = re.search(r'const AppName = "([^"]+)"', source("appname.go")).group(1)
PORTS = [int(p) for p in re.search(r"var appPorts = \[\]int\{([0-9, ]+)\}", source("server.go")).group(1).split(",")]
VERSION = source("VERSION").strip()
VARS = {
    "{{NAME}}": NAME,
    "{{SCHEME}}": NAME.lower(),
    "{{PORTS}}": json.dumps(PORTS),
    "{{HOSTS}}": json.dumps([f"http://127.0.0.1:{p}/*" for p in PORTS]),
}

def fill(text):
    for k, v in VARS.items():
        text = text.replace(k, v)
    left = re.findall(r"\{\{[A-Z]+\}\}", text)
    if left:
        raise SystemExit(f"unknown placeholder {left[0]}")
    return text

def png(size):
    # blue rounded square with a white down arrow and a bar
    bg, fg = (37, 99, 235), (255, 255, 255)
    rows = []
    r = size * 0.22
    for y in range(size):
        row = bytearray([0])
        for x in range(size):
            cx, cy = min(x, size - 1 - x), min(y, size - 1 - y)
            inside = True
            if cx < r and cy < r:
                inside = (r - cx - 0.5) ** 2 + (r - cy - 0.5) ** 2 <= r * r
            px = (0, 0, 0, 0)
            if inside:
                px = bg + (255,)
                u, v = x / size, y / size
                shaft = 0.42 <= u <= 0.58 and 0.18 <= v <= 0.52
                head = 0.52 <= v <= 0.72 and abs(u - 0.5) <= (0.72 - v) * 1.1 + 0.0
                bar = 0.24 <= u <= 0.76 and 0.78 <= v <= 0.86
                if shaft or head or bar:
                    px = fg + (255,)
            row += bytes(px)
        rows.append(bytes(row))
    raw = b"".join(rows)
    def chunk(t, d):
        c = struct.pack(">I", len(d)) + t + d
        return c + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b"")

os.makedirs(os.path.join(here, "icons"), exist_ok=True)
for s in (16, 32, 48, 128):
    with open(os.path.join(here, "icons", f"{s}.png"), "wb") as f:
        f.write(png(s))

files = ["core.js", "background.js", "video.js", "popup.html", "popup.js", "popup.css"]
os.makedirs(os.path.join(here, "dist"), exist_ok=True)
for name, manifest in (("chrome-edge", "manifest.chrome.json"), ("firefox", "manifest.firefox.json")):
    m = json.loads(fill(open(os.path.join(here, manifest), encoding="utf-8").read()))
    m["version"] = VERSION
    out = os.path.join(here, "dist", f"{NAME}-extension-{name}.zip")
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("manifest.json", json.dumps(m, indent=2))
        for f in files:
            z.writestr(f, fill(open(os.path.join(here, "src", f), encoding="utf-8").read()))
        for s in (16, 32, 48, 128):
            z.write(os.path.join(here, "icons", f"{s}.png"), f"icons/{s}.png")
    print("wrote", out)
