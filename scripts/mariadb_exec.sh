#!/usr/bin/env bash
# Run SQL against the Compose mariadb service (catalog DB).
# Ensures the service is up and healthy before querying.
#
# Usage:
#   ./scripts/mariadb_exec.sh "SELECT COUNT(*) FROM tracks"
#   ./scripts/mariadb_exec.sh wait
#   ./scripts/mariadb_exec.sh indexes   # catalog perf smoke indexes
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

: "${MARIADB_PASSWORD:?set MARIADB_PASSWORD in .env or environment}"

wait_healthy() {
  docker compose up -d mariadb >/dev/null
  for _ in $(seq 1 90); do
    if docker compose exec -T mariadb healthcheck.sh --connect --innodb_initialized >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "mariadb service not healthy" >&2
  return 1
}

query() {
  docker compose exec -T mariadb mariadb \
    -ulatenttone \
    -p"${MARIADB_PASSWORD}" \
    latenttone \
    -Nse "$1"
}

verify_perf_indexes() {
  local found missing
  found=$(query "SELECT DISTINCT index_name FROM information_schema.statistics WHERE table_schema='latenttone' AND index_name IN ('idx_tracks_album_missing','idx_playback_events_track') ORDER BY 1")
  missing=""
  for need in idx_tracks_album_missing idx_playback_events_track; do
    if ! grep -qx "$need" <<<"$found"; then
      missing="${missing:+$missing, }$need"
    fi
  done
  if [[ -n "$missing" ]]; then
    echo "missing indexes: $missing" >&2
    exit 1
  fi
  echo "indexes ok: idx_playback_events_track, idx_tracks_album_missing"
}

cmd="${1:-}"
case "$cmd" in
  wait)
    wait_healthy
    ;;
  indexes)
    wait_healthy
    verify_perf_indexes
    ;;
  "")
    echo "usage: $(basename "$0") wait|indexes|<sql>" >&2
    exit 2
    ;;
  *)
    wait_healthy
    query "$cmd"
    ;;
esac
