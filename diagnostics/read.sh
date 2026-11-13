#!/usr/bin/env bash
# read.sh [days] [outdir]: reads the problem reports of the last N days (default 2) into <outdir>/reports.jsonl
# (default ./diagnostics-out). Needs: gh (logged in, REST only), git, openssl, gzip, python3.
#
# How: makes a one-time RSA key pair in a temporary folder, starts the workflow "Read problem reports" with the public
# key, waits for it, fetches the encrypted result from the branch diagnostics-data, decrypts it, and deletes the key.
# The reports are private data of the people who sent them (scrubbed, but still): keep reports.jsonl out of commits,
# issues, pull requests and chat; quote only what a fix needs (a cause, a version, a number of installs).
set -euo pipefail
days="${1:-2}"; out="${2:-./diagnostics-out}"
repo="${PULLWELL_REPO:-nisar87/pullwell}"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$tmp/priv.pem" 2>/dev/null
openssl pkey -in "$tmp/priv.pem" -pubout -out "$tmp/pub.pem"
pub="$(base64 -w0 "$tmp/pub.pem")"

since="$(date -u +%FT%TZ)"
gh api -X POST "repos/$repo/actions/workflows/diagnostics.yml/dispatches" -f ref=main -f "inputs[days]=$days" -f "inputs[pubkey]=$pub" >/dev/null
echo "started the reading workflow, waiting..."
run=""
for i in $(seq 60); do
  sleep 5
  run="$(gh api "repos/$repo/actions/workflows/diagnostics.yml/runs?event=workflow_dispatch&per_page=5" --jq "[.workflow_runs[]|select(.created_at >= \"$since\")]|first|.id // empty")"
  [ -n "$run" ] && break
done
[ -n "$run" ] || { echo "the workflow did not start"; exit 2; }
for i in $(seq 90); do
  st="$(gh api "repos/$repo/actions/runs/$run" --jq '.status+" "+(.conclusion // "")')"
  case "$st" in "completed success") break;; completed*) echo "the workflow failed ($st): https://github.com/$repo/actions/runs/$run"; exit 3;; esac
  sleep 5
done
[ "$st" = "completed success" ] || { echo "the workflow did not finish in time"; exit 4; }

git init -q "$tmp/data"
git -C "$tmp/data" fetch -q --depth 1 "https://github.com/$repo.git" diagnostics-data
git -C "$tmp/data" show FETCH_HEAD:key.enc > "$tmp/key.enc"
git -C "$tmp/data" show FETCH_HEAD:reports.enc > "$tmp/reports.enc"
openssl pkeyutl -decrypt -inkey "$tmp/priv.pem" -pkeyopt rsa_padding_mode:oaep -pkeyopt rsa_oaep_md:sha256 -in "$tmp/key.enc" -out "$tmp/key.txt"
openssl enc -d -aes-256-cbc -pbkdf2 -iter 200000 -pass file:"$tmp/key.txt" -in "$tmp/reports.enc" | gunzip > "$out/reports.jsonl"
echo "$(wc -l < "$out/reports.jsonl") report line(s) in $out/reports.jsonl"
