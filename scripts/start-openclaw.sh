#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/deploy/.env"
DERIVED="$ROOT/build/openclaw-leeclaw"
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
