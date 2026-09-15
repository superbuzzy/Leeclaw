#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [ "${LEECLAW_DESTRUCTIVE_RECREATE:-}" != "1" ]; then
  echo "Refusing to recreate containers. Re-run with LEECLAW_DESTRUCTIVE_RECREATE=1." >&2
  exit 2
fi

STATE="$ROOT/deploy/openclaw/state"
USER_DATA="$STATE/browser/openclaw/user-data"
MARKER="leeclaw-v0.10-recreate-$(date +%s)-$$"
mkdir -p "$USER_DATA"
printf '%s\n' "$MARKER" > "$STATE/.v0.10-persistence-marker"
printf '%s\n' "$MARKER" > "$USER_DATA/.v0.10-persistence-marker"

echo "[recreate 1/4] preflight"
"$ROOT/scripts/check-gateway-sandbox.sh"
"$ROOT/scripts/verify-browser-runtime.sh"

echo "[recreate 2/4] removing only LeeClaw OpenClaw application/proxy containers"
docker rm -f leeclaw-auth-gateway leeclaw-openclaw >/dev/null

echo "[recreate 3/4] rebuilding without manual repair"
"$ROOT/scripts/start-openclaw.sh"
for _ in $(seq 1 60); do
  if [ "$(docker inspect -f '{{.State.Health.Status}}' leeclaw-openclaw 2>/dev/null || true)" = healthy ] && \
     [ "$(docker inspect -f '{{.State.Health.Status}}' leeclaw-auth-gateway 2>/dev/null || true)" = healthy ]; then break; fi
  sleep 2
done
[ "$(docker inspect -f '{{.State.Health.Status}}' leeclaw-openclaw 2>/dev/null || true)" = healthy ] || { echo "FAIL [persistence:openclaw-health] recreated OpenClaw did not become healthy" >&2; exit 1; }
[ "$(docker inspect -f '{{.State.Health.Status}}' leeclaw-auth-gateway 2>/dev/null || true)" = healthy ] || { echo "FAIL [persistence:proxy-health] recreated auth/sandbox proxy did not become healthy" >&2; exit 1; }

echo "[recreate 4/4] post-recreate unified verification"
[ "$(cat "$STATE/.v0.10-persistence-marker")" = "$MARKER" ] || { echo "FAIL [persistence:config] state marker was lost" >&2; exit 1; }
[ "$(cat "$USER_DATA/.v0.10-persistence-marker")" = "$MARKER" ] || { echo "FAIL [persistence:browser-user-data] marker was lost" >&2; exit 1; }
LEECLAW_RUNTIME_VERIFY=1 LEECLAW_RECREATE_CHILD=1 "$ROOT/cobra-knowledge/scripts/verify-v0.10.sh"
echo "PASS destructive v0.10 container recreation regression"
