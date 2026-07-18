#!/usr/bin/env bash
# Copyright (C) 2026 martinsah
# SPDX-License-Identifier: GPL-3.0-only
# Author: martinsah
# Date: 2026-07-15
#
# Gate B: auth → session → progressive stream → skip (×2) with post-skip stream
# bytes → transcode matrix (skip + stream under mp3/aac/opus prefs, then back to
# original) → teardown.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
export DATA_DIR="${DATA_DIR:-$(mktemp -d /tmp/latenttone-stream-XXXX)}"
export MARIADB_DATA="${MARIADB_DATA:-$DATA_DIR/mariadb}"
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

# Unauthenticated product route must 403 (401 would re-trigger proxy Basic Auth)
CODE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/sessions" \
  -H 'Content-Type: application/json' -d '{"seed_track_id":1}')
if [[ "$CODE" != "403" ]]; then
  echo "expected 403 for unauth session, got $CODE" >&2
  exit 1
fi

# Pick a seed track once the catalog UI is up (prefer scraping /tracks).
SEED=$(curl -fsS "$BASE/tracks" | grep -oE '/tracks/[0-9]+' | head -1 | grep -oE '[0-9]+' || true)
if [[ -z "$SEED" ]]; then
  SEED=$(bash "$ROOT/scripts/mariadb_exec.sh" "SELECT id FROM tracks WHERE missing_at IS NULL LIMIT 1")
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

# --- Transcode matrix: skip + stream under each explicit format pref ---------
# Exercises the UI path Settings → stream prefs → skip → progressive stream →
# session-status badge fields, per format (one bitrate; bitrates share a path).

set_stream_format() {
  local fmt="$1"
  local resp got
  resp=$(curl -fsS -X PATCH "$BASE/api/v1/me/stream-prefs" \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"stream_format\":\"$fmt\",\"bitrate_kbps\":192}")
  got=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["stream_format"])' <<<"$resp")
  if [[ "$got" != "$fmt" ]]; then
    echo "stream-prefs patch failed for $fmt: $resp" >&2
    exit 1
  fi
  echo "stream-prefs format=$fmt bitrate=192"
}

# Session status must advertise the effective codec/transcoding flag the
# floating-player badge renders (stream_codec / stream_transcoding).
check_status_stream() {
  local expect_codec="$1"
  local label="$2"
  local st
  st=$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/sessions/${SID}")
  python3 - "$expect_codec" "$label" "$st" <<'PY'
import json, sys
expect, label, raw = sys.argv[1], sys.argv[2], sys.argv[3]
st = json.loads(raw)
now = (st.get("now_playing") or {}).get("track_id")
codec = st.get("stream_codec") or ""
if not codec:
    sys.exit(f"{label}: session status missing stream_codec")
if st.get("stream_track_id") != now:
    sys.exit(f"{label}: stream_track_id={st.get('stream_track_id')} != now_playing={now}")
if expect and codec != expect:
    sys.exit(f"{label}: stream_codec={codec}, want {expect}")
if expect and not st.get("stream_transcoding"):
    sys.exit(f"{label}: stream_transcoding should be true for {expect}")
print(f"{label} status codec={codec} transcoding={st.get('stream_transcoding')}")
PY
}

# Transcoded streams are unbounded FFmpeg pipes; read a bounded prefix and
# verify the Content-Type matches the requested encode target.
fetch_transcoded() {
  local tid="$1"
  local want_ctype="$2"
  local label="$3"
  local hdrs bytes ctype
  hdrs=$(mktemp /tmp/lt-stream-hdrs.XXXX)
  # head truncates the endless pipe at 64 KB; curl's resulting write error (23)
  # is expected, so run it silently.
  bytes=$( (curl -s -D "$hdrs" -H "Authorization: Bearer $TOKEN" \
    "$BASE/api/v1/tracks/${tid}/stream" || true) | head -c 65536 | wc -c)
  ctype=$(awk 'tolower($1)=="content-type:"{print tolower($2)}' "$hdrs" | tr -d '\r' | head -1)
  rm -f "$hdrs"
  if [[ "$bytes" -lt 4096 ]]; then
    echo "$label transcoded stream too small: $bytes bytes (track=$tid)" >&2
    exit 1
  fi
  if [[ "$ctype" != "$want_ctype" ]]; then
    echo "$label content-type=$ctype, want $want_ctype (track=$tid)" >&2
    exit 1
  fi
  echo "$label transcoded bytes=$bytes ctype=$ctype track=$tid"
}

CUR="$NEXT2"
for FMT in mp3 aac opus; do
  case "$FMT" in
    mp3)  CTYPE="audio/mpeg" ;;
    aac)  CTYPE="audio/aac" ;;
    opus) CTYPE="audio/ogg" ;;
  esac
  set_stream_format "$FMT"
  CUR=$(skip_advance "$CUR" "skip-$FMT")
  check_status_stream "$FMT" "status-$FMT"
  fetch_transcoded "$CUR" "$CTYPE" "stream-$FMT"
done

# HLS packaging must work under transcode prefs (regression: embedded cover
# art used to feed libx264 and kill FFmpeg — playlist never appeared).
HLS_OK=""
for i in 1 2 3; do
  HLS_CODE=$(curl -sS -o /tmp/lt-hls-transcode.m3u8 -w '%{http_code}' \
    -H "Authorization: Bearer $TOKEN" \
    "$BASE/api/v1/sessions/${SID}/hls/index.m3u8" || true)
  if [[ "$HLS_CODE" == "200" ]] && grep -q '^#EXTM3U' /tmp/lt-hls-transcode.m3u8; then
    HLS_OK=1
    break
  fi
  sleep 2
done
if [[ -z "$HLS_OK" ]]; then
  echo "HLS playlist unavailable under transcode prefs (last http=$HLS_CODE)" >&2
  exit 1
fi
echo "hls-under-transcode ok (track=$CUR)"

# Back to original: skip once more, stream must serve plain file bytes again.
set_stream_format "original"
CUR=$(skip_advance "$CUR" "skip-original")
check_status_stream "" "status-original"
fetch_progressive "$CUR" "after-original"

curl -fsS -X DELETE -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/v1/sessions/${SID}" >/dev/null

echo "stream_smoke ok"
