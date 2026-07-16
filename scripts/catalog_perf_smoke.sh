#!/usr/bin/env bash
# Catalog browse performance regression against a running browse instance.
# Expects BROWSE_PORT (default 18080). Optional DATA_DIR verifies performance indexes.
# Does not start or stop Compose.
set -euo pipefail

BROWSE_PORT="${BROWSE_PORT:-18080}"
BASE="http://127.0.0.1:${BROWSE_PORT}"

# Budgets are generous for cold SQLite + host variance; without idx_tracks_album_missing
# /artists was ~10s. Override via env if needed.
ARTISTS_MAX_S="${ARTISTS_MAX_S:-2.0}"
ALBUMS_MAX_S="${ALBUMS_MAX_S:-1.0}"
TRACKS_MAX_S="${TRACKS_MAX_S:-2.0}"
YEARS_MAX_S="${YEARS_MAX_S:-1.0}"

echo "catalog_perf_smoke against $BASE (artists<=${ARTISTS_MAX_S}s albums<=${ALBUMS_MAX_S}s tracks<=${TRACKS_MAX_S}s)"

check_json_ok() {
  local path="$1"
  local expect_key="$2"
  local max_s="$3"
  local out meta code time
  out="$(mktemp)"
  meta=$(curl -sS -o "$out" -w '%{http_code} %{time_total}' "$BASE$path")
  code=$(awk '{print $1}' <<<"$meta")
  time=$(awk '{print $2}' <<<"$meta")
  if [[ "$code" != "200" ]]; then
    echo "FAIL $path HTTP $code" >&2
    rm -f "$out"
    exit 1
  fi
  PATH_ARG="$path" KEY_ARG="$expect_key" MAX_ARG="$max_s" TIME_ARG="$time" OUT_ARG="$out" python3 - <<'PY'
import json, os, sys
path = os.environ["PATH_ARG"]
key = os.environ["KEY_ARG"]
max_s = float(os.environ["MAX_ARG"])
elapsed = float(os.environ["TIME_ARG"])
with open(os.environ["OUT_ARG"]) as f:
    d = json.load(f)
if key not in d:
    raise SystemExit(f"FAIL {path}: missing key {key!r} in {list(d)[:8]}")
print(f"GET {path} ok time={elapsed:.3f}s (max {max_s}s)")
if elapsed > max_s:
    raise SystemExit(f"FAIL latency {path}: {elapsed:.3f}s > {max_s}s")
PY
  rm -f "$out"
}

# Warm once so the budget is not dominated by cold page cache alone, then measure.
curl -fsS "$BASE/api/v1/catalog" >/dev/null
curl -fsS "$BASE/api/v1/catalog/artists" >/dev/null

check_json_ok "/api/v1/catalog" "tracks" "1.0"
check_json_ok "/api/v1/catalog/artists" "artists" "$ARTISTS_MAX_S"
check_json_ok "/api/v1/catalog/albums?limit=500" "albums" "$ALBUMS_MAX_S"
check_json_ok "/api/v1/catalog/tracks?limit=200" "tracks" "$TRACKS_MAX_S"
check_json_ok "/api/v1/catalog/years" "years" "$YEARS_MAX_S"

if [[ -n "${DATA_DIR:-}" && -f "${DATA_DIR}/latenttone.db" ]]; then
  echo "verifying performance indexes in $DATA_DIR/latenttone.db"
  docker run --rm -v "$DATA_DIR:/data" --entrypoint python3 latenttone:dev -c '
import sqlite3
con=sqlite3.connect("/data/latenttone.db")
names={r[0] for r in con.execute("select name from sqlite_master where type=\"index\"")}
need={"idx_tracks_album_missing","idx_playback_events_track"}
missing=sorted(need-names)
if missing:
    raise SystemExit("missing indexes: "+", ".join(missing))
print("indexes ok:", ", ".join(sorted(need)))
'
fi

echo "catalog_perf_smoke ok"
