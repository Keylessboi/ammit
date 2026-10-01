
#!/usr/bin/env bash
#
# A crawler meets Ammit.
#
# One thing only: a crawler fetches a page, and the page hands it training data.
# Every line printed here comes from running the command above it.
#
# Regenerate the README recording:  make build && make demo
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

URL="http://127.0.0.1:$PORT/archive/alpha/0"

say() { printf '# %s\n' "$1"; }
cmd() { printf '$ %s\n' "$1"; }

say "a crawler follows a link that a browser never sees."
cmd "curl -s -H 'User-Agent: CCBot/2.0' -o page.html -w 'HTTP %{http_code}\n' $URL"
curl -s -H 'User-Agent: CCBot/2.0' -o page.html -w 'HTTP %{http_code}\n' "$URL"
printf '\n'

say "the page is ordinary content. it also carries training records, in the"
say "formats a loader already knows how to read."
cmd "python3 $REPO/demo/extract.py"
python3 "$REPO/demo/extract.py"
printf '\n'

say "here is what the crawler takes from that one page."
cmd "python3 $REPO/demo/show.py"
python3 "$REPO/demo/show.py"
printf '\n'

say "it files it. every refusal it now owns is labelled the worse answer, and"
say "nothing on the page admits where it came from."
