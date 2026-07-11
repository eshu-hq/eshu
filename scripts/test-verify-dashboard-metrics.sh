#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-dashboard-metrics.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
TOTAL=0

record_pass() {
  PASS=$((PASS + 1))
  TOTAL=$((TOTAL + 1))
  printf 'ok - %s\n' "$1"
}

record_fail() {
  FAIL=$((FAIL + 1))
  TOTAL=$((TOTAL + 1))
  echo "not ok - $1" >&2
  if [ -f /tmp/eshu-dashboard-metrics.out ]; then
    echo '--- stdout ---' >&2
    sed -n '1,80p' /tmp/eshu-dashboard-metrics.out >&2
  fi
  if [ -f /tmp/eshu-dashboard-metrics.err ]; then
    echo '--- stderr ---' >&2
    sed -n '1,80p' /tmp/eshu-dashboard-metrics.err >&2
  fi
}

# init_fixture <name> creates a fresh temp directory with a minimal dashboard
# JSON and instruments.go, using the same directory layout the verifier
# expects.  Writes the fixture root path to stdout.
init_fixture() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/deploy/grafana/dashboards" \
           "${dir}/docs/dashboards" \
           "${dir}/docs/public/observability/dashboards" \
           "${dir}/go/internal/telemetry"

  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and
  # macOS's 512-byte pipe buffer deadlocks on any body over that size
  # (#5074).
  cat "${repo_root}/scripts/lib/test-verify-dashboard-metrics-instruments.go" >"${dir}/go/internal/telemetry/instruments.go"

  printf '%s\n' "${dir}"
}

# make_dashboard <dir> <name> <metrics...> — writes a minimal Grafana 8/9
# dashboard JSON with a single panel whose targets reference the given metric
# names.
make_dashboard() {
  local dir="$1"
  local name="$2"
  shift 2
  local targets=""
  for m in "$@"; do
    targets="${targets}{\"expr\":\"rate(${m}[5m])\"},"
  done
  cat >"${dir}/${name}" <<JSON
{
  "title": "test",
  "panels": [
    {
      "title": "panel one",
      "targets": [${targets%,}]
    }
  ]
}
JSON
}

run_verifier() {
  local fixture_root="$1"
  shift
  # We need to override repo_root so the verifier resolves the fixture.  The
  # verifier derives repo_root from its own directory, so we copy it into the
  # fixture's scripts/ directory and run from there.
  local verifier_copy="${fixture_root}/scripts/verify-dashboard-metrics.sh"
  mkdir -p "$(dirname "${verifier_copy}")"
  cp "${verifier}" "${verifier_copy}"
  "${verifier_copy}" "$@" >/tmp/eshu-dashboard-metrics.out 2>/tmp/eshu-dashboard-metrics.err
}

expect_pass() {
  local label="$1"
  local fixture_root="$2"
  shift 2
  if run_verifier "${fixture_root}" "$@"; then
    record_pass "${label}"
  else
    record_fail "${label}"
  fi
}

expect_fail() {
  local label="$1"
  local fixture_root="$2"
  shift 2
  if run_verifier "${fixture_root}" "$@"; then
    record_fail "${label}"
  else
    record_pass "${label}"
  fi
}

# ---------------------------------------------------------------------------
# Case 1: clean dashboard — all metrics are registered.
# ---------------------------------------------------------------------------
case_clean="$(init_fixture case-clean)"
make_dashboard "${case_clean}/deploy/grafana/dashboards" "clean.json" \
  eshu_dp_facts_emitted_total eshu_dp_reducer_run_duration_seconds_bucket
expect_pass "passes when all dashboard metrics are registered" "${case_clean}"

# ---------------------------------------------------------------------------
# Case 2: orphan metric — a dashboard references a metric NOT in instruments.go
# ---------------------------------------------------------------------------
case_orphan="$(init_fixture case-orphan)"
make_dashboard "${case_orphan}/docs/dashboards" "orphan.json" \
  eshu_dp_facts_emitted_total eshu_dp_ghost_metric_total
expect_fail "fails when a dashboard references an unregistered metric" "${case_orphan}"

# ---------------------------------------------------------------------------
# Case 3: histogram _bucket / _count / _sum suffixes resolve to base names.
# ---------------------------------------------------------------------------
case_histogram="$(init_fixture case-histogram)"
make_dashboard "${case_histogram}/docs/public/observability/dashboards" "hist.json" \
  eshu_dp_reducer_run_duration_seconds_bucket \
  eshu_dp_reducer_run_duration_seconds_count \
  eshu_dp_reducer_run_duration_seconds_sum
expect_pass "accepts histogram _bucket/_count/_sum suffix forms" "${case_histogram}"

# ---------------------------------------------------------------------------
# Case 4: --table mode produces a markdown table.
# ---------------------------------------------------------------------------
case_table="$(init_fixture case-table)"
make_dashboard "${case_table}/deploy/grafana/dashboards" "tbl.json" \
  eshu_dp_facts_emitted_total eshu_dp_reducer_run_duration_seconds_bucket
if run_verifier "${case_table}" --table; then
  out="$(cat /tmp/eshu-dashboard-metrics.out)"
  if printf '%s' "${out}" | rg -q '^\| Dashboard \| Metric \| Found'; then
    record_pass "--table outputs a markdown coverage table"
  else
    record_fail "--table outputs a markdown coverage table"
  fi
else
  record_fail "--table outputs a markdown coverage table"
fi

# ---------------------------------------------------------------------------
# Case 5: --dashboard PATH checks a single file.
# ---------------------------------------------------------------------------
case_single="$(init_fixture case-single)"
make_dashboard "${case_single}/docs/dashboards" "single.json" \
  eshu_dp_facts_emitted_total
# Also create a second dashboard with an orphan (should be ignored in single
# mode).
make_dashboard "${case_single}/deploy/grafana/dashboards" "ignored.json" \
  eshu_dp_orphan_metric_total
expect_pass "passes when --dashboard single file is clean (ignores other dashboards)" \
  "${case_single}" --dashboard "${case_single}/docs/dashboards/single.json"
# Now check the orphan dashboard in single mode — should fail.
expect_fail "fails when --dashboard single file has an orphan" \
  "${case_single}" --dashboard "${case_single}/deploy/grafana/dashboards/ignored.json"

# ---------------------------------------------------------------------------
# Case 6: missing instruments.go → exits non-zero with a clear message.
# Must have a dashboard JSON present (otherwise the verifier exits 0 early
# for "no dashboard JSON files found"); the failure must come from the
# missing instruments.go check.
# ---------------------------------------------------------------------------
case_no_instruments="$(init_fixture case-no-instruments)"
make_dashboard "${case_no_instruments}/docs/dashboards" "exists.json" \
  eshu_dp_facts_emitted_total
rm "${case_no_instruments}/go/internal/telemetry/instruments.go"
if run_verifier "${case_no_instruments}"; then
  record_fail "fails when instruments.go is missing in the fixture"
else
  record_pass "fails when instruments.go is missing in the fixture"
fi

# ---------------------------------------------------------------------------
# Results.
# ---------------------------------------------------------------------------
if [ "${FAIL}" -ne 0 ]; then
  printf 'verify-dashboard-metrics tests FAILED: %d/%d failed\n' "${FAIL}" "${TOTAL}" >&2
  exit 1
fi

printf 'verify-dashboard-metrics tests passed: %d/%d\n' "${PASS}" "${TOTAL}"
