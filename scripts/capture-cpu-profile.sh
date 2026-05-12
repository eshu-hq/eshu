#!/usr/bin/env bash
# capture-cpu-profile.sh — event-driven dual-side CPU profile capture for
# an `eshu graph start` run.
#
# Waits for a log marker that signals "interesting phase begins" in the
# run log, sleeps briefly to let the workers ramp into steady state, then
# captures matched-wall-clock CPU profiles from the ingester pprof endpoint
# and the NornicDB pprof endpoint in parallel. After the CPU window closes,
# snapshots heap, allocs, and goroutine state on the NornicDB side, and a
# goroutine snapshot on the ingester side.
#
# Usage:
#   capture-cpu-profile.sh <RUN_DIR> <INGESTER_PPROF> <NORNICDB_PPROF>
#
#   RUN_DIR         directory containing run.log; profiles/ subdir is created
#                   if missing
#   INGESTER_PPROF  host:port for the ingester's pprof endpoint (e.g.
#                   127.0.0.1:53210 or :0-assigned port discovered from
#                   the run.log "pprof server listening" line)
#   NORNICDB_PPROF  host:port for the NornicDB pprof endpoint set via
#                   NORNICDB_PPROF_ENABLED=true NORNICDB_PPROF_LISTEN=...
#
# Tuning via env vars:
#   PPROF_LOG_MARKER   ripgrep pattern that signals "begin capturing"
#                      (default: 'canonical phase group completed.*phase=files',
#                      which fires when files phase ends and entities phase
#                      begins on the eshu canonical writer)
#   PPROF_SLEEP_S      seconds to sleep between the marker and starting
#                      the CPU profile (default: 5; use larger values if
#                      the workers take longer to reach steady state)
#   PPROF_CPU_S        CPU profile capture window in seconds (default: 20;
#                      the previous default of 60 outlived fast entities
#                      phases on the post-Path-D NornicDB binary)
#   PPROF_MARKER_TIMEOUT_S
#                      maximum seconds to wait for the marker before
#                      giving up (default: 1500 = 25 min)
#
# Outputs (in $RUN_DIR/profiles/):
#   watcher.log              human-readable timestamps for each step
#   ingester-cpu-${PPROF_CPU_S}s.pb.gz
#   ingester-goroutines.txt
#   nornicdb-cpu-${PPROF_CPU_S}s.pb.gz
#   nornicdb-heap.pb.gz
#   nornicdb-allocs.pb.gz
#   nornicdb-goroutines.txt
#
# Exit codes:
#   0 — capture completed (profiles may still be empty if the pprof endpoint
#       died mid-curl; check watcher.log byte counts)
#   1 — marker never appeared within PPROF_MARKER_TIMEOUT_S
#   2 — bad argument

set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <RUN_DIR> <INGESTER_PPROF> <NORNICDB_PPROF>" >&2
  exit 2
fi

RUN_DIR="$1"
INGESTER_PPROF="$2"
NORNICDB_PPROF="$3"

PPROF_LOG_MARKER="${PPROF_LOG_MARKER:-canonical phase group completed.*phase=files}"
PPROF_SLEEP_S="${PPROF_SLEEP_S:-5}"
PPROF_CPU_S="${PPROF_CPU_S:-20}"
PPROF_MARKER_TIMEOUT_S="${PPROF_MARKER_TIMEOUT_S:-1500}"

LOG="$RUN_DIR/run.log"
PROFILE_DIR="$RUN_DIR/profiles"
mkdir -p "$PROFILE_DIR"
WATCHER_LOG="$PROFILE_DIR/watcher.log"

ts() { date -u +%FT%TZ; }

{
  echo "[$(ts)] dual-side watcher started"
  echo "[$(ts)]   RUN_DIR=$RUN_DIR"
  echo "[$(ts)]   INGESTER_PPROF=$INGESTER_PPROF NORNICDB_PPROF=$NORNICDB_PPROF"
  echo "[$(ts)]   PPROF_LOG_MARKER='$PPROF_LOG_MARKER' PPROF_SLEEP_S=$PPROF_SLEEP_S PPROF_CPU_S=$PPROF_CPU_S"
} > "$WATCHER_LOG"

# Wait for the marker, with a hard timeout so the watcher cannot wedge
# forever if the eshu process dies before reaching the interesting phase.
ELAPSED=0
while [ "$ELAPSED" -lt "$PPROF_MARKER_TIMEOUT_S" ]; do
  if rg -q "$PPROF_LOG_MARKER" "$LOG" 2>/dev/null; then
    break
  fi
  sleep 5
  ELAPSED=$((ELAPSED + 5))
done

if [ "$ELAPSED" -ge "$PPROF_MARKER_TIMEOUT_S" ]; then
  echo "[$(ts)] TIMEOUT waiting for marker after ${PPROF_MARKER_TIMEOUT_S}s; aborting" >> "$WATCHER_LOG"
  exit 1
fi

echo "[$(ts)] marker matched; sleeping ${PPROF_SLEEP_S}s for workers to reach steady state" >> "$WATCHER_LOG"
sleep "$PPROF_SLEEP_S"

echo "[$(ts)] starting ${PPROF_CPU_S}s CPU profiles (parallel: ingester + nornicdb)" >> "$WATCHER_LOG"

INGESTER_CPU="$PROFILE_DIR/ingester-cpu-${PPROF_CPU_S}s.pb.gz"
NORNICDB_CPU="$PROFILE_DIR/nornicdb-cpu-${PPROF_CPU_S}s.pb.gz"

# curl exits non-zero when the pprof endpoint dies mid-capture (e.g. the
# eshu stack tore down before the seconds= window closed). We want to
# capture that rc in the watcher log rather than abort the script, so
# allow these specific commands to continue on failure under `set -e`.
curl -sS -o "$INGESTER_CPU" "http://${INGESTER_PPROF}/debug/pprof/profile?seconds=${PPROF_CPU_S}" &
ING_PID=$!
curl -sS -o "$NORNICDB_CPU" "http://${NORNICDB_PPROF}/debug/pprof/profile?seconds=${PPROF_CPU_S}" &
NDB_PID=$!

ING_RC=0
wait "$ING_PID" || ING_RC=$?
ING_BYTES=$(stat -f%z "$INGESTER_CPU" 2>/dev/null || stat -c%s "$INGESTER_CPU" 2>/dev/null || echo 0)
echo "[$(ts)] ingester CPU captured rc=$ING_RC bytes=$ING_BYTES" >> "$WATCHER_LOG"

NDB_RC=0
wait "$NDB_PID" || NDB_RC=$?
NDB_BYTES=$(stat -f%z "$NORNICDB_CPU" 2>/dev/null || stat -c%s "$NORNICDB_CPU" 2>/dev/null || echo 0)
echo "[$(ts)] nornicdb CPU captured rc=$NDB_RC bytes=$NDB_BYTES" >> "$WATCHER_LOG"

# Heap, allocs, goroutine snapshots after the CPU window closes. These are
# one-shot reads (no seconds= parameter) so they finish quickly. We tolerate
# curl failure here too — the eshu stack may have already torn down.
for kind in heap allocs; do
  curl -sS -o "$PROFILE_DIR/nornicdb-${kind}.pb.gz" "http://${NORNICDB_PPROF}/debug/pprof/${kind}" 2>/dev/null || true
  echo "[$(ts)] nornicdb $kind captured" >> "$WATCHER_LOG"
done

curl -sS -o "$PROFILE_DIR/nornicdb-goroutines.txt" "http://${NORNICDB_PPROF}/debug/pprof/goroutine?debug=2" 2>/dev/null || true
echo "[$(ts)] nornicdb goroutines captured" >> "$WATCHER_LOG"

curl -sS -o "$PROFILE_DIR/ingester-goroutines.txt" "http://${INGESTER_PPROF}/debug/pprof/goroutine?debug=2" 2>/dev/null || true
echo "[$(ts)] ingester goroutines captured" >> "$WATCHER_LOG"

echo "[$(ts)] watcher done" >> "$WATCHER_LOG"
