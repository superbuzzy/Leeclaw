#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONTAINER="${OPENCLAW_CONTAINER:-leeclaw-openclaw}"
CONFIG="${OPENCLAW_CONFIG_PATH_HOST:-$ROOT/deploy/openclaw/state/openclaw.json}"
fail() { echo "FAIL [browser:$1] $2" >&2; exit 1; }

browser_path="$(node -e 'const c=require(process.argv[1]); process.stdout.write(c.browser?.executablePath || "")' "$CONFIG")"
[ -n "$browser_path" ] || fail config "browser.executablePath is empty"
case "$browser_path" in /home/node/wk/*) fail persistence "executablePath points at the container writable layer: $browser_path";; esac
docker inspect "$CONTAINER" >/dev/null 2>&1 || fail process "container $CONTAINER does not exist"
docker exec "$CONTAINER" test -x "$browser_path" || fail executable "missing or non-executable: $browser_path"
echo "PASS [browser:executable] $browser_path"

missing="$(docker exec "$CONTAINER" sh -c 'ldd "$1" 2>&1 | grep "not found" || true' sh "$browser_path")"
[ -z "$missing" ] || fail libraries "$missing"
echo "PASS [browser:libraries] ldd has no missing libraries"

zh_count="$(docker exec "$CONTAINER" sh -c "fc-list :lang=zh 2>/dev/null | wc -l" | tr -d '[:space:]')"
[ "${zh_count:-0}" -gt 0 ] || fail fonts "fc-list :lang=zh returned no fonts"
echo "PASS [browser:fonts] $zh_count Chinese font faces"

docker exec "$CONTAINER" "$browser_path" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage --dump-dom \
  'data:text/html;charset=utf-8,%3Ch1%3ELeeClaw%20Browser%20OK%3C%2Fh1%3E' 2>/dev/null | grep -q 'LeeClaw Browser OK' \
  || fail english-render "headless English DOM render failed"
docker exec "$CONTAINER" sh -c 'rm -f /tmp/leeclaw-browser-zh.png; "$1" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage --screenshot=/tmp/leeclaw-browser-zh.png --window-size=640,240 "data:text/html;charset=utf-8,%3Ccanvas%20id=c%20width=600%20height=180%3E%3C/canvas%3E%3Cscript%3Elet%20x=c.getContext(%27'"'"'2d%27'"'"')%3Bx.font=%27'"'"'32px%20sans-serif%27'"'"'%3Bx.fillText(%27'"'"'LeeClaw%20中文渲染%20🦞%27'"'"'%2C20%2C70)%3B%3C%2Fscript%3E" >/dev/null 2>&1; test -s /tmp/leeclaw-browser-zh.png; test "$(od -An -tx1 -N8 /tmp/leeclaw-browser-zh.png | tr -d " \n")" = 89504e470d0a1a0a' sh "$browser_path" \
  || fail canvas-render "Chinese Canvas screenshot failed or was not a PNG"
echo "PASS [browser:render] English DOM and Chinese Canvas screenshot"
