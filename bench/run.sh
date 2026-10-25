#!/usr/bin/env bash
# Reproducible token benchmark: katana output vs raijin-crawl output on the same crawl.
#
# usage: bench/run.sh [workdir]
#        LIVE=1 bench/run.sh [workdir]   start the local lab, crawl it again and refresh bench/fixtures first
# needs: go, python3 with tiktoken (pip install tiktoken); LIVE=1 also needs katana
# optional: ANTHROPIC_API_KEY + pip install anthropic for exact Claude token counts
# env: PY=<python with tiktoken>, KATANA=<path>
#
# Safety: LIVE=1 crawls only the lab it starts itself (bench/lab.py) on 127.0.0.1, ports 19500 and
# 19501, as http://localhost:<port>/, depth 3, with katana's crawl scope set to localhost. It stops the
# lab when it exits. A live run takes about two minutes.
set -euo pipefail

W=${1:-$(mktemp -d)}
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/.." && pwd)
PY=${PY:-python3}
KATANA=${KATANA:-katana}
mkdir -p "$W"
echo "workdir: $W"

# Lab: one python process with two sites (see lab.py).
# noisy:    a catalog where almost every page is the same template: 60 user profiles, 40 products
#           with a reviews tab, 20 articles under a hash, 10 posts under a UUID, tag pages, CSS and JS.
# findings: a small app with a login form, an upload form, a search form, a JSON API, /graphql, a JS
#           file with more API routes, robots.txt and sitemap.xml pointing at a backup, an .env, an old
#           config and a debug endpoint, a 401, a 403 and a 500.
declare -A port=([noisy]=19500 [findings]=19501)
printf 'localhost\n' > "$W/scope.txt"

(cd "$ROOT" && go build -o "$W/raijin-crawl" ./cmd/raijin-crawl)

if [ "${LIVE:-}" = 1 ]; then
  for p in "${port[@]}"; do
    if ss -ltn | grep -q ":$p "; then echo "port $p is busy; free it first" >&2; exit 1; fi
  done
  python3 "$HERE/lab.py" "${port[noisy]}=shop" "${port[findings]}=app" > "$W/lab.log" 2>&1 &
  echo $! > "$W/pids"
  trap '[ -f "$W/pids" ] && xargs kill < "$W/pids" 2>/dev/null || true' EXIT
  sleep 1
  for name in noisy findings; do
    f=$HERE/fixtures/$name
    u=http://localhost:${port[$name]}/
    mkdir -p "$f"
    # raijin-crawl runs katana (-jsonl -or -fx -d 3 -jc -jsl -kf all -cs <host>) and saves its JSONL
    # as raw: that file is the fixture.
    "$W/raijin-crawl" --katana "$KATANA" --scope "$W/scope.txt" -u "$u" --raw "$f/raw.jsonl" > "$W/$name.live.json"
    # katana as an agent would run it, with the same crawl options: JSONL (raw request and response
    # included, katana's default) and text (one URL per line, banner and log on stderr).
    crawl=(-u "$u" -d 3 -jc -jsl -kf all -fx -cs '^https?://localhost(:[0-9]+)?([/?#]|$)' -duc -nc)
    "$KATANA" "${crawl[@]}" -jsonl -silent < /dev/null > "$f/katana.jsonl"
    "$KATANA" "${crawl[@]}" < /dev/null > "$f/text.txt" 2> "$f/text_stderr.txt"
    # what raijin-crawl said about the engine (engine_version, engine_format): the drift job reads it
    python3 -c 'import json,sys
for e in json.load(open(sys.argv[1])).get("errors") or []:
    print("live %s error: %s: %s" % (sys.argv[2], e["kind"], e["hint"]))' "$W/$name.live.json" "$name"
  done
fi

scenario() {
  local name=$1
  local f=$HERE/fixtures/$name o=$W/$name
  mkdir -p "$o"
  # The same lines as raw without the response bodies: katana -jsonl -or -ob, but with the endpoints
  # katana only finds when it keeps the body (-ob stops -jsl).
  python3 -c 'import json,sys
for l in open(sys.argv[1]):
    e = json.loads(l)
    (e.get("response") or {}).pop("body", None)
    print(json.dumps(e, separators=(",", ":")))' "$f/raw.jsonl" > "$o/lean.jsonl"
  # raijin-crawl replays raw with --input, so every row sees the same crawl.
  (cd "$ROOT" && "$W/raijin-crawl" --scope "$W/scope.txt" --input "bench/fixtures/$name/raw.jsonl") > "$o/raijin.json"
  (cd "$ROOT" && "$W/raijin-crawl" --scope "$W/scope.txt" --input "bench/fixtures/$name/raw.jsonl" --pretty) > "$o/raijin_pretty.json"
  (cd "$ROOT" && "$W/raijin-crawl" --scope "$W/scope.txt" --input "bench/fixtures/$name/raw.jsonl" --format table) > "$o/raijin_table.txt"
  echo; echo "## $name: $(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["summary"])' "$o/raijin.json")"; echo
  python3 "$HERE/verify.py" "$f/raw.jsonl" "$o/raijin.json"
  echo "separate katana runs: -jsonl $(grep -c '^{' "$f/katana.jsonl") results, text $(grep -c '^http' "$f/text.txt") URLs"
  "$PY" "$HERE/count_tokens.py" \
    "katana -jsonl=$f/katana.jsonl" \
    "katana -jsonl (raijin-crawl's raw)=$f/raw.jsonl" \
    "katana -jsonl, no bodies=$o/lean.jsonl" \
    "katana text (stdout+stderr)=$f/text.txt+$f/text_stderr.txt" \
    "raijin-crawl JSON (default)=$o/raijin.json" \
    "raijin-crawl --pretty=$o/raijin_pretty.json" \
    "raijin-crawl --format table=$o/raijin_table.txt"
}

scenario noisy
scenario findings
