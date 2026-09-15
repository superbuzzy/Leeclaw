#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/deploy/.env"

[ -f "$ENV_FILE" ] || { echo "missing $ENV_FILE" >&2; exit 1; }
docker network inspect leeclaw-net >/dev/null 2>&1 || docker network create leeclaw-net >/dev/null
echo "[1/5] starting WeKnora + Neo4j"
docker compose --env-file "$ENV_FILE" -p leeclaw-weknora \
  -f "$ROOT/upstream/weknora/docker-compose.yml" \
  -f "$ROOT/deploy/weknora.override.yaml" \
  --profile neo4j up -d postgres redis docreader neo4j app frontend

echo "[2/5] starting OpenViking"
docker compose --env-file "$ENV_FILE" -p leeclaw \
  -f "$ROOT/deploy/compose.yaml" up -d openviking

echo "[3/5] initializing WeKnora tenant"
"$ROOT/scripts/init-weknora.sh"

echo "[4/5] building and starting LeeClaw Core"
docker compose --env-file "$ENV_FILE" -p leeclaw \
  -f "$ROOT/deploy/compose.yaml" up -d --build leeclaw-core

echo "[5/5] building and starting OpenClaw"
"$ROOT/scripts/start-openclaw.sh"

echo "LeeClaw services submitted; run scripts/status.sh for health"
