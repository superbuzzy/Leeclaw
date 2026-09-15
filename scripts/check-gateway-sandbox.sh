#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONTAINER="${OPENCLAW_CONTAINER:-leeclaw-openclaw}"
PROXY_CONTAINER="${LEECLAW_PROXY_CONTAINER:-leeclaw-auth-gateway}"
GATEWAY_URL="${LEECLAW_EXTERNAL_GATEWAY_URL:-http://127.0.0.1:${OPENCLAW_GATEWAY_PORT:-18789}}"
SANDBOX_URL="${LEECLAW_EXTERNAL_SANDBOX_URL:-http://127.0.0.1:${OPENCLAW_SANDBOX_PORT:-18790}}"
KNOWLEDGE_URL="${LEECLAW_EXTERNAL_KNOWLEDGE_URL:-http://127.0.0.1:${LEECLAW_KNOWLEDGE_PORT:-18791}}"
CONFIG="${OPENCLAW_CONFIG_PATH_HOST:-$ROOT/deploy/openclaw/state/openclaw.json}"

fail() { echo "FAIL [sandbox:$1] $2" >&2; exit 1; }
probe_external() {
  local layer="$1" url="$2" code
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$url" || true)"
  [ "$code" != "000" ] || fail "$layer" "$url is unreachable"
  echo "PASS [sandbox:$layer] $url -> HTTP $code"
}

docker inspect "$CONTAINER" >/dev/null 2>&1 || fail process "container $CONTAINER does not exist"
[ "$(docker inspect -f '{{.State.Running}}' "$CONTAINER")" = true ] || fail process "container $CONTAINER is not running"
docker inspect "$PROXY_CONTAINER" >/dev/null 2>&1 || fail proxy "container $PROXY_CONTAINER does not exist"
[ "$(docker inspect -f '{{.State.Running}}' "$PROXY_CONTAINER")" = true ] || fail proxy "container $PROXY_CONTAINER is not running"
[ "$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$PROXY_CONTAINER")" = healthy ] || fail proxy "container $PROXY_CONTAINER is not healthy"
for port in 18789 18790; do
  docker exec "$CONTAINER" node -e "fetch('http://127.0.0.1:$port/').then(()=>process.exit(0),()=>process.exit(1))" \
    || fail container "listener 127.0.0.1:$port is unreachable inside $CONTAINER"
  echo "PASS [sandbox:container] $CONTAINER:127.0.0.1:$port reachable"
done

probe_external proxy-gateway "$GATEWAY_URL/health"
probe_external proxy-sandbox "$SANDBOX_URL/"
probe_external proxy-knowledge "$KNOWLEDGE_URL/health"

node - "$CONFIG" "$SANDBOX_URL" <<'NODE' || fail browser-origin "configured origin/port does not match the browser-visible sandbox URL"
const fs = require("node:fs");
const cfg = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const apps = cfg.mcp?.apps;
if (apps?.enabled !== true) throw new Error("mcp.apps.enabled must be true");
const external = new URL(process.argv[3]);
if (apps.sandboxOrigin) {
  if (new URL(apps.sandboxOrigin).origin !== external.origin) throw new Error("sandboxOrigin mismatch");
} else if (Number(apps.sandboxPort ?? 18790) !== Number(external.port || (external.protocol === "https:" ? 443 : 80))) {
  throw new Error("sandboxPort mismatch");
}
NODE
echo "PASS [sandbox:browser-origin] OpenClaw config matches $SANDBOX_URL"
