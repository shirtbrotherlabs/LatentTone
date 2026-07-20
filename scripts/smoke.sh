#!/usr/bin/env bash
# Compose smoke: build, one-shot scan, assert DB has rows, check SPA + operator browse UI.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export MUSIC_LIBRARY="${MUSIC_LIBRARY:?set MUSIC_LIBRARY to a host music library path (mounted :ro)}"
export DATA_DIR="${DATA_DIR:-$(mktemp -d /tmp/latenttone-smoke-XXXX)}"
export MARIADB_DATA="${MARIADB_DATA:-$DATA_DIR/mariadb}"
export BROWSE_PORT="${BROWSE_PORT:-18080}"
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-lt-smoke-$$}"
chmod 755 "$DATA_DIR" 2>/dev/null || true

cleanup() {
  docker compose --profile scan down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "MUSIC_LIBRARY=$MUSIC_LIBRARY (ro)"
echo "DATA_DIR=$DATA_DIR"
echo "COMPOSE_PROJECT_NAME=$COMPOSE_PROJECT_NAME"

docker compose build
docker compose --profile scan run --rm scan

COUNT=$(bash "$ROOT/scripts/mariadb_exec.sh" "SELECT COUNT(*) FROM tracks WHERE missing_at IS NULL")
echo "tracks in catalog: $COUNT"
if [[ "$COUNT" -lt 1 ]]; then
  echo "expected at least one track" >&2
  exit 1
fi

docker compose up -d browse
for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS "http://127.0.0.1:${BROWSE_PORT}/app/" >/tmp/lt-app.html 2>/dev/null; then
    break
  fi
  sleep 2
done
if [[ ! -s /tmp/lt-app.html ]]; then
  echo "browse did not become ready on :${BROWSE_PORT}" >&2
  exit 1
fi

bash "$ROOT/scripts/spa_smoke.sh"
bash "$ROOT/scripts/catalog_perf_smoke.sh"

curl -fsS "http://127.0.0.1:${BROWSE_PORT}/browse" -o /tmp/lt-browse.html
grep -qi "LatentTone" /tmp/lt-browse.html
curl -fsS "http://127.0.0.1:${BROWSE_PORT}/artists" | grep -qi "Artists"
# Operator catalog pages must not ship a player; SPA under /app/ may.
if grep -Eiq '<audio|hls\.js|MediaSource' /tmp/lt-browse.html; then
  echo "playback markup found on /browse — operator catalog must stay browse-only" >&2
  exit 1
fi

echo "smoke ok"
