#!/usr/bin/env bash
# SPA / operator route smoke against a running browse (or browse-stream) instance.
# Expects BROWSE_PORT (default 18080). Does not start or stop Compose.
set -euo pipefail

BROWSE_PORT="${BROWSE_PORT:-18080}"
BASE="http://127.0.0.1:${BROWSE_PORT}"

echo "spa_smoke against $BASE"

LOC=$(curl -sS -o /dev/null -w '%{http_code} %{redirect_url}' "$BASE/" || true)
CODE=$(awk '{print $1}' <<<"$LOC")
REDIR=$(awk '{print $2}' <<<"$LOC")
if [[ "$CODE" != "302" && "$CODE" != "301" && "$CODE" != "307" && "$CODE" != "308" ]]; then
  echo "expected redirect from / got HTTP $CODE" >&2
  exit 1
fi
if [[ "$REDIR" != *"/app"* ]]; then
  echo "expected Location containing /app, got: $REDIR" >&2
  exit 1
fi
echo "GET / → $CODE $REDIR"

curl -fsS "$BASE/app/" -o /tmp/lt-app.html
grep -Eiq 'LatentTone|root|vite|manifest' /tmp/lt-app.html
echo "GET /app/ ok"

curl -fsS "$BASE/browse" -o /tmp/lt-browse.html
grep -qi "LatentTone" /tmp/lt-browse.html
echo "GET /browse ok"

CFG=$(curl -fsS "$BASE/api/v1/config")
python3 -c 'import json,sys; d=json.load(sys.stdin); assert "public_base_url" in d and d["public_base_url"], d' <<<"$CFG"
echo "GET /api/v1/config ok ($CFG)"

echo "spa_smoke ok"
