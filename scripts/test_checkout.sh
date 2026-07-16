#!/usr/bin/env bash
# Developer test-checkout regression harness.
# Suites: fast | browse | stream | full (default: browse)
#
#   ./scripts/test_checkout.sh [--ref REF] [--suite SUITE] [--keep-worktree]
#
# Optional --ref creates a disposable git worktree, runs suites there, then removes it
# (unless --keep-worktree). Isolation: temp DATA_DIR, distinct ports, COMPOSE_PROJECT_NAME.
set -euo pipefail

ORIG_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SUITE="browse"
REF=""
KEEP_WORKTREE=0
WORKTREE=""
RUN_ROOT="$ORIG_ROOT"

usage() {
  cat <<'EOF'
Usage: ./scripts/test_checkout.sh [--ref REF] [--suite SUITE] [--keep-worktree]

Suites:
  fast    go test internal/db + internal/web, then go test ./...
  browse  fast + scripts/smoke.sh (scan, SPA/operator curls, catalog_perf_smoke)
  stream  browse + scripts/stream_smoke.sh (Gate B incl. skip ×2)
  full    stream + embed profile + neighbor playlist API sanity

Environment:
  MUSIC_LIBRARY   default /mnt2/media/music (mounted :ro by Compose)
  DATA_DIR        optional shared override (otherwise per-step temp dirs)
  ARTISTS_MAX_S / ALBUMS_MAX_S / TRACKS_MAX_S / YEARS_MAX_S
                  optional catalog_perf_smoke latency budgets (seconds)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --suite)
      SUITE="${2:?}"
      shift 2
      ;;
    --ref)
      REF="${2:?}"
      shift 2
      ;;
    --keep-worktree)
      KEEP_WORKTREE=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$SUITE" in
  fast|browse|stream|full) ;;
  *)
    echo "invalid suite: $SUITE (want fast|browse|stream|full)" >&2
    exit 2
    ;;
esac

step() {
  local name="$1"
  shift
  echo ""
  echo "=== [$SUITE] $name ==="
  if "$@"; then
    echo "OK  $name"
  else
    local rc=$?
    echo "FAIL $name (exit $rc)" >&2
    exit "$rc"
  fi
}

run_go_test_pkgs() {
  local pkgs=("$@")
  if command -v go >/dev/null 2>&1; then
    (cd "$RUN_ROOT" && go test -mod=vendor -count=1 "${pkgs[@]}")
  else
    echo "host go not found; using docker golang:1.22-bookworm"
    docker run --rm -v "$RUN_ROOT:/src" -w /src golang:1.22-bookworm \
      go test -mod=vendor -buildvcs=false -count=1 "${pkgs[@]}"
  fi
}

run_go_test_db_web() {
  run_go_test_pkgs ./internal/db/... ./internal/web/...
}

run_go_test() {
  run_go_test_pkgs ./...
}

run_smoke() {
  (
    cd "$RUN_ROOT"
    export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
    export BROWSE_PORT="${BROWSE_PORT:-18080}"
    export COMPOSE_PROJECT_NAME="lt-checkout-browse-$$"
    if [[ -z "${DATA_DIR:-}" ]]; then
      DATA_DIR="$(mktemp -d /tmp/latenttone-checkout-XXXX)"
    fi
    chmod 755 "$DATA_DIR" 2>/dev/null || true
    export DATA_DIR
    echo "DATA_DIR=$DATA_DIR"
    bash ./scripts/smoke.sh
  )
}

run_stream() {
  (
    cd "$RUN_ROOT"
    export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
    export BROWSE_PORT="${STREAM_BROWSE_PORT:-18081}"
    export COMPOSE_PROJECT_NAME="lt-checkout-stream-$$"
    # Always isolate stream DATA_DIR from browse step (fresh scan inside stream_smoke).
    DATA_DIR="$(mktemp -d /tmp/latenttone-checkout-stream-XXXX)"
    chmod 755 "$DATA_DIR" 2>/dev/null || true
    export DATA_DIR
    echo "DATA_DIR=$DATA_DIR"
    bash ./scripts/stream_smoke.sh
  )
}

run_embed_neighbor() {
  local data_dir port project base seed token reg pid
  data_dir="$(mktemp -d /tmp/latenttone-checkout-embed-XXXX)"
  port="${EMBED_BROWSE_PORT:-18082}"
  project="lt-checkout-embed-$$"
  base="http://127.0.0.1:${port}"

  (
    cd "$RUN_ROOT"
    export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
    export DATA_DIR="$data_dir"
    export BROWSE_PORT="$port"
    export COMPOSE_PROJECT_NAME="$project"
    chmod 755 "$DATA_DIR" 2>/dev/null || true

    cleanup_embed() {
      docker compose --profile embed --profile scan down --remove-orphans >/dev/null 2>&1 || true
      docker compose down --remove-orphans >/dev/null 2>&1 || true
    }
    trap cleanup_embed EXIT

    echo "embed DATA_DIR=$DATA_DIR BROWSE_PORT=$BROWSE_PORT"
    docker compose build browse
    docker compose --profile scan run --rm scan
    docker compose --profile embed run --rm embed
    docker compose up -d browse

    for i in $(seq 1 30); do
      if curl -fsS "$base/app/" >/dev/null 2>&1; then
        break
      fi
      sleep 1
    done

    seed=$(docker run --rm -v "$DATA_DIR:/data" --entrypoint python3 latenttone:dev -c \
      "import sqlite3; print(sqlite3.connect('/data/latenttone.db').execute('select id from tracks where missing_at is null order by id limit 1').fetchone()[0])")
    if [[ -z "$seed" || "$seed" == "0" ]]; then
      echo "no seed track after embed" >&2
      exit 1
    fi
    echo "neighbor seed_track_id=$seed"

    # Neighbor generate (operator playlist API; needs vectors from embed)
    PL=$(curl -fsS -X POST "$base/api/v1/playlists" \
      -H 'Content-Type: application/json' \
      -d "{\"seed_track_id\":$seed,\"length\":8}")
    pid=$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id") or "")' <<<"$PL")
    if [[ -z "$pid" ]]; then
      echo "neighbor playlist missing id: $PL" >&2
      exit 1
    fi
    echo "neighbor playlist id=$pid"

    # Authenticated promote from neighbor (Phase 3C)
    reg=$(curl -fsS -X POST "$base/api/v1/auth/register" \
      -H 'Content-Type: application/json' \
      -d "{\"username\":\"embed$(date +%s)\",\"password\":\"smokepass12\"}")
    token=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))' <<<"$reg")
    if [[ -z "$token" ]]; then
      echo "register failed for from-neighbor: $reg" >&2
      exit 1
    fi
    FN=$(curl -fsS -X POST "$base/api/v1/me/playlists/from-neighbor" \
      -H "Authorization: Bearer $token" \
      -H 'Content-Type: application/json' \
      -d "{\"playlist_id\":$pid,\"name\":\"Checkout Neighbor\"}")
    python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("id"), d' <<<"$FN"
    echo "from-neighbor ok"
  )
}

cleanup_worktree() {
  if [[ -n "$WORKTREE" && "$KEEP_WORKTREE" -eq 0 ]]; then
    echo "removing worktree $WORKTREE"
    git -C "$ORIG_ROOT" worktree remove --force "$WORKTREE" >/dev/null 2>&1 || rm -rf "$WORKTREE"
  elif [[ -n "$WORKTREE" && "$KEEP_WORKTREE" -eq 1 ]]; then
    echo "keeping worktree $WORKTREE"
  fi
}
trap cleanup_worktree EXIT

if [[ -n "$REF" ]]; then
  slug=$(echo "$REF" | tr '/:' '--' | tr -cd 'A-Za-z0-9._-' | cut -c1-48)
  WORKTREE="${TMPDIR:-/tmp}/latenttone-checkout-${slug}-$$"
  echo "creating worktree at $WORKTREE (ref=$REF)"
  git -C "$ORIG_ROOT" worktree add --detach "$WORKTREE" "$REF"
  RUN_ROOT="$WORKTREE"
fi

export MUSIC_LIBRARY="${MUSIC_LIBRARY:-/mnt2/media/music}"
echo "test_checkout suite=$SUITE root=$RUN_ROOT music=$MUSIC_LIBRARY"

step "go test internal/db + internal/web" run_go_test_db_web
step "go test ./..." run_go_test

if [[ "$SUITE" == "fast" ]]; then
  echo ""
  echo "test_checkout ok (suite=fast)"
  exit 0
fi

# smoke.sh includes spa_smoke + catalog_perf_smoke (latency + index checks).
step "compose browse smoke (+ catalog perf)" run_smoke

if [[ "$SUITE" == "browse" ]]; then
  echo ""
  echo "test_checkout ok (suite=browse)"
  exit 0
fi

step "compose stream smoke (Gate B + skip)" run_stream

if [[ "$SUITE" == "stream" ]]; then
  echo ""
  echo "test_checkout ok (suite=stream)"
  exit 0
fi

step "embed + neighbor sanity" run_embed_neighbor

echo ""
echo "test_checkout ok (suite=full)"
