#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Checks every assembled result against the control repetitions with
# check_arm.py and prints the evidence summary: one block per run, then the
# numbers the enablement gate and the recommended TTL need. Exit 1 when any
# check failed.
# Usage: report.sh <results-root>
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
results="${1:?usage: report.sh <results-root>}"
status=0

controls=()
for dir in "$results"/C-rep*; do
  [[ -f "$dir/result.json" ]] && controls+=(--control "$dir/result.json")
done
if ((${#controls[@]} == 0)); then
  echo "report: no control (C) results under $results" >&2
  exit 2
fi

for dir in "$results"/*-rep*; do
  name="$(basename "$dir")"
  [[ -f "$dir/result.json" ]] || continue
  kind=treatment
  case "$name" in
    C-rep*) continue ;;
    C0-rep*) kind=control ;;
    T3-rep*) kind=t3 ;;
    T2-rep*) kind=t2 ;;
  esac
  echo "== $name ($kind)"
  python3 -I "$here/check_arm.py" --arm "$dir/result.json" --kind "$kind" "${controls[@]}" || status=1
done

echo "== numbers for the enablement gate (consult p50/p95 per run, K, parked rows)"
python3 -I - "$results" <<'PY'
import glob, json, os, sys
for path in sorted(glob.glob(os.path.join(sys.argv[1], "*-rep*", "result.json"))):
  r = json.load(open(path))
  consult = r.get("consult", {})
  a = consult.get("A", {})
  print("%-10s ttl=%-4s consult(A) p50=%.1fms p95=%.1fms n=%d | K_peak=%s parked_peak=%s woken_holding=%s wait_rows=%s" % (
      os.path.basename(os.path.dirname(path)), r.get("ttl"), 1000 * a.get("p50_s", 0), 1000 * a.get("p95_s", 0),
      a.get("count", 0), r["report"]["k_peak"], r["report"]["parked_rows_peak"], r["report"]["woken_holding_wait"],
      r["report"]["wait_table_rows_final"]))
PY
exit "$status"
