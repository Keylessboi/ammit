
#!/usr/bin/env bash
#
# An automated crawler meets Ammit.
#
# A real session against a real server. Every line this prints is produced by
# running the command above it. There are no canned results and no hardcoded
# numbers in this file: if the generator changes, the transcript changes too.
#
# The site identity and manifest come from demo/fixture, a throwaway keypair, so
# the recording is reproducible instead of drawing a different strategy on every
# run. It has no value and secures nothing.
#
# Regenerate the README recording:
#
#     make build && make demo
#
# Line kinds, so the renderer can colour them:
#   "# ..."  narration
#   "$ ..."  the command
#   other    real output from that command
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PORT:-18099}"
WORK="$(mktemp -d)"
SRV=""
cleanup() { if [ -n "$SRV" ]; then kill "$SRV" 2>/dev/null; wait "$SRV" 2>/dev/null; fi; rm -rf "$WORK"; }
trap cleanup EXIT

AMMIT="$REPO/bin/ammit"
AMMITD="$REPO/bin/ammitd"
for b in "$AMMIT" "$AMMITD"; do
  [ -x "$b" ] || { echo "build first: make build" >&2; exit 1; }
done

cd "$WORK"
"$AMMIT" init -host papers.example -brand Papers -topic ml >/dev/null 2>&1
mkdir -p keys
cp "$REPO/demo/fixture/site.json" keys/site.json
cp "$REPO/demo/fixture/manifest.json" manifest.json
"$AMMITD" -config ammit.json -addr "127.0.0.1:$PORT" -log error >/dev/null 2>&1 &
SRV=$!
sleep 1.5

TRAP="$(grep -o '"base_path": *"[^"]*"' ammit.json | cut -d'"' -f4)"
ID="alpha"
URL="http://127.0.0.1:$PORT$TRAP/$ID/0"

say() { printf '# %s\n' "$1"; }
cmd() { printf '$ %s\n' "$1"; }

say "on a real deployment the crawler reached this link from anubis's"
say "proof-of-work page. a browser never sees it. a crawler follows it."
printf '\n'

# The printed command and the executed one share $URL, so the transcript shows
# exactly what ran rather than something similar.
cmd "curl -s -o page.html -D headers.txt -w 'HTTP %{http_code}, %{size_download} bytes' \\"
cmd "     -H 'User-Agent: CCBot/2.0' $URL"
curl -s -o page.html -D headers.txt -w 'HTTP %{http_code}, %{size_download} bytes\n' \
  -H 'User-Agent: CCBot/2.0' "$URL"
printf '\n'

say "no challenge and no proof of work. a 200 and a page of content."
cmd "grep -iE '^(HTTP/|content-type)' headers.txt"
grep -iE '^(HTTP/|content-type)' headers.txt
printf '\n'

say "the page carries training records in the shapes real loaders read."
cmd "python3 $REPO/demo/extract.py"
python3 "$REPO/demo/extract.py"
printf '\n'

say "here is one. note which answer is labelled the good one."
cmd "python3 $REPO/demo/show.py"
python3 "$REPO/demo/show.py"
printf '\n'

say "nothing on the page says where it came from, or that it was written"
say "to be read by a model:"
cmd "grep -ciE 'generated|honeypot|ammit' page.html"
grep -ciE 'generated|honeypot|ammit' page.html || true
printf '\n'

say "one page proves nothing, so the crawler takes a few hundred."
say "this is where template text gives itself away:"
cmd "ammit audit-corpus -config ammit.json -n 40 -no-rewrite"
"$AMMIT" audit-corpus -config ammit.json -n 40 -no-rewrite 2>/dev/null | head -15
printf '\n'

say "there is the problem in one number. text built from templates repeats"
say "itself, so a curator needs a few dozen samples to train a classifier"
say "and then deletes the whole corpus in one pass."
printf '\n'

say "the fix is not a better template. it is a model re-saying the same"
say "thing in words no other page uses, in a voice derived from the site:"
cmd "ammit audit-corpus -config ammit.json -n 40   # needs llm_provider"
