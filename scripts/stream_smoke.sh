#!/usr/bin/env bash
# Gate B: auth → session → progressive stream (and HLS if ready) → feedback → next track.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
export DATA_DIR="${DATA_DIR:-$(mktemp -d /tmp/latenttone-stream-XXXX)}"
export BROWSE_PORT="${BROWSE_PORT:-18081}"
BASE="http://127.0.0.1:${BROWSE_PORT}"
chmod 755 "$DATA_DIR" 2>/dev/null || true

cleanup() {
  docker compose --profile stream-smoke down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "MUSIC_LIBRARY=$MUSIC_LIBRARY (ro)"
echo "DATA_DIR=$DATA_DIR"

docker compose build browse
docker compose --profile scan run --rm scan
docker compose --profile stream-smoke up -d browse-stream

for i in $(seq 1 30); do
  if curl -fsS "$BASE/" >/dev/null 2>&1; then
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

# Progressive fallback
BYTES=$(curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/tracks/${NOW}/stream" | wc -c)
if [[ "$BYTES" -lt 100 ]]; then
  echo "progressive stream too small: $BYTES" >&2
  exit 1
fi
echo "progressive bytes=$BYTES"

# HLS playlist (best-effort; may still be generating)
HLS_CODE=$(curl -sS -o /tmp/lt-hls.m3u8 -w '%{http_code}' \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/sessions/${SID}/hls/index.m3u8" || true)
echo "hls http=$HLS_CODE"

FB=$(curl -fsS -X POST "$BASE/api/v1/sessions/${SID}/feedback" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"signal\":\"skip\",\"track_id\":$NOW}")
NEXT=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["now_playing"]["track_id"])' <<<"$FB")
if [[ -z "$NEXT" || "$NEXT" == "$NOW" ]]; then
  echo "skip did not advance: $FB" >&2
  exit 1
fi
echo "after skip now=$NEXT"

curl -fsS -X DELETE -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/sessions/${SID}" >/dev/null

echo "stream_smoke ok"
