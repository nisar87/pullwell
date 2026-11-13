#!/usr/bin/env python3
"""Reads the problem reports of the last N days from Firestore (REST) and writes them as JSON lines.

Run by .github/workflows/diagnostics.yml with TOKEN (a short-lived Google access token). Prints counts only: the
reports themselves never go to a log.
"""
import argparse, json, os, sys, urllib.request, datetime

PROJECT = "pullwell-app"
URL = f"https://firestore.googleapis.com/v1/projects/{PROJECT}/databases/(default)/documents:runQuery"


def plain(v):
    if "stringValue" in v: return v["stringValue"]
    if "timestampValue" in v: return v["timestampValue"]
    if "integerValue" in v: return int(v["integerValue"])
    if "booleanValue" in v: return v["booleanValue"]
    return None


def page(token, since, offset, size):
    body = {"structuredQuery": {
        "from": [{"collectionId": "reports"}],
        "where": {"fieldFilter": {"field": {"fieldPath": "at"}, "op": "GREATER_THAN_OR_EQUAL", "value": {"timestampValue": since}}},
        "orderBy": [{"field": {"fieldPath": "at"}, "direction": "ASCENDING"}],
        "offset": offset, "limit": size}}
    req = urllib.request.Request(URL, json.dumps(body).encode(), {"Authorization": "Bearer " + token, "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as r:
        return [x["document"] for x in json.load(r) if "document" in x]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--days", type=float, default=2)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    token = os.environ["TOKEN"]
    since = (datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(days=a.days)).strftime("%Y-%m-%dT%H:%M:%SZ")
    n, offset, installs = 0, 0, set()
    with open(a.out, "w") as f:
        while True:
            docs = page(token, since, offset, 200)
            for d in docs:
                row = {k: plain(v) for k, v in d.get("fields", {}).items()}
                row["id"] = d["name"].rsplit("/", 1)[-1]
                installs.add(row.get("install"))
                f.write(json.dumps(row, ensure_ascii=False) + "\n")
                n += 1
            offset += len(docs)
            if len(docs) < 200:
                break
    print(f"read {n} report(s) from {len(installs)} install(s), since {since}")


if __name__ == "__main__":
    sys.exit(main())
