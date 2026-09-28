#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Generation retention timing for #7127 PR-3d (arbiter rulings arb-7127-3d P8,
# arb-7127-3d-b PB4/PB7, arb-7127-3d-c PC1/PC2, arb-7127-3d-d PC2-D, arb-7127-3d-e PC2-E), through
# TestRetentionTimingP8 in go/internal/storage/postgres/freshness/links, on
# binaries built from this tree ("after") and from BASE_REF ("before"). A
# batch's transaction duration bounds how long it holds the scope and
# candidate generation rows FOR UPDATE. Needs bash 4 or later.
#
# Usage: 7127-ledger-retention-timing.sh ADMIN_DSN BASE_REF ROUNDS OUT_DIR PHASE ROW_LIMIT FIXTURE...
#   PHASE run:     rounds of one before-run and one after-run per fixture, first
#                  mover alternating, until ROUNDS valid rounds per fixture or
#                  2 x ROUNDS attempts (rule PD5)
#   PHASE drain:   one drain (batches until one prunes nothing) per binary and fixture
#   PHASE explain: EXPLAIN (ANALYZE, BUFFERS) of the shipped ledger delete, after binary;
#                  the whole JSON plan is kept in OUT_DIR/plan-FIXTURE.json
# Fixtures: empty, link (P8), big771, big1542 (PB4).
#
# Validity, rule PD of arbiter ruling arb-7127-3d-d:
#   PD1  GATE_FILE, when set, gets `docker ps` and `uptime` at start and end.
#        The host must be quiet: no other gate, build, test or lint runs.
#   PD2  every run waits (up to LOAD_WAIT_SECONDS, default 600) for the
#        1-minute load to fall below LOAD_THRESHOLD (default half the CPU
#        count), and records load1 at its start and at its end;
#   PD3  load1 is sampled every second during the run; load1_max is recorded;
#   PD4  a round (one before-run and one after-run of one fixture) is valid
#        only if all loads of both runs are below LOAD_THRESHOLD and the
#        before-run (the base binary's control) took at most CONTROL_MAX_MS
#        (default 2000; it is sized for these fixtures at BatchRowLimit
#        100,000). An invalid round is re-run;
#   PD5  at most 2 x ROUNDS attempts per fixture; fewer valid rounds means the
#        host could not be quieted, which is not a code result.
# Every run line carries round, round_valid, load1, load1_end, load1_max,
# cpus and threshold, and a run-mode line the cluster's WAL and checkpointer
# deltas around the prune (wal: wal_records, wal_fpi, wal_bytes,
# wal_buffers_full, lsn_bytes, ckpt_*; arbiter ruling arb-7127-3d-e). SKIP_BUILD=1 reuses templates built on the same server.
set -euo pipefail

dsn="$1"
base_ref="$2"
rounds="$3"
out="$4"
phase="$5"
row_limit="$6"
shift 6
fixtures=("$@")
repo="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
test_file="go/internal/storage/postgres/freshness/links/retention_timing_live_test.go"
mkdir -p "$out"

load1() { sysctl -n vm.loadavg 2>/dev/null | awk '{print $2}' || cut -d' ' -f1 /proc/loadavg; }
ncpu() { sysctl -n hw.ncpu 2>/dev/null || nproc; }
threshold="${LOAD_THRESHOLD:-$(awk -v c="$(ncpu)" 'BEGIN { print c / 2 }')}"
control_max_ms="${CONTROL_MAX_MS:-2000}"

declare_host() { # label
  if [ -n "${GATE_FILE:-}" ]; then
    {
      echo "== $1 $(date -u +%FT%TZ)"
      uptime
      docker ps --format '{{.Names}} {{.Status}}' 2>/dev/null || true
    } >>"$GATE_FILE"
  fi
}

(cd "$repo/go" && go test -c -o "$out/rt_after" ./internal/storage/postgres/freshness/links)
if [ ! -d "$out/base-wt" ]; then
  git -C "$repo" worktree add --detach "$out/base-wt" "$base_ref" >/dev/null 2>&1
fi
cp "$repo/$test_file" "$out/base-wt/$test_file"
(cd "$out/base-wt/go" && go test -c -o "$out/rt_before" ./internal/storage/postgres/freshness/links)

# wait_for_load waits, up to LOAD_WAIT_SECONDS, for load1 below the threshold.
wait_for_load() {
  local waited=0
  while [ "$waited" -lt "${LOAD_WAIT_SECONDS:-600}" ] &&
    awk -v l="$(load1)" -v t="$threshold" 'BEGIN { exit !(l >= t) }'; do
    sleep 15
    waited=$((waited + 15))
  done
}

# run prints the run's TIMING line as JSON with its loads (PD2, PD3).
run() { # binary label fixture mode round
  local load cpus load_end load_max pid log
  wait_for_load
  load="$(load1)"
  cpus="$(ncpu)"
  load_max="$load"
  log="$(mktemp "$out/run.XXXXXX")"
  ESHU_RETENTION_TIMING_DSN="$dsn" ESHU_RETENTION_TIMING_MODE="$4" ESHU_RETENTION_TIMING_FIXTURE="$3" \
    ESHU_RETENTION_TIMING_LABEL="$2" ESHU_RETENTION_TIMING_ROW_LIMIT="$row_limit" \
    "$1" -test.run '^TestRetentionTimingP8$' -test.count=1 >"$log" 2>&1 &
  pid=$!
  while kill -0 "$pid" 2>/dev/null; do
    load_max="$(awk -v a="$load_max" -v b="$(load1)" 'BEGIN { print (b > a) ? b : a }')"
    sleep 1
  done
  wait "$pid" || true
  load_end="$(load1)"
  load_max="$(awk -v a="$load_max" -v b="$load_end" 'BEGIN { print (b > a) ? b : a }')"
  sed -n "s/^TIMING {/{\"round\":$5,\"load1\":$load,\"load1_end\":$load_end,\"load1_max\":$load_max,\"cpus\":$cpus,\"threshold\":$threshold,/p" "$log"
  if [ "$4" = explain ]; then sed -n 's/^PLAN //p' "$log" >"$out/plan-$3.json"; fi
  rm -f "$log"
}

# round_valid reads two run lines (before, after) and prints true or false (PD4).
round_valid() {
  python3 -c '
import json, sys
t, cmax = float(sys.argv[1]), float(sys.argv[2])
runs = [json.loads(l) for l in sys.argv[3:] if l.strip()]
ok = len(runs) == 2 and all(max(r["load1"], r["load1_end"], r["load1_max"]) < t for r in runs)
before = [r for r in runs if r["label"] == "before"]
ok = ok and len(before) == 1 and before[0]["duration_ms"] <= cmax
print("true" if ok else "false")' "$threshold" "$control_max_ms" "$@"
}

list="$(IFS=,; echo "${fixtures[*]}")"
if [ -z "${SKIP_BUILD:-}" ]; then
  ESHU_RETENTION_TIMING_DSN="$dsn" ESHU_RETENTION_TIMING_MODE=build ESHU_RETENTION_TIMING_FIXTURES="$list" \
    "$out/rt_after" -test.run '^TestRetentionTimingP8$' -test.count=1 >"$out/build.log" 2>&1 ||
    { tail -20 "$out/build.log"; exit 1; }
fi
results="$out/$phase-results.jsonl"
: >"$results"
declare_host "start $phase threshold=$threshold control_max_ms=$control_max_ms"
if [ "$phase" = explain ]; then
  for fixture in "${fixtures[@]}"; do
    run "$out/rt_after" "after" "$fixture" explain 0 | tee -a "$results"
  done
elif [ "$phase" = drain ]; then
  for fixture in "${fixtures[@]}"; do
    for side in before after; do
      run "$out/rt_$side" "$side" "$fixture" drain 0 | tee -a "$results"
    done
  done
else
  declare -A valid attempts
  for fixture in "${fixtures[@]}"; do
    valid[$fixture]=0
    attempts[$fixture]=0
  done
  for attempt in $(seq 1 $((2 * rounds))); do
    pending=0
    for fixture in "${fixtures[@]}"; do
      [ "${valid[$fixture]}" -ge "$rounds" ] && continue
      pending=1
      attempts[$fixture]=$attempt
      if [ $((attempt % 2)) -eq 1 ]; then order="before after"; else order="after before"; fi
      lines=()
      for side in $order; do
        lines+=("$(run "$out/rt_$side" "$side" "$fixture" run "$attempt")")
      done
      ok="$(round_valid "${lines[@]}")"
      for line in "${lines[@]}"; do
        printf '%s\n' "{\"round_valid\":$ok,${line#\{}" | tee -a "$results"
      done
      if [ "$ok" = true ]; then valid[$fixture]=$((valid[$fixture] + 1)); fi
    done
    [ "$pending" -eq 0 ] && break
  done
  for fixture in "${fixtures[@]}"; do
    echo "fixture $fixture: ${valid[$fixture]} valid rounds in ${attempts[$fixture]} attempts" | tee -a "$out/rounds.txt"
  done
fi
declare_host "end $phase"
git -C "$repo" worktree remove --force "$out/base-wt"
echo "results: $results"
