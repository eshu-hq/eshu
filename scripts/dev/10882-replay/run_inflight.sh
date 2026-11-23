#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Entry point of the in-flight arms (decision (g) of the replay ruling): IFC (the
# control) and IF, three repetitions each, then the checker. A separate script so
# it can land beside a running matrix without editing run.sh. The product binaries
# must come from the matrix build (same commit); only the seeder is rebuilt, from
# this checkout, as replayseed-if.test (it adds ESHU_REPLAY_SKIP_MIX).
#
# Usage: run_inflight.sh --results DIR [--reps 3] [--build-seeder]
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../../.." && pwd)"
# shellcheck source=lib/env.sh
source "$here/lib/env.sh"
for lib in host proc sampler collect hosted arm inflight; do
  # shellcheck source=/dev/null
  source "$here/lib/$lib.sh"
done

results="" reps=3 build_seeder=0
while (($#)); do
  case "$1" in
    --results) results="$2"; shift 2 ;;
    --reps) reps="$2"; shift 2 ;;
    --build-seeder) build_seeder=1; shift ;;
    *) echo "unknown argument $1" >&2; exit 2 ;;
  esac
done
[[ -n "$results" && -f "$results/build.commit" ]] || { echo "--results must be a matrix results dir with build.commit" >&2; exit 2; }

export REPLAY_REPO_ROOT="$repo_root" REPLAY_RESULTS="$results" REPLAY_FIXTURES="$REPLAY_HOME/fixtures"
export REPLAY_SEEDER="$REPLAY_BIN/replayseed.test" REPLAY_SEEDER_IF="$REPLAY_BIN/replayseed-if.test"
if ((build_seeder)); then
  (cd "$repo_root/go" && nice -n 5 go test -c -tags replayseed -o "$REPLAY_SEEDER_IF" ./internal/storage/postgres)
fi
started="$(date +%s)"
replay_require_venue
for rep in $(seq 1 "$reps"); do
  for arm in IFC IF; do
    replay_check_wall_cap "$started" || break 2
    replay_wait_for_load "$started" || break 2
    replay_run_inflight "$arm" "$rep"
  done
done

status=0
judge=()
[[ "${REPLAY_JUDGE:-active}" == effective ]] && judge=(--judge-effective)
mapfile -t refs < <(for d in "$results"/IFC-rep*; do [[ -f "$d/result.json" ]] && printf -- '--control\n%s\n' "$d/result.json"; done)
echo "== control set IFC"
python3 -I "$here/check_arm.py" ${judge[@]+"${judge[@]}"} --kind controlset "${refs[@]}" || status=1
for d in "$results"/IF-rep*; do
  [[ -f "$d/result.json" ]] || continue
  echo "== $(basename "$d") (inflight)"
  python3 -I "$here/check_arm.py" ${judge[@]+"${judge[@]}"} --arm "$d/result.json" --kind inflight "${refs[@]}" || status=1
done
exit "$status"
