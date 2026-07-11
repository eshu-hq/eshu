#!/usr/bin/env bash
#
# test-generate-operator-dashboard.sh — prove scripts/generate-operator-dashboard.sh
# is hermetic, idempotent, and produces a Grafana dashboard JSON that
# contains the headline eshu_dp_* panels from the X1 contract.
# Mirrors the test-verify-* shape in this repo.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="${repo_root}/scripts/generate-operator-dashboard.sh"
expected_path="${repo_root}/docs/public/observability/dashboards/eshu-operator-overview.json"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

require_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    printf 'skip - jq is not installed; cannot validate JSON\n' >&2
    exit 0
  fi
}

require_jq

# run_with_watchdog runs "$@" in the background and kills it if it has not
# finished within the given timeout. This is a portable (bash 3.2 and
# Homebrew bash alike, no GNU coreutils `timeout` required) substitute for
# `timeout N cmd`. It exists so a future regression that reintroduces a
# >512-byte heredoc body (issue #5019: bash >=5.1 writes the whole heredoc
# body to a pipe before spawning the reader, which deadlocks past macOS's
# 512-byte pipe buffer) fails this test mirror in seconds instead of
# hanging `make pre-pr` silently.
run_with_watchdog() {
  local timeout_secs="$1"
  shift
  "$@" &
  local pid=$!
  local waited=0
  while kill -0 "${pid}" 2>/dev/null; do
    sleep 1
    waited=$((waited + 1))
    if [ "${waited}" -ge "${timeout_secs}" ]; then
      kill -9 "${pid}" 2>/dev/null || true
      wait "${pid}" 2>/dev/null || true
      return 124
    fi
  done
  wait "${pid}"
}

# Case 1: the generator runs and produces a well-formed JSON.
out_path="${tmp_root}/case-1.json"
if ! run_with_watchdog 20 env \
  ESHU_OPERATOR_DASHBOARD_REPO_ROOT="${repo_root}" \
  ESHU_OPERATOR_DASHBOARD_OUTPUT_PATH="${out_path}" \
  "${generator}" >/dev/null; then
  record_fail "generator did not complete within the 20s watchdog (see issue #5019)"
  printf 'generate-operator-dashboard tests FAILED: watchdog triggered\n' >&2
  exit 1
fi
if jq -e . "${out_path}" >/dev/null 2>&1; then
  record_pass "generator produces a well-formed JSON"
else
  record_fail "generator output is not valid JSON"
fi

# Case 2: no drift — a fresh generator run reproduces the committed
# artifact byte-for-byte. (Deterministic, drift-free output is the
# load-bearing property of the gate; this is stronger than a re-run
# idempotency check.)
if cmp -s "${out_path}" "${expected_path}"; then
  record_pass "generator output matches the committed artifact"
else
  record_fail "generator output diverges from the committed artifact"
fi

# Case 3: the committed artifact parses as Grafana dashboard JSON with
# the expected top-level shape.
if jq -e '
  .title == "Eshu Operator Overview" and
  .uid == "eshu-operator-overview" and
  (.schemaVersion | type == "number") and
  (.panels | type == "array") and
  (.templating.list | type == "array")
' "${expected_path}" >/dev/null 2>&1; then
  record_pass "committed artifact has the expected top-level shape"
else
  record_fail "committed artifact top-level shape mismatch"
fi

# Case 4: every headline eshu_dp_* metric from the metric registry
# appears in the dashboard's panels. This is the load-bearing link
# between the X1 contract (eshu_dp_* families) and the operator
# surface (the dashboard).
metrics_lib="${repo_root}/scripts/lib/operator-dashboard-metrics.sh"
if [ ! -f "${metrics_lib}" ]; then
  record_fail "metric registry ${metrics_lib} is missing"
else
  missing=0
  while IFS= read -r metric; do
    [ -n "${metric}" ] || continue
    if ! jq -e --arg m "${metric}" '[.panels[].targets[]?.expr // "" | test($m)] | any' "${expected_path}" >/dev/null 2>&1; then
      missing=$((missing + 1))
      printf '  missing panel expression referencing %s\n' "${metric}" >&2
    fi
  done < <(rg -o "eshu_dp_[a-zA-Z0-9_]+" "${metrics_lib}" | sort -u)
  if [ "${missing}" -eq 0 ]; then
    record_pass "every eshu_dp_* metric from the registry appears in the dashboard"
  else
    record_fail "${missing} eshu_dp_* metric(s) from the registry are not in any panel expression"
  fi
fi

# Case 5: the "Is Eshu Healthy?" row is present with the alarm
# single-stat. This is the 3 AM alarm row the spec requires.
if jq -e '
  [.panels[] | select(.type == "row") | .title] | index("Is Eshu Healthy?") != null
' "${expected_path}" >/dev/null 2>&1; then
  record_pass "dashboard has the 'Is Eshu Healthy?' row"
else
  record_fail "dashboard is missing the 'Is Eshu Healthy?' row"
fi

# Case 6: the headline templating variables (datasource, pool, queue,
# route, scope_id) are present.
if jq -e '
  [.templating.list[].name] | contains(["datasource", "pool", "queue", "route", "scope_id"])
' "${expected_path}" >/dev/null 2>&1; then
  record_pass "dashboard exposes the headline templating variables"
else
  record_fail "dashboard is missing one or more headline templating variables"
fi

# Case 7: the file size is sane (between 5KB and 200KB).
size=$(wc -c < "${expected_path}" | tr -d ' ')
if [ "${size}" -gt 5000 ] && [ "${size}" -lt 200000 ]; then
  record_pass "dashboard size is sane (${size} bytes)"
else
  record_fail "dashboard size is out of range: ${size} bytes"
fi

# Case 8: every panel id is unique. Grafana requires unique panel ids; a
# collision (two panels sharing an id) breaks panel linking and edit routing.
dup_ids=$(jq -r '[.panels[].id] | group_by(.) | map(select(length > 1) | .[0]) | .[]' "${expected_path}" 2>/dev/null)
if [ -z "${dup_ids}" ]; then
  record_pass "every panel id is unique"
else
  record_fail "duplicate panel id(s): $(echo "${dup_ids}" | tr '\n' ' ')"
fi

# Case 9: the first-class dead-letter operator surface has a route-keyed
# dashboard panel, so operators can see whether the bounded read is being used
# and whether it is erroring.
if jq -e '
  [.panels[] | select(.title == "Dead-Letter Operator Surface") | .targets[]?.expr]
  | any(test("POST /api/v0/admin/dead-letters/query"))
' "${expected_path}" >/dev/null 2>&1; then
  record_pass "dashboard has the dead-letter operator surface panel"
else
  record_fail "dashboard is missing the dead-letter operator surface panel"
fi

if [ "${FAIL}" -ne 0 ]; then
  printf 'generate-operator-dashboard tests FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

printf 'generate-operator-dashboard tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
