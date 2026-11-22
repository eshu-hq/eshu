#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Entry point of the #10882 built-binary replay: runs the selected arms, each for
# the given repetitions, from binaries built at one commit, against the replay-*
# PostgreSQL 18.6 and pinned Neo4j containers, then checks every treatment arm
# against the control and writes the evidence summary.
#
# Arms: C (control), C0 (control without the completion step), T1 (TTL 0),
# T1TTL (candidate TTL), T2 (hosted A), T3 (not-takeable A). Order: the control
# first, so every later arm has its reference.
#
# Usage: run.sh [--arms "C C0 T1 T1TTL T3"] [--reps 3] [--results DIR] [--no-build]
# Starts an arm only when load1 < 60; stops at the 4 hour wall cap. Never starts
# without the operator's explicit GO: this script is not run by CI or hooks.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../../.." && pwd)"
# shellcheck source=lib/env.sh
source "$here/lib/env.sh"
for lib in host proc sampler collect hosted arm; do
  # shellcheck source=/dev/null
  source "$here/lib/$lib.sh"
done

arms="C C0 T1 T1TTL T2 T3"
reps=3
results="$REPLAY_SCRATCH/results"
build=1
while (($#)); do
  case "$1" in
    --arms) arms="$2"; shift 2 ;;
    --reps) reps="$2"; shift 2 ;;
    --results) results="$2"; shift 2 ;;
    --no-build) build=0; shift ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done

export REPLAY_RESULTS="$results" REPLAY_FIXTURES="$REPLAY_HOME/fixtures" REPLAY_SEEDER="$REPLAY_BIN/replayseed.test"
mkdir -p "$results" "$REPLAY_SCRATCH"
started="$(date +%s)"
echo "$started" >"$results/replay.started"

replay_require_venue
if ((build)); then
  "$here/lib/build.sh" "$repo_root" "$results"
fi

for rep in $(seq 1 "$reps"); do
  for arm in $arms; do
    replay_check_wall_cap "$started" || break 2
    replay_wait_for_load "$started" || break 2
    replay_run_arm "$arm" "$rep"
  done
done

"$here/report.sh" "$results"
