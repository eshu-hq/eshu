#!/usr/bin/env bash
# 9740 retest evaluation: exact PromQL + window procedure.
#
# Usage (QA Prometheus via port-forward; tear the forward down afterwards):
#   kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
#   PROM_URL=http://127.0.0.1:9090 bash docs/internal/arbiter/9740-retest-eval.sh --mode 2b
#   PROM_URL=http://127.0.0.1:9090 bash docs/internal/arbiter/9740-retest-eval.sh --mode 2a \
#     --end 2026-11-22T12:00:00Z
#
# Modes:
#   2b  trailing-48h series/counts for ALL outcomes of
#       eshu_dp_security_alert_reconciliation_impact_wait_total plus the
#       same-period security_alert_reconciliation execution count.
#   2a  rolling-1h windows at 5m steps over the trailing 14 days ending at
#       --end (default now): replay_flagged rate >= 0.5x executions rate AND
#       oldest age rising 30 min straight. Prints max ratio + window +
#       reopened/pending_noop beside it.
#
# Window procedure (2a), programmed exactly as implemented below:
#   step 5m; at each step t, ratio(t) = increase(fanout_total{outcome=
#   "replay_flagged"}[1h]) / increase(reducer_executions_total{domain=
#   "supply_chain_impact"}[1h]). Steps with zero denominator are skipped.
#   Fire iff some t has ratio(t) >= 0.5 AND the oldest-age gauge
#   (eshu_runtime_domain_oldest_age_seconds{domain="supply_chain_impact"})
#   rises strictly across 7 consecutive 5m samples ending at or after t
#   (30 minutes of straight rise). Report: max ratio, its window, and the
#   reopened + pending_noop increases over that window.
set -euo pipefail

PROM_URL="${PROM_URL:-http://127.0.0.1:9090}"
MODE="2b"
END="now"

while [ $# -gt 0 ]; do
  case "$1" in
    --mode) MODE="$2"; shift 2 ;;
    --end) END="$2"; shift 2 ;;
    --prom-url) PROM_URL="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

WAIT_METRIC='eshu_dp_security_alert_reconciliation_impact_wait_total'
EXEC_METRIC='eshu_dp_reducer_executions_total'
FANOUT_METRIC='eshu_dp_supply_chain_impact_fanout_total'
AGE_METRIC='eshu_runtime_domain_oldest_age_seconds'

q() { # q <promql> [start] [end] [step] -> response body
  local query="$1" start="${2:-}" end="${3:-}" step="${4:-}"
  if [ -n "$start" ]; then
    curl -sS -G "${PROM_URL}/api/v1/query_range" \
      --data-urlencode "query=${query}" \
      --data-urlencode "start=${start}" \
      --data-urlencode "end=${end}" \
      --data-urlencode "step=${step}" \
      --max-time 300
  else
    curl -sS -G "${PROM_URL}/api/v1/query" \
      --data-urlencode "query=${query}" \
      --max-time 120
  fi
}

EARLIEST_48H="$(date -u -d '48 hours ago' +%Y-%m-%dT%H:%M:%SZ)"
NOW_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

case "$MODE" in
  2b)
    echo "== 2b window: ${EARLIEST_48H} .. ${NOW_UTC} =="
    echo "Q1 series-with-samples per outcome:"
    echo "  count by (outcome) (max_over_time(${WAIT_METRIC}[48h]))"
    q "count by (outcome) (max_over_time(${WAIT_METRIC}[48h]))"
    echo; echo "Q2 48h counts per outcome:"
    echo "  sum by (outcome) (increase(${WAIT_METRIC}[48h]))"
    q "sum by (outcome) (increase(${WAIT_METRIC}[48h]))"
    echo; echo "Q3 reconciliation execution denominator (same 48h):"
    echo "  sum(increase(${EXEC_METRIC}{domain=\"security_alert_reconciliation\"}[48h]))"
    q "sum(increase(${EXEC_METRIC}{domain=\"security_alert_reconciliation\"}[48h]))"
    echo
    ;;
  2a)
    if [ "$END" = "now" ]; then END="$NOW_UTC"; fi
    START="$(date -u -d "$END - 14 days" +%Y-%m-%dT%H:%M:%SZ)"
    echo "== 2a window: ${START} .. ${END} step 5m =="
    R_FLAG='increase('"${FANOUT_METRIC}"'{domain="supply_chain_impact",outcome="replay_flagged"}[1h])'
    R_EXEC='increase('"${EXEC_METRIC}"'{domain="supply_chain_impact",queue="reducer"}[1h])'
    R_OPEN='increase('"${FANOUT_METRIC}"'{domain="supply_chain_impact",outcome="reopened"}[1h])'
    R_NOOP='increase('"${FANOUT_METRIC}"'{domain="supply_chain_impact",outcome="pending_noop"}[1h])'
    echo "flagged : ${R_FLAG}"
    echo "exec    : ${R_EXEC}"
    echo "age     : ${AGE_METRIC}{domain=\"supply_chain_impact\"}"
    export PROM_URL R_FLAG R_EXEC R_OPEN R_NOOP AGE_METRIC START END
    q "$R_FLAG" "$START" "$END" "5m" > /tmp/retest-2a-flag.json
    q "$R_EXEC" "$START" "$END" "5m" > /tmp/retest-2a-exec.json
    q "$R_OPEN" "$START" "$END" "5m" > /tmp/retest-2a-open.json
    q "$R_NOOP" "$START" "$END" "5m" > /tmp/retest-2a-noop.json
    q "${AGE_METRIC}{domain=\"supply_chain_impact\"}" "$START" "$END" "5m" > /tmp/retest-2a-age.json
    python3 - <<'EOF'
import json

def series(path):
    # Sum across EVERY returned series per timestamp. These counters carry
    # third labels (fanout cause / execution status), so reading r[0] alone
    # would silently drop most of the rate and corrupt the 0.5 threshold.
    from collections import defaultdict
    d = json.load(open(path))
    assert d["status"] == "success", d
    acc = defaultdict(float)
    for s in d["data"]["result"]:
        for t, v in s["values"]:
            acc[t] += float(v)
    return sorted(acc.items())

flag = series("/tmp/retest-2a-flag.json")
exc = dict(series("/tmp/retest-2a-exec.json"))
opn = dict(series("/tmp/retest-2a-open.json"))
nop = dict(series("/tmp/retest-2a-noop.json"))
age = series("/tmp/retest-2a-age.json")

# 30-min straight rise first: 7 consecutive strictly increasing 5m age
# samples. The ruling fires iff some t has ratio>=0.5 AND a rise window
# ending at or after t, so the ratio search below only considers covered t.
fires = []
for i in range(len(age) - 6):
    w = age[i:i + 7]
    if all(b[1] > a[1] for a, b in zip(w, w[1:])):
        fires.append((w[0][0], w[-1][0], w[-1][1] - w[0][1]))

def covered(t):
    return any(e >= t for _, e, _ in fires)

best = None
uncovered_peak = None
for t, f in flag:
    e = exc.get(t)
    if e is None or e <= 0:
        continue
    r = f / e
    if uncovered_peak is None or r > uncovered_peak[1]:
        uncovered_peak = (t, r)
    if not covered(t):
        continue
    if best is None or r > best[1]:
        best = (t, r, f, e, opn.get(t, 0.0), nop.get(t, 0.0))

import datetime
if best is None:
    if uncovered_peak is None:
        print("max_ratio: NO_DATA (zero/no execution denominator at every step)")
    else:
        ts = datetime.datetime.fromtimestamp(uncovered_peak[0], datetime.timezone.utc).isoformat()
        print(f"max_ratio: NO_FIRE {uncovered_peak[1]:.4f} at {ts} "
              f"has no 30m rise window ending at/after it")
else:
    ts = datetime.datetime.fromtimestamp(best[0], datetime.timezone.utc).isoformat()
    print(f"max_ratio: {best[1]:.4f} at {ts} "
          f"(window {ts} minus 1h; flagged={best[2]:.2f} exec={best[3]:.2f}) "
          f"FIRES iff >= 0.5000 with rise window ending at/after {ts}")
    print(f"beside: reopened={best[4]:.2f} pending_noop={best[5]:.2f} over that 1h window")

print(f"rise_windows_30m_strict: {len(fires)}")
for s, e, d in fires[:10]:
    ss = datetime.datetime.fromtimestamp(s, datetime.timezone.utc).isoformat()
    ee = datetime.datetime.fromtimestamp(e, datetime.timezone.utc).isoformat()
    print(f"  rise {ss} .. {ee} delta={d:.1f}s")
EOF
    ;;
  *)
    echo "unknown --mode $MODE (want 2b or 2a)" >&2; exit 2 ;;
esac
