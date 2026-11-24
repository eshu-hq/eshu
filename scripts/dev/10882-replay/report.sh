#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Checks every assembled result with check_arm.py and prints the evidence summary:
# the control sets (count and equality), one block per run against the right
# control (C for C0, T1, T1TTL, T3, T3TTL; C2 for T2 and T2TTL), the proof that two control
# runs differ only in the normalised payload fields, CG as a reported reading, and
# the numbers the enablement gate and the recommended TTL need. Exit 1 when any
# check failed; nothing here retunes a check.
# Usage: report.sh <results-root>
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export REPLAY_SCRIPT_DIR="$here"
results="${1:?usage: report.sh <results-root>}"
status=0
# Which final-state comparison is judged: REPLAY_JUDGE=active (default, the literal
# ruling) or effective (the chain comparison). Both are always printed.
judge=()
[[ "${REPLAY_JUDGE:-active}" == effective ]] && judge=(--judge-effective)

collect() { # <arm> -> --control args for every repetition with a result
  local dir
  for dir in "$results/$1"-rep*; do
    [[ -f "$dir/result.json" ]] && printf -- '--control\n%s\n' "$dir/result.json"
  done
}
mapfile -t controls < <(collect C)
mapfile -t controls2 < <(collect C2)
cf=()
[[ -f "$results/CF-rep1/result.json" ]] && cf=(--cf "$results/CF-rep1/result.json")
if ((${#controls[@]} == 0)); then
  echo "report: no control (C) results under $results" >&2
  exit 2
fi

echo "== control set C (count and equality of the repetitions)"
python3 -I "$here/check_arm.py" ${judge[@]+"${judge[@]}"} --kind controlset "${controls[@]}" || status=1
if ((${#controls2[@]} > 0)); then
  echo "== control set C2"
  python3 -I "$here/check_arm.py" ${judge[@]+"${judge[@]}"} --kind controlset "${controls2[@]}" || status=1
fi

echo "== run-variant fields: top-level payload keys that differ between control runs C-rep1 and each later C-rep"
python3 -I "$here/report_run_variant.py" "$results" || status=1

for dir in "$results"/*-rep*; do
  name="$(basename "$dir")"
  [[ -f "$dir/result.json" ]] || continue
  case "$name" in
    C-rep*|C2-rep*) continue ;;
    CF-rep*) kind=cfref; ref=("${controls[@]:0:2}") ;;
    C0-rep*) kind=control; ref=("${controls[@]}") ;;
    T3-rep*|T3TTL-rep*) kind=t3; ref=("${controls[@]}") ;;
    T2-rep*|T2TTL-rep*) kind=t2; ref=("${controls2[@]}") ;;
    CG-rep*) kind=cg; ref=() ;;
    *) kind=treatment; ref=("${controls[@]}") ;;
  esac
  echo "== $name ($kind)"
  python3 -I "$here/check_arm.py" ${judge[@]+"${judge[@]}"} --arm "$dir/result.json" --kind "$kind" ${cf[@]+"${cf[@]}"} ${ref[@]+"${ref[@]}"} || status=1
done

echo "== host load during the run (one sample a minute, loadavg.tsv)"
python3 -I - "$results" <<'PY'
import os, statistics, sys
path = os.path.join(sys.argv[1], "loadavg.tsv")
if os.path.exists(path):
  loads = [float(line.split("\t")[1]) for line in open(path) if "\t" in line]
  print("samples=%d min=%.1f median=%.1f max=%.1f" % (len(loads), min(loads), statistics.median(loads), max(loads)))
else:
  print("no loadavg.tsv")
PY

echo "== numbers (engaged-scrape consult p50/p95 and share within 100 ms, K, parked rows, superseded, A drain after catch-up)"
python3 -I "$here/report_numbers.py" "$results"
exit "$status"
