#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO="$(cd "$ROOT/.." && pwd)"
cd "$ROOT"

echo "[version] LeeClaw v0.10.1"
[ "$(cat VERSION)" = "0.10.1" ]

echo "[build] Go, vet, binaries and plugin syntax"
if command -v go >/dev/null 2>&1; then
  go test ./... -count=1 -timeout=60s
  go vet ./...
  mkdir -p bin
  go build -o bin/cobra-knowledge ./cmd/cobra
  go build -o bin/cobra-context-mcp ./cmd/context-mcp
  go build -o bin/cobra-graph-api ./cmd/graph-api
else
  command -v docker >/dev/null 2>&1 || { echo "FAIL [build] neither Go nor Docker is available" >&2; exit 1; }
  docker run --rm --user "$(id -u):$(id -g)" -e GOCACHE=/tmp/go-cache -e GOPATH=/tmp/go \
    -v "$ROOT:/src" -w /src golang:1.23-alpine sh -c \
    'go test ./... -count=1 -timeout=60s && go vet ./... && mkdir -p bin && go build -o bin/cobra-knowledge ./cmd/cobra && go build -o bin/cobra-context-mcp ./cmd/context-mcp && go build -o bin/cobra-graph-api ./cmd/graph-api'
fi
for f in integrations/openclaw/workspace-core/index.js integrations/openclaw/knowledge-plugin/index.js integrations/openclaw/knowledge-plugin/lib/*.js integrations/openclaw/openviking-plugin/index.js integrations/openclaw/openviking-plugin/lib/*.js integrations/openclaw/promotion-plugin/index.js integrations/openclaw/promotion-plugin/lib/*.js; do node --check "$f"; done
node --check "$REPO/scripts/verify-knowledge-ui.mjs"
node --test integrations/openclaw/workspace-core/test/*.test.js integrations/openclaw/knowledge-plugin/test/*.test.js integrations/openclaw/openviking-plugin/test/*.test.js

echo "[account] OpenClaw Profile -> Workspace binding"
node - "$REPO/deploy/leeclaw/workspaces.json" <<'NODE'
const fs = require("node:fs");
const registry = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
if (!Array.isArray(registry.workspaces) || !registry.workspaces.length) throw new Error("no workspaces");
for (const ws of registry.workspaces) {
  if (!Array.isArray(ws.members) || !ws.members.some((m) => m.profileId && m.role)) throw new Error(`${ws.id}: no OpenClaw Profile membership`);
}
NODE
grep -Fq 'authenticatedUserProfile?.profileId' integrations/openclaw/openviking-plugin/lib/principal.js
grep -Fq 'config.gateway.auth = { mode: "password" }' "$REPO/scripts/start-openclaw.sh"
grep -Fq 'profileId": "gateway-owner"' "$REPO/deploy/leeclaw/workspaces.json"
! grep -Fq 'loginHTML' cmd/auth-gateway/main.go

echo "[openviking] Workspace -> account mapping"
node - "$REPO/deploy/leeclaw/workspaces.json" <<'NODE'
const fs = require("node:fs"); const r = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
for (const ws of r.workspaces) if (!ws.openviking?.accountId) throw new Error(`${ws.id}: missing OpenViking account`);
NODE
grep -Fq 'headers["X-OpenViking-Account"]' integrations/openclaw/openviking-plugin/lib/client.js

echo "[weknora] Workspace -> Tenant / KB scope mapping"
node - "$REPO/deploy/leeclaw/workspaces.json" <<'NODE'
const fs = require("node:fs"); const r = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
for (const ws of r.workspaces) {
  if (!ws.weknora?.tenantId || !ws.weknora?.apiKeyEnv || !Array.isArray(ws.weknora?.knowledgeBaseIds)) throw new Error(`${ws.id}: incomplete WeKnora scope`);
}
NODE
grep -Fq 'weknoraKnowledgeBaseIds' integrations/openclaw/workspace-core/index.js

echo "[memory] OpenClaw Memory surface -> LeeClawMemoryService -> OpenViking"
grep -Fq '"kind":"memory"' integrations/openclaw/openviking-plugin/openclaw.plugin.json
grep -Fq '"memory": "leeclaw-openviking"' configs/openclaw-v0.10.example.json
for method in listMemories searchMemories getMemory createMemory updateMemory deleteMemory listSessions getSession searchArchive getMemorySource; do grep -Fq "$method" integrations/openclaw/openviking-plugin/lib/memory-service.js; done
grep -Fq 'target_uri: "viking://~/memories"' integrations/openclaw/openviking-plugin/lib/client.js
! grep -Fq 'registerNavigation({ id: "memory"' integrations/openclaw/openviking-plugin/browser/index.js
! grep -Fq 'registerNavigation({ id: "skills"' integrations/openclaw/openviking-plugin/browser/index.js

echo "[knowledge-ui] WeKnora native Knowledge UI through server-derived session"
grep -Fq 'leeclaw.knowledge.webSession' integrations/openclaw/knowledge-plugin/index.js
grep -Fq 'platform/knowledge-bases?leeclaw_embed=1' integrations/openclaw/knowledge-plugin/browser/index.js
grep -Fq 'WEKNORA_API_KEY_LEECLAW' cmd/auth-gateway/main.go
grep -Fq 'WEKNORA_BOOTSTRAP_PASSWORD' cmd/auth-gateway/main.go
grep -Fq 'Authorization", "Bearer "+sess.weknoraToken' cmd/auth-gateway/main.go
! grep -Fq 'id:"workspaces"' integrations/openclaw/knowledge-plugin/browser/index.js

echo "[promotion] Experience -> Candidate -> Review -> Publish"
grep -Fq 'type Candidate struct' internal/promotion/types.go
grep -Fq 'func promotionGate' internal/promotion/service.go
grep -Fq 'verifyTraceability' internal/promotion/openviking_backend.go
grep -Fq 'delete_created_knowledge' internal/promotion/weknora_backend.go
grep -Fq 'restore_previous_skill' internal/promotion/openviking_backend.go
for decision in DecisionCreate DecisionUpdate DecisionSkip DecisionConflict; do grep -Fq "$decision" internal/promotion/types.go; done
grep -Fq 'promotion authentication required' internal/promotionapi/handler.go
grep -Fq 'operator.admin' integrations/openclaw/promotion-plugin/index.js
! grep -Eq 'promotion.*(publish|rollback)' integrations/openclaw/knowledge-plugin/openclaw.plugin.json

echo "[sandbox] persistent 18789 / 18790 exposure"
grep -Fq 'OPENCLAW_SANDBOX_PORT:-18790' "$REPO/deploy/openclaw.override.yaml"
grep -Fq 'LEECLAW_SANDBOX_UPSTREAM' "$REPO/deploy/openclaw.override.yaml"
grep -Fq 'LEECLAW_KNOWLEDGE_PORT:-18791' "$REPO/deploy/openclaw.override.yaml"
node -e 'const c=require(process.argv[1]); if(c.mcp?.apps?.enabled!==true || c.mcp.apps.sandboxPort!==18790) process.exit(1)' "$REPO/deploy/openclaw/state/openclaw.json"

echo "[browser] image runtime, libraries and Chinese fonts"
grep -Fq 'OPENCLAW_IMAGE_APT_PACKAGES: chromium fonts-noto-cjk fonts-noto-color-emoji fontconfig' "$REPO/deploy/openclaw.override.yaml"
node -e 'const c=require(process.argv[1]); if(c.browser?.executablePath!=="/usr/bin/chromium") process.exit(1)' "$REPO/deploy/openclaw/state/openclaw.json"
grep -Fq 'Acquire::ForceIPv4=true' "$REPO/deploy/openclaw-local-build.patch"

runtime=0
if [ "${LEECLAW_RUNTIME_VERIFY:-}" = 1 ] || docker inspect leeclaw-openclaw >/dev/null 2>&1; then runtime=1; fi
if [ "$runtime" = 1 ]; then
  "$REPO/scripts/check-gateway-sandbox.sh"
  "$REPO/scripts/verify-browser-runtime.sh"
  for spec in 'openviking:http://127.0.0.1:1933/health' 'weknora:http://127.0.0.1:8080/health' 'promotion:http://127.0.0.1:8090/healthz'; do
    layer="${spec%%:*}"; url="${spec#*:}"; code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "$url" || true)"
    [ "$code" != 000 ] || { echo "FAIL [$layer:runtime] $url unreachable" >&2; exit 1; }
    echo "PASS [$layer:runtime] $url -> HTTP $code"
  done
  if [ "${LEECLAW_UI_VERIFY:-}" = 1 ]; then
    docker run --rm --network host --env-file "$REPO/deploy/.env" \
      -e LEECLAW_UI_ARTIFACT_DIR=/artifacts \
      -v "$REPO/scripts/verify-knowledge-ui.mjs:/verify-knowledge-ui.mjs:ro" \
      -v /tmp:/artifacts openclaw:local node /verify-knowledge-ui.mjs
  else
    echo "SKIP [ui] set LEECLAW_UI_VERIFY=1 after approving this browser once in OpenClaw"
  fi
else
  echo "SKIP [runtime] containers are not running; set LEECLAW_RUNTIME_VERIFY=1 to require live checks"
fi

echo "[persistence] state mounts and recreate contract"
grep -Fq 'OPENCLAW_CONFIG_DIR' "$REPO/upstream/openclaw/docker-compose.yml"
grep -Fq 'LEECLAW_DESTRUCTIVE_RECREATE' "$REPO/scripts/recreate-v0.10-test.sh"
grep -Fq '/usr/bin/chromium' "$REPO/docs/V0.10_PATCH_PERSISTENCE.md"
if [ "${LEECLAW_DESTRUCTIVE_RECREATE:-}" = 1 ] && [ "${LEECLAW_RECREATE_CHILD:-}" != 1 ]; then
  "$REPO/scripts/recreate-v0.10-test.sh"
fi

echo "[upstream] OpenClaw / WeKnora / OpenViking clean"
./scripts/check-upstream-clean.sh
echo "PASS LeeClaw v0.10.1 patch verification"
