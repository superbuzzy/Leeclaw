#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/deploy/.env"
DERIVED="$ROOT/build/openclaw-leeclaw"

# OpenClaw owns the login screen. Shared-password mode resolves to its durable
# gateway-owner profile; downstream identities remain server-side mappings.
node - "$ROOT/deploy/openclaw/state/openclaw.json" <<'NODE'
const fs = require("node:fs");
const path = process.argv[2];
const config = JSON.parse(fs.readFileSync(path, "utf8"));
config.gateway ??= {};
config.gateway.auth = { mode: "password" };
config.gateway.controlUi ??= {};
const origins = new Set(config.gateway.controlUi.allowedOrigins ?? []);
origins.add("http://localhost:18789");
origins.add("http://127.0.0.1:18789");
config.gateway.controlUi.allowedOrigins = [...origins];
fs.writeFileSync(path, JSON.stringify(config, null, 2) + "\n");
NODE
# The derived tree is disposable. Rebuild it every time so plugin, adapter and
# image patches cannot remain stale across a LeeClaw update.
"$ROOT/cobra-knowledge/integrations/openclaw/apply-integration.sh" "$ROOT/upstream/openclaw" "$DERIVED"

BUILD_PATCH="$ROOT/deploy/openclaw-local-build.patch"
if patch --dry-run --forward -d "$DERIVED" -p1 < "$BUILD_PATCH" >/dev/null 2>&1; then
  patch --forward -d "$DERIVED" -p1 < "$BUILD_PATCH"
elif patch --dry-run --reverse -d "$DERIVED" -p1 < "$BUILD_PATCH" >/dev/null 2>&1; then
  echo "OpenClaw local build patch is already applied"
else
  echo "OpenClaw local build patch no longer applies cleanly; inspect $BUILD_PATCH" >&2
  exit 1
fi

docker compose --env-file "$ENV_FILE" -p leeclaw-openclaw \
  -f "$DERIVED/docker-compose.yml" \
  -f "$ROOT/deploy/openclaw.override.yaml" \
  up -d --build openclaw-gateway auth-gateway
