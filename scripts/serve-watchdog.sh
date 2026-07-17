#!/bin/sh
# Copyright (C) 2026 martinsah
# SPDX-License-Identifier: GPL-3.0-only
#
# Supervises `latenttone serve` inside the browse container. If the HTTP API
# stops responding (mutex wedge, LanceDB hang, etc.) the child is killed and
# restarted. One-shot subcommands (scan, embed, migrate-sqlite) bypass this.

set -eu

LATENTTONE=/usr/local/bin/latenttone
PROBE_URL="${LATENTTONE_HEALTH_URL:-http://127.0.0.1:8080/api/v1/config}"
PROBE_TIMEOUT="${LATENTTONE_HEALTH_TIMEOUT_SEC:-30}"
PROBE_INTERVAL="${LATENTTONE_HEALTH_INTERVAL_SEC:-60}"
START_PERIOD="${LATENTTONE_HEALTH_START_PERIOD_SEC:-90}"
FAIL_THRESHOLD="${LATENTTONE_HEALTH_FAIL_THRESHOLD:-1}"

case "${1:-serve}" in
  scan|embed|migrate-sqlite)
    exec "$LATENTTONE" "$@"
    ;;
esac

echo "latenttone-watchdog: probe=$PROBE_URL timeout=${PROBE_TIMEOUT}s interval=${PROBE_INTERVAL}s start_period=${START_PERIOD}s"

started_at=$(date +%s)
fail_streak=0

while true; do
  "$LATENTTONE" "$@" &
  pid=$!
  trap 'kill -TERM "$pid" 2>/dev/null; wait "$pid" 2>/dev/null; exit 143' TERM INT

  while kill -0 "$pid" 2>/dev/null; do
    sleep "$PROBE_INTERVAL"
    now=$(date +%s)
    if [ $((now - started_at)) -lt "$START_PERIOD" ]; then
      continue
    fi
    if curl -sf --max-time "$PROBE_TIMEOUT" "$PROBE_URL" >/dev/null; then
      fail_streak=0
      continue
    fi
    fail_streak=$((fail_streak + 1))
    echo "latenttone-watchdog: health probe failed ($fail_streak/$FAIL_THRESHOLD)" >&2
    if [ "$fail_streak" -ge "$FAIL_THRESHOLD" ]; then
      echo "latenttone-watchdog: killing pid $pid and restarting" >&2
      kill -TERM "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      fail_streak=0
      started_at=$(date +%s)
      break
    fi
  done

  if kill -0 "$pid" 2>/dev/null; then
    continue
  fi
  wait "$pid" 2>/dev/null || true
  echo "latenttone-watchdog: latenttone exited; restarting in 3s" >&2
  sleep 3
  started_at=$(date +%s)
done
