#!/usr/bin/env python3
"""summary.py reports.jsonl: what stands out in the problem reports, without printing whole logs.

Ignores test installs (install id starting with "test-"). Prints: how many reports/installs/versions, crash reports,
runs that did not end by themselves, recovered crashes, problems per cause (with the number of installs), summed
counts of failure kinds, and the most common warning lines. A line is shown at most once, shortened.
"""
import collections, json, re, sys

rows = [json.loads(l) for l in open(sys.argv[1], encoding="utf-8") if l.strip()]
real = [r for r in rows if not str(r.get("install", "")).startswith("test-")]
print(f"{len(real)} report(s) from {len({r['install'] for r in real})} install(s) ({len(rows) - len(real)} test report(s) ignored)")
print("versions:", dict(collections.Counter(r.get("version") for r in real)))
print("windows:", dict(collections.Counter(r.get("os") for r in real)))

crash = [r for r in real if r.get("kind") == "crash"]
print(f"\nCRASH reports: {len(crash)}")
for r in crash:
    first = (r.get("crash") or "").strip().splitlines()[:6]
    print(f"- install {r['install'][:8]} version {r.get('version')} at {r.get('at')}: " + " | ".join(first)[:400])

pat = {
    "unclean last run": re.compile(r"last run: it did not end by itself"),
    "recovered crash": re.compile(r"recovered from a crash in (.*)"),
    "window script error": re.compile(r"window script error: (.*)"),
    "download problem": re.compile(r"download \S+ \((?:file|torrent)\): problem at \d+%: ([\w()-]+)"),
    "settings refused": re.compile(r"settings refused: (.*)"),
    "wake/freeze": re.compile(r"wake: the program did not run for (\d+) s"),
    "not sent": re.compile(r"diagnostics: not sent: (.*)"),
}
seen = {k: collections.Counter() for k in pat}
who = {k: collections.defaultdict(set) for k in pat}
for r in real:
    for line in (r.get("log") or "").splitlines():
        for k, rx in pat.items():
            m = rx.search(line)
            if m:
                key = (m.group(1) if m.groups() else "-")[:140]
                if k == "wake/freeze": key = "-"
                seen[k][key] += 1
                who[k][key].add(r["install"])
for k in pat:
    if seen[k]:
        print(f"\n{k}:")
        for key, n in seen[k].most_common(12):
            print(f"- {n}x on {len(who[k][key])} install(s): {key}")

sums = collections.Counter()
for r in real:
    for line in (r.get("log") or "").splitlines():
        if "counts over" in line and "added up:" in line:
            for kv in re.findall(r"([a-z][\w.-]*)=(\d+)", line.split("added up:")[1]):
                sums[kv[0]] += int(kv[1])
bad = {k: v for k, v in sums.items() if re.search(r"problem|panic|error|refused|declined|crash|unclean|fail", k)}
print("\ncounts of trouble:", dict(sorted(bad.items(), key=lambda x: -x[1])[:25]) or "none")
print("all counts:", dict(sorted(sums.items(), key=lambda x: -x[1])[:25]))
