#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/deploy/.env"
BASE_URL="${WEKNORA_BASE_URL:-http://127.0.0.1:8080}"

[ -f "$ENV_FILE" ] || { echo "missing $ENV_FILE" >&2; exit 1; }

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

tmp_dir="$(mktemp -d /tmp/leeclaw-weknora-init.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

for _ in $(seq 1 30); do
  if curl -fsS "$BASE_URL/health" >/dev/null; then
    break
  fi
  sleep 2
done
curl -fsS "$BASE_URL/health" >/dev/null

api_key="${WEKNORA_API_KEY_LEECLAW:-}"
tenant_id=""
if [ -z "$api_key" ]; then
  email="${WEKNORA_BOOTSTRAP_EMAIL:-leeclaw@local.invalid}"
  password="${WEKNORA_BOOTSTRAP_PASSWORD:-}"
  if [ -z "$password" ]; then
    password="Lc1!$(openssl rand -hex 16)"
    sed -i "s|^WEKNORA_BOOTSTRAP_PASSWORD=.*$|WEKNORA_BOOTSTRAP_PASSWORD=$password|" "$ENV_FILE"
  fi

  register_code="$(curl -sS -o "$tmp_dir/register.json" -w '%{http_code}' \
    "$BASE_URL/api/v1/auth/register" \
    -H 'Content-Type: application/json' \
    --data "{\"username\":\"LeeClaw Local\",\"email\":\"$email\",\"password\":\"$password\"}")"
  if [ "$register_code" != "201" ] && [ "$register_code" != "400" ]; then
    echo "WeKnora registration failed (HTTP $register_code)" >&2
    exit 1
  fi

  login_code="$(curl -sS -o "$tmp_dir/login.json" -w '%{http_code}' \
    "$BASE_URL/api/v1/auth/login" \
    -H 'Content-Type: application/json' \
    --data "{\"email\":\"$email\",\"password\":\"$password\"}")"
  if [ "$login_code" != "200" ]; then
    echo "WeKnora login failed (HTTP $login_code)" >&2
    exit 1
  fi

  token="$(node -e 'const d=require(process.argv[1]); process.stdout.write(d.token || "")' "$tmp_dir/login.json")"
  tenant_id="$(node -e 'const d=require(process.argv[1]); process.stdout.write(String(d.active_tenant?.id || ""))' "$tmp_dir/login.json")"
  if [ -z "$token" ] || [ -z "$tenant_id" ]; then
    echo "WeKnora login response did not include token and tenant" >&2
    exit 1
  fi

  key_code="$(curl -sS -o "$tmp_dir/key.json" -w '%{http_code}' \
    "$BASE_URL/api/v1/tenants/$tenant_id/api-keys" \
    -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' \
    --data '{"name":"LeeClaw Runtime","full_access":true,"knowledge_base_ids":[],"capabilities":[]}')"
  if [ "$key_code" != "200" ] && [ "$key_code" != "201" ]; then
    echo "WeKnora API key creation failed (HTTP $key_code)" >&2
    exit 1
  fi

  api_key="$(node -e 'const d=require(process.argv[1]); process.stdout.write(d.data?.token || d.data?.api_key || "")' "$tmp_dir/key.json")"
  if [ -z "$api_key" ]; then
    echo "WeKnora API key response did not include the one-time token" >&2
    exit 1
  fi
  sed -i "s|^WEKNORA_API_KEY_LEECLAW=.*$|WEKNORA_API_KEY_LEECLAW=$api_key|" "$ENV_FILE"
else
  echo "WeKnora tenant API key is already configured"
fi

list_code="$(curl -sS -o "$tmp_dir/kbs.json" -w '%{http_code}' \
  "$BASE_URL/api/v1/knowledge-bases?page=1&page_size=100" \
  -H "X-API-Key: $api_key")"
if [ "$list_code" != "200" ]; then
  echo "WeKnora knowledge base listing failed (HTTP $list_code)" >&2
  exit 1
fi
kb_id="$(node -e 'const fs=require("fs"); const d=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); const rows=Array.isArray(d.data)?d.data:(d.data?.items||[]); process.stdout.write(rows.find(x=>x.name==="LeeClaw Knowledge")?.id||"")' "$tmp_dir/kbs.json")"

if [ -z "$kb_id" ]; then
  create_code="$(curl -sS -o "$tmp_dir/kb-create.json" -w '%{http_code}' \
    "$BASE_URL/api/v1/knowledge-bases" \
    -H "X-API-Key: $api_key" \
    -H 'Content-Type: application/json' \
    --data '{"name":"LeeClaw Knowledge","description":"Default durable knowledge base for LeeClaw","type":"document","is_temporary":false,"chunking_config":{"chunk_size":1000,"chunk_overlap":200,"separators":["\n\n","\n","。","."]},"embedding_model_id":"leeclaw-embedding-default","summary_model_id":"leeclaw-deepseek-default","storage_provider_config":{"provider":"local"}}')"
  if [ "$create_code" != "200" ] && [ "$create_code" != "201" ]; then
    echo "WeKnora knowledge base creation failed (HTTP $create_code)" >&2
    exit 1
  fi
  kb_id="$(node -e 'const d=require(process.argv[1]); process.stdout.write(d.data?.id||"")' "$tmp_dir/kb-create.json")"
fi

if [ -z "$kb_id" ]; then
  echo "WeKnora knowledge base ID is missing" >&2
  exit 1
fi

[ -n "$tenant_id" ] || tenant_id="$(node -e 'const fs=require("fs"); const d=JSON.parse(fs.readFileSync(process.argv[1],"utf8")); const rows=Array.isArray(d.data)?d.data:(d.data?.items||[]); process.stdout.write(String(rows.find(x=>x.name==="LeeClaw Knowledge")?.tenant_id||""))' "$tmp_dir/kbs.json")"
[ -n "$tenant_id" ] || tenant_id="10000"
if grep -q '^WEKNORA_TENANT_ID=' "$ENV_FILE"; then
  sed -i "s|^WEKNORA_TENANT_ID=.*$|WEKNORA_TENANT_ID=$tenant_id|" "$ENV_FILE"
else
  sed -i "/^WEKNORA_API_KEY_LEECLAW=/a WEKNORA_TENANT_ID=$tenant_id" "$ENV_FILE"
fi
sed -i -E "s|\"tenantId\": \"[0-9]+\"|\"tenantId\": \"$tenant_id\"|" "$ROOT/deploy/leeclaw/workspaces.json"
sed -i -E "s|\"knowledgeBaseIds\": \[[^]]*\]|\"knowledgeBaseIds\": [\"$kb_id\"]|" "$ROOT/deploy/leeclaw/workspaces.json"
chmod 600 "$ENV_FILE"
echo "WeKnora initialized for LeeClaw (tenant $tenant_id, knowledge base $kb_id)"
