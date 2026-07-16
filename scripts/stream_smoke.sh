#!/usr/bin/env bash
# Gate B: auth → session → progressive stream → skip (×2) with post-skip stream bytes → teardown.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
export DATA_DIR="${DATA_DIR:-$(mktemp -d /tmp/latenttone-stream-XXXX)}"
export BROWSE_PORT="${BROWSE_PORT:-18081}"
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-lt-stream-$$}"
BASE="http://127.0.0.1:${BROWSE_PORT}"
chmod 755 "$DATA_DIR" 2>/dev/null || true

cleanup() {
  docker compose --profile stream-smoke down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "MUSIC_LIBRARY=$MUSIC_LIBRARY (ro)"
echo "DATA_DIR=$DATA_DIR"
echo "COMPOSE_PROJECT_NAME=$COMPOSE_PROJECT_NAME"

docker compose build browse
docker compose --profile scan run --rm scan
docker compose --profile stream-smoke up -d browse-stream

for i in $(seq 1 30); do
  if curl -fsS "$BASE/app/" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

USER="smoke$(date +%s)"
PASS="smokepass12"
REG=$(curl -fsS -X POST "$BASE/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USER\",\"password\":\"$PASS\"}")
TOKEN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])' <<<"$REG")
if [[ -z "$TOKEN" ]]; then
  echo "register failed: $REG" >&2
  exit 1
fi

# Unauthenticated product route must 401
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/sessions" \
  -H 'Content-Type: application/json' -d '{"seed_track_id":1}')
if [[ "$CODE" != "401" ]]; then
  echo "expected 401 for unauth session, got $CODE" >&2
  exit 1
fi

# Pick a seed track once the catalog UI is up (prefer scraping /tracks).
SEED=$(curl -fsS "$BASE/tracks" | grep -oE '/tracks/[0-9]+' | head -1 | grep -oE '[0-9]+' || true)
if [[ -z "$SEED" ]]; then
  SEED=$(docker compose --profile stream-smoke exec -T browse-stream \
    python3 -c "import sqlite3; print(sqlite3.connect('/data/latenttone.db').execute('select id from tracks where missing_at is null limit 1').fetchone()[0])")
fi
if [[ -z "$SEED" || "$SEED" == "0" ]]; then
  echo "no seed track in catalog" >&2
  exit 1
fi

SESS=$(curl -fsS -X POST "$BASE/api/v1/sessions" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"seed_track_id\":$SEED}")
SID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$SESS")
NOW=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["now_playing"]["track_id"])' <<<"$SESS")
if [[ -z "$SID" || -z "$NOW" ]]; then
  echo "create session failed: $SESS" >&2
  exit 1
fi
echo "session=$SID now=$NOW"

fetch_progressive() {
  local tid="$1"
  local label="$2"
  local bytes
  bytes=$(curl -fsS -H "Authorization: Bearer $TOKEN" \
    "$BASE/api/v1/tracks/${tid}/stream" | wc -c)
  if [[ "$bytes" -lt 100 ]]; then
    echo "$label progressive stream too small: $bytes (track=$tid)" >&2
    exit 1
  fi
  echo "$label progressive bytes=$bytes track=$tid"
}

skip_advance() {
  local from="$1"
  local label="$2"
  local fb next
  fb=$(curl -fsS -X POST "$BASE/api/v1/sessions/${SID}/feedback" \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"signal\":\"skip\",\"track_id\":$from}")
  next=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["now_playing"]["track_id"])' <<<"$fb")
  if [[ -z "$next" || "$next" == "$from" ]]; then
    echo "$label skip did not advance: $fb" >&2
    exit 1
  fi
  echo "$label after skip now=$next" >&2
  printf '%s' "$next"
}

# Progressive for seed track
fetch_progressive "$NOW" "initial"

# HLS playlist (best-effort; may still be generating)
HLS_CODE=$(curl -sS -o /tmp/lt-hls.m3u8 -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/sessions/${SID}/hls/index.m3u8" || true)
echo "hls http=$HLS_CODE"

# Skip #1 + post-skip stream
NEXT=$(skip_advance "$NOW" "skip1")
fetch_progressive "$NEXT" "after-skip1"

# Skip #2 + post-skip stream
NEXT2=$(skip_advance "$NEXT" "skip2")
if [[ "$NEXT2" == "$NEXT" ]]; then
  echo "skip2 did not advance past $NEXT" >&2
  exit 1
fi
fetch_progressive "$NEXT2" "after-skip2"

curl -fsS -X DELETE -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/sessions/${SID}" >/dev/null

echo "stream_smoke ok"
