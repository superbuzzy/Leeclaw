#!/usr/bin/env bash
set -euo pipefail
docker ps --filter 'name=leeclaw-' --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
echo
for url in \
  http://127.0.0.1:8080/health \
  http://127.0.0.1:8081/ \
  http://127.0.0.1:1933/health \
  http://127.0.0.1:8090/healthz \
  http://127.0.0.1:18789/health \
  http://127.0.0.1:18791/health; do
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 "$url" || true)"
  printf '%-40s %s\n' "$url" "$code"
done
code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:18790/ || true)"
printf '%-40s %s\n' http://127.0.0.1:18790/ "$code"
