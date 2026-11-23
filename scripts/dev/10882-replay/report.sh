#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Checks every assembled result with check_arm.py and prints the evidence summary:
# the control sets (count and equality), one block per run against the right
# control (C for C0, T1, T1TTL, T3, T3TTL; C2 for T2), the proof that two control
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
python3 -I - "$results" <<'PY' || status=1
import glob, os, sys
sys.path.insert(0, os.environ.get("REPLAY_SCRIPT_DIR", "."))
import replay_facts
runs = sorted(glob.glob(os.path.join(sys.argv[1], "C-rep*", "export", "real_facts.tsv")))
want = sorted(replay_facts.RUN_VARIANT_PAYLOAD_FIELDS)
bad = False
for other in runs[1:]:
  keys = replay_facts.differing_top_level_keys(runs[0], other)
  ok = keys == want
  bad |= not ok
  print("%s %s vs %s: differing top-level payload keys = %s (must be exactly %s)" % (
      "PASS" if ok else "FAIL", os.path.basename(os.path.dirname(os.path.dirname(runs[0]))),
      os.path.basename(os.path.dirname(os.path.dirname(other))), keys, want))
  for run in (runs[0], other):
    for scope, counts in replay_facts.fact_counts(run).items():
      if counts["facts"] != counts["distinct_stable_keys"]:
        bad = True
        print("FAIL fact count != distinct stable keys in %s %s: %s" % (run, scope, counts))
sys.exit(1 if bad else 0)
PY

for dir in "$results"/*-rep*; do
  name="$(basename "$dir")"
  [[ -f "$dir/result.json" ]] || continue
  case "$name" in
    C-rep*|C2-rep*) continue ;;
    CF-rep*) kind=cfref; ref=("${controls[@]:0:2}") ;;
    C0-rep*) kind=control; ref=("${controls[@]}") ;;
    T3-rep*|T3TTL-rep*) kind=t3; ref=("${controls[@]}") ;;
    T2-rep*) kind=t2; ref=("${controls2[@]}") ;;
    CG-rep*) kind=cg; ref=() ;;
    *) kind=treatment; ref=("${controls[@]}") ;;
  esac
  echo "== $name ($kind)"
  python3 -I "$here/check_arm.py" ${judge[@]+"${judge[@]}"} --arm "$dir/result.json" --kind "$kind" ${cf[@]+"${cf[@]}"} ${ref[@]+"${ref[@]}"} || status=1
done

echo "== numbers (engaged-scrape consult p50/p95 and share within 100 ms, K, parked rows, superseded, A drain after catch-up)"
python3 -I - "$results" <<'PY'
import glob, json, os, sys
for path in sorted(glob.glob(os.path.join(sys.argv[1], "*-rep*", "result.json"))):
  r = json.load(open(path))
  a = r.get("consult", {}).get("A", {})
  rep = r["report"]
  print("%-12s ttl=%-3s consult(A) p50=%.1fms p95=%.1fms within100ms=%.2f n=%d | K_peak=%s parked_peak=%s woken=%s wait_rows=%s "
        "| superseded_scopes=%s paced_A=%s | A_done_after_catchup=%ss | load1 max=%s end=%s" % (
            os.path.basename(os.path.dirname(path)), r.get("ttl"), 1000 * a.get("p50_s", 0), 1000 * a.get("p95_s", 0),
            a.get("within_100ms", 0), a.get("count", 0), rep["k_peak"], rep["parked_rows_peak"], rep["woken_holding_wait"],
            rep["wait_table_rows_final"], r["paced"]["superseded_scopes"], r["paced"]["paced_items"].get("A", 0),
            r.get("a_drain", {}).get("after_catchup_s"), rep.get("load1_max"), rep.get("load1_end")))
PY
exit "$status"
