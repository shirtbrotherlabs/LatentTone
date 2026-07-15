#!/usr/bin/env bash
# Compose smoke: build, one-shot scan, assert DB has rows, check browse UI (no playback).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
export DATA_DIR="${DATA_DIR:-$(mktemp -d /tmp/latenttone-smoke-XXXX)}"
export BROWSE_PORT="${BROWSE_PORT:-18080}"

cleanup() {
  docker compose --profile scan down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "MUSIC_LIBRARY=$MUSIC_LIBRARY (ro)"
echo "DATA_DIR=$DATA_DIR"

docker compose build
docker compose --profile scan run --rm scan

COUNT=$(docker run --rm -v "$DATA_DIR:/data:ro" keinos/sqlite3 \
  sqlite3 /data/latenttone.db "SELECT COUNT(*) FROM tracks WHERE missing_at IS NULL;")
echo "tracks in catalog: $COUNT"
if [[ "$COUNT" -lt 1 ]]; then
  echo "expected at least one track" >&2
  exit 1
fi

docker compose up -d browse
for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS "http://127.0.0.1:${BROWSE_PORT}/" >/tmp/lt-home.html 2>/dev/null; then
    break
  fi
  sleep 2
done
grep -qi "LatentTone" /tmp/lt-home.html
curl -fsS "http://127.0.0.1:${BROWSE_PORT}/artists" | grep -qi "Artists"
if grep -Eiq '<audio|hls\.js|MediaSource' /tmp/lt-home.html; then
  echo "playback markup found — Phase 1 UI must be browse-only" >&2
  exit 1
fi

echo "smoke ok"
