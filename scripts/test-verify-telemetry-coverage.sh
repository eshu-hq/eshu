#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-telemetry-coverage.sh"

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
  if [ -f /tmp/eshu-telemetry-coverage.out ]; then
    echo '--- stdout ---' >&2
    sed -n '1,160p' /tmp/eshu-telemetry-coverage.out >&2
  fi
  if [ -f /tmp/eshu-telemetry-coverage.err ]; then
    echo '--- stderr ---' >&2
    sed -n '1,160p' /tmp/eshu-telemetry-coverage.err >&2
  fi
}

# init_repo <name> creates a fresh tmp git repo with a minimal but valid
# telemetry-coverage.md table and matching instruments.go, then commits.
# Writes the repo path to stdout.
init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Telemetry Coverage Test"
  mkdir -p "${dir}/docs/public/observability" "${dir}/go/internal/telemetry" "${dir}/go/internal/reducer"
  cat >"${dir}/docs/public/observability/telemetry-coverage.md" <<'MD'
# Telemetry Coverage Contract

This page enumerates every observable stage in the Eshu data plane and the
metric, span, or log key it must emit. The CI coverage script (X2) diffs
against it.

## Reducer Stages

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| queue claim | go/internal/reducer/service.go:1 | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
| reducer run | go/internal/reducer/service.go:2 | `eshu_dp_reducer_run_duration_seconds` | reducer runtime |
MD
  cat >"${dir}/go/internal/telemetry/instruments.go" <<'GO'
// Package telemetry holds the metric instruments for the test repo.
package telemetry

import "go.opentelemetry.io/otel/metric"

type Inits struct{}

// InitInstruments registers all metrics referenced by the X1 doc.
func InitInstruments(meter metric.Meter) (*Inits, error) {
	if _, err := meter.Int64Histogram(
		"eshu_dp_queue_claim_duration_seconds",
		metric.WithDescription("queue claim duration"),
	); err != nil {
		return nil, err
	}
	if _, err := meter.Float64Histogram(
		"eshu_dp_reducer_run_duration_seconds",
		metric.WithDescription("reducer run duration"),
	); err != nil {
		return nil, err
	}
	return &Inits{}, nil
}
GO
  printf 'package reducer\n' >"${dir}/go/internal/reducer/service.go"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

run_verifier() {
  local dir="$1"
  ESHU_TELEMETRY_COVERAGE_REPO_ROOT="${dir}" \
    ESHU_TELEMETRY_COVERAGE_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-telemetry-coverage.out 2>/tmp/eshu-telemetry-coverage.err
}

expect_pass() {
  local label="$1"
  local dir="$2"
  if run_verifier "${dir}"; then
    record_pass "${label}"
  else
    record_fail "${label}"
  fi
}

expect_fail() {
  local label="$1"
  local dir="$2"
  if run_verifier "${dir}"; then
    record_fail "${label}"
  else
    record_pass "${label}"
  fi
}

# Case 1: a clean repo where the doc and instruments.go agree and there are
# no new files should pass.
case_pass="$(init_repo case-pass)"
expect_pass "passes when doc and instruments.go agree" "${case_pass}"

# Case 2: doc mentions a metric that is NOT registered in instruments.go.
# The verifier must fail with a per-stage report.
case_missing_metric="$(init_repo case-missing-metric)"
cat >>"${case_missing_metric}/docs/public/observability/telemetry-coverage.md" <<'MD'

| ghost stage | go/internal/reducer/service.go:3 | `eshu_dp_ghost_metric_total` | reducer runtime |
MD
git -C "${case_missing_metric}" add .
git -C "${case_missing_metric}" commit -q -m "add ghost stage to doc"
expect_fail "fails when doc references unregistered metric" "${case_missing_metric}"

# Case 3: instruments.go registers a metric that is NOT in the doc.
# This is the #3633 defined-but-never-registered drift class.
case_drift_metric="$(init_repo case-drift-metric)"
cat >>"${case_drift_metric}/go/internal/telemetry/instruments.go" <<'GO'
	if _, err := meter.Int64Counter(
		"eshu_dp_undocumented_metric_total",
		metric.WithDescription("registered but not in the X1 doc"),
	); err != nil {
		return nil, err
	}
GO
git -C "${case_drift_metric}" add .
git -C "${case_drift_metric}" commit -q -m "register undocumented metric"
expect_fail "fails when instruments.go registers a metric missing from the doc" "${case_drift_metric}"

# Case 4: a new file appears under go/internal/ that the X1 doc does not
# reference. The verifier must flag the untracked stage.
case_new_file="$(init_repo case-new-file)"
mkdir -p "${case_new_file}/go/internal/reducer/newstage"
printf 'package newstage\n' >"${case_new_file}/go/internal/reducer/newstage/materialization.go"
git -C "${case_new_file}" add .
git -C "${case_new_file}" commit -q -m "add new reducer stage without doc row"
expect_fail "fails when a new go/internal file is not covered by the doc" "${case_new_file}"

# Case 5: the doc has a No-Observability-Change: marker for a stage whose
# underlying counters are intentionally not registered. The verifier must
# accept the marker and exit 0.
case_no_change="$(init_repo case-no-change)"
cat >"${case_no_change}/docs/public/observability/telemetry-coverage.md" <<'MD'
# Telemetry Coverage Contract

This page enumerates every observable stage in the Eshu data plane and the
metric, span, or log key it must emit. The CI coverage script (X2) diffs
against it.

## Reducer Stages

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| queue claim | go/internal/reducer/service.go:1 | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
| reducer run | go/internal/reducer/service.go:2 | `eshu_dp_reducer_run_duration_seconds` | reducer runtime |
| content re-read | go/internal/telemetry/instruments.go:1 | `No-Observability-Change: eshu_dp_content_rereads_total and eshu_dp_content_reread_skips_total counters are registered but no longer emit; facts emitted/fact batches committed cover the path` | reducer fact commit |
MD
git -C "${case_no_change}" add .
git -C "${case_no_change}" commit -q -m "add No-Observability-Change marker"
expect_pass "accepts No-Observability-Change marker in doc row" "${case_no_change}"

# Case 6: missing doc entirely should fail cleanly with a clear error.
case_missing_doc="$(init_repo case-missing-doc)"
rm "${case_missing_doc}/docs/public/observability/telemetry-coverage.md"
git -C "${case_missing_doc}" add -A
git -C "${case_missing_doc}" commit -q -m "delete doc"
expect_fail "fails when telemetry-coverage.md is missing" "${case_missing_doc}"

if [ "${FAIL}" -ne 0 ]; then
  printf 'verify-telemetry-coverage tests FAILED: %d/%d failed\n' "${FAIL}" "${TOTAL}" >&2
  exit 1
fi

printf 'verify-telemetry-coverage tests passed: %d/%d\n' "${PASS}" "${TOTAL}"
