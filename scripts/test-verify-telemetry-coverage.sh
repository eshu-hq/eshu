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

# Case 4b: a new NON-.go file (package docs / evidence note) under a stage-owner
# directory must NOT be treated as a pipeline stage — a stage is a *.go source
# file; docs never register telemetry. Regression for the false positive that
# flagged go/internal/reducer/evidence-*.md.
case_new_doc="$(init_repo case-new-doc)"
mkdir -p "${case_new_doc}/go/internal/reducer" "${case_new_doc}/go/internal/collector"
printf '# evidence\n\nNo-Regression Evidence: n/a\n' >"${case_new_doc}/go/internal/reducer/evidence-example.md"
printf '# collector\n' >"${case_new_doc}/go/internal/collector/README.md"
git -C "${case_new_doc}" add .
git -C "${case_new_doc}" commit -q -m "add reducer evidence note + collector README"
expect_pass "ignores new non-.go files under stage-owner directories" "${case_new_doc}"

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

# Case 7: a new file is added AND a doc row is added that names the file,
# but the row's metric column is blank or TODO. The verifier must fail
# because the row has no eshu_dp_* metric or No-Observability-Change:
# marker, which would defeat the "every stage must register telemetry"
# policy. The fixed check requires a signal, not just a covered path.
case_blank_metric="$(init_repo case-blank-metric)"
mkdir -p "${case_blank_metric}/go/internal/reducer/blankstage"
printf 'package blankstage\n' >"${case_blank_metric}/go/internal/reducer/blankstage/materialization.go"
cat >>"${case_blank_metric}/docs/public/observability/telemetry-coverage.md" <<'MD'

| blank stage | go/internal/reducer/blankstage/materialization.go:1 | TODO | reducer runtime |
MD
git -C "${case_blank_metric}" add .
git -C "${case_blank_metric}" commit -q -m "add stage with blank/TODO metric column"
expect_fail "fails when a new row's metric column has no signal" "${case_blank_metric}"

# Case 8: a new file is added under go/internal/content/shape. The
# verifier's allow-list must cover the real package path; the original
# allow-list had a wrong path (go/internal/contentshape/*) that let
# content-shape files slip through. The new path
# (go/internal/content/shape/*.go) catches them.
case_content_shape="$(init_repo case-content-shape)"
mkdir -p "${case_content_shape}/go/internal/content/shape/newshape"
printf 'package newshape\n' >"${case_content_shape}/go/internal/content/shape/newshape/materialize.go"
git -C "${case_content_shape}" add .
git -C "${case_content_shape}" commit -q -m "add content shape stage"
expect_fail "fails when a new go/internal/content/shape file is not covered by the doc" "${case_content_shape}"

# Case 9 (repo-root under GIT_DIR): the verifier must derive repo_root from its
# own location, not `git rev-parse --show-toplevel`. Git hooks export GIT_DIR,
# under which `git -C scripts rev-parse --show-toplevel` returns <repo>/scripts,
# so the doc/instruments existence checks resolve under the wrong root and report
# a false "is missing". Run a COPY of the verifier from the fixture's scripts/
# with GIT_DIR set and ESHU_TELEMETRY_COVERAGE_REPO_ROOT unset; it must resolve
# the fixture root and PASS.
case_gitdir="$(init_repo case-gitdir)"
mkdir -p "${case_gitdir}/scripts"
cp "${verifier}" "${case_gitdir}/scripts/verify-telemetry-coverage.sh"
git -C "${case_gitdir}" add .
git -C "${case_gitdir}" commit -q -m "copy verifier into fixture scripts"
if env -u ESHU_TELEMETRY_COVERAGE_REPO_ROOT -u GITHUB_BASE_REF \
    GIT_DIR="${case_gitdir}/.git" ESHU_TELEMETRY_COVERAGE_BASE=HEAD~1 \
    "${case_gitdir}/scripts/verify-telemetry-coverage.sh" \
    >/tmp/eshu-telemetry-coverage.out 2>/tmp/eshu-telemetry-coverage.err; then
  record_pass "resolves repo_root from script location under GIT_DIR"
else
  record_fail "resolves repo_root from script location under GIT_DIR"
fi

# Case 10 (merge-base base): with no explicit base and no GITHUB_BASE_REF, the
# verifier must fall back to merge-base(origin/main, HEAD), not HEAD~1. On a
# branch with >1 commit past origin/main, a HEAD~1 base misses the first commit's
# new stage; merge-base catches it. origin/main is pinned at the initial commit,
# commit B adds an uncovered stage file, commit C (HEAD) is unrelated:
# merge-base flags B's stage and fails; a HEAD~1 base diffs only C and wrongly
# passes.
case_mergebase="$(init_repo case-mergebase)"
git -C "${case_mergebase}" update-ref refs/remotes/origin/main HEAD
mkdir -p "${case_mergebase}/go/internal/reducer/branchstage"
printf 'package branchstage\n' >"${case_mergebase}/go/internal/reducer/branchstage/materialization.go"
git -C "${case_mergebase}" add .
git -C "${case_mergebase}" commit -q -m "B: uncovered stage"
printf 'package reducer\n// trailing comment\n' >"${case_mergebase}/go/internal/reducer/service.go"
git -C "${case_mergebase}" add .
git -C "${case_mergebase}" commit -q -m "C: unrelated change"
if env -u ESHU_TELEMETRY_COVERAGE_BASE -u GITHUB_BASE_REF \
    ESHU_TELEMETRY_COVERAGE_REPO_ROOT="${case_mergebase}" \
    "${verifier}" >/tmp/eshu-telemetry-coverage.out 2>/tmp/eshu-telemetry-coverage.err; then
  record_fail "merge-base fallback flags a stage added before HEAD~1"
else
  record_pass "merge-base fallback flags a stage added before HEAD~1"
fi

# Case 11: clean bucket sets pass. Add a histogram with explicit buckets to
# instruments.go and a matching histogram-buckets doc section, plus a row in
# the main table so check (2) doesn't flag the new metric as undocumented.
# The verifier must confirm bidirectional bucket agreement.
case_buckets_clean="$(init_repo case-buckets-clean)"
cat >>"${case_buckets_clean}/go/internal/telemetry/instruments.go" <<'GO'
	if _, err := meter.Float64Histogram(
		"eshu_dp_test_histogram_seconds",
		metric.WithDescription("test histogram"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 10, 100),
	); err != nil {
		return nil, err
	}
GO
cat >>"${case_buckets_clean}/docs/public/observability/telemetry-coverage.md" <<'MD'

| test histogram | go/internal/telemetry/instruments.go:99 | `eshu_dp_test_histogram_seconds` | test |

<!-- eshu:metric:section=histogram-buckets -->
## Histogram Bucket Boundaries

| set_name | boundary_values |
| --- | --- |
| test-histogram-seconds | 1, 10, 100 |
MD
git -C "${case_buckets_clean}" add .
git -C "${case_buckets_clean}" commit -q -m "add histogram with doc row and bucket section"
expect_pass "passes when bucket sets agree between doc and code" "${case_buckets_clean}"

# Case 12: undocumented bucket set fails. Add a bucket set to instruments.go
# that is NOT in the doc's histogram-buckets section.
case_buckets_undocumented="$(init_repo case-buckets-undocumented)"
cat >>"${case_buckets_undocumented}/go/internal/telemetry/instruments.go" <<'GO'
	if _, err := meter.Float64Histogram(
		"eshu_dp_undocumented_histogram_seconds",
		metric.WithDescription("undocumented histogram"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(5, 50, 500),
	); err != nil {
		return nil, err
	}
GO
cat >>"${case_buckets_undocumented}/docs/public/observability/telemetry-coverage.md" <<'MD'

<!-- eshu:metric:section=histogram-buckets -->
## Histogram Bucket Boundaries

| set_name | boundary_values |
| --- | --- |
| known-set-seconds | 1, 10, 100 |
MD
git -C "${case_buckets_undocumented}" add .
git -C "${case_buckets_undocumented}" commit -q -m "add undocumented bucket set"
expect_fail "fails when code has bucket set not in doc" "${case_buckets_undocumented}"

# Case 13: documented bucket set missing from code fails. Add a bucket set
# to the doc that does NOT exist in instruments.go.
case_buckets_missing="$(init_repo case-buckets-missing)"
cat >>"${case_buckets_missing}/docs/public/observability/telemetry-coverage.md" <<'MD'

<!-- eshu:metric:section=histogram-buckets -->
## Histogram Bucket Boundaries

| set_name | boundary_values |
| --- | --- |
| ghost-histogram-seconds | 7, 77, 777 |
MD
git -C "${case_buckets_missing}" add .
git -C "${case_buckets_missing}" commit -q -m "add ghost bucket set to doc"
expect_fail "fails when doc has bucket set missing from code" "${case_buckets_missing}"

if [ "${FAIL}" -ne 0 ]; then
  printf 'verify-telemetry-coverage tests FAILED: %d/%d failed\n' "${FAIL}" "${TOTAL}" >&2
  exit 1
fi

printf 'verify-telemetry-coverage tests passed: %d/%d\n' "${PASS}" "${TOTAL}"
