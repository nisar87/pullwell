#!/usr/bin/env python3
"""Encode the demo video from the frames and timelines that showcase.mjs wrote.
Usage: encode_demo.py <showcase-out-dir> <video.mp4> <loop.gif>
Needs ffmpeg on PATH or the imageio-ffmpeg package (pip install imageio-ffmpeg)."""
import json, os, shutil, subprocess, sys

out, mp4, gif = sys.argv[1:4]
ff = shutil.which('ffmpeg')
if not ff:
    import imageio_ffmpeg
    ff = imageio_ffmpeg.get_ffmpeg_exe()
scenes = json.load(open(os.path.join(out, 'scenes.json')))
pad = {'a-list': '0x0e1013', 'b-progress': '0x0e1013', 'c-light': '0xf2f4f7'}
parts = []
for sc in scenes:
    t = max(1.0, sc['seconds'])
    vf = (f"fps=30,scale=1280:720:force_original_aspect_ratio=decrease:flags=lanczos,"
          f"pad=1280:720:(ow-iw)/2:(oh-ih)/2:color={pad.get(sc['name'], '0x0e1013')},"
          f"fade=t=in:st=0:d=0.35,fade=t=out:st={t - 0.35:.2f}:d=0.35,format=yuv420p")
    part = os.path.abspath(os.path.join(out, f"scene-{sc['name']}.mp4"))
    subprocess.run([ff, '-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', os.path.abspath(os.path.join(out, f"timeline-{sc['name']}.txt")),
                    '-vf', vf, '-t', f'{t:.2f}', '-c:v', 'libx264', '-preset', 'slow', '-crf', '19', '-movflags', '+faststart', os.path.abspath(part)], check=True)
    parts.append(part)
lst = os.path.abspath(os.path.join(out, 'parts.txt'))
open(lst, 'w').write(''.join(f"file '{os.path.basename(p)}'\n" for p in parts))
mp4 = os.path.abspath(mp4); gif = os.path.abspath(gif)
subprocess.run([ff, '-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', lst, '-c', 'copy', '-movflags', '+faststart', mp4], check=True)
subprocess.run([ff, '-y', '-loglevel', 'error', '-i', mp4, '-vf',
                'fps=12,scale=800:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=96:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5',
                '-loop', '0', gif], check=True)
print('video', os.path.getsize(mp4), 'gif', os.path.getsize(gif))
