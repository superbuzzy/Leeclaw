#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/deploy/.env"
DERIVED="$ROOT/build/openclaw-leeclaw"
if [ -f "$DERIVED/docker-compose.yml" ]; then
  docker compose --env-file "$ENV_FILE" -p leeclaw-openclaw \
    -f "$DERIVED/docker-compose.yml" \
    -f "$ROOT/deploy/openclaw.override.yaml" down
fi
docker compose --env-file "$ENV_FILE" -p leeclaw -f "$ROOT/deploy/compose.yaml" down
docker compose --env-file "$ENV_FILE" -p leeclaw-weknora \
  -f "$ROOT/upstream/weknora/docker-compose.yml" \
  -f "$ROOT/deploy/weknora.override.yaml" --profile neo4j down
