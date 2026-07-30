#!/usr/bin/env bash
# Malformed/stale-row regression cases for test-verify-telemetry-coverage.sh
# (#5855: the stale-target series, the P1 blank-path/blank-metric follow-ups,
# and the third review round's row-selection P1).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's init_repo(), expect_pass(), expect_fail(), run_verifier(),
# record_pass(), record_fail(), and case_pass (the clean fixture from case 1).
# Extracted to keep the orchestrator under the repo's 500-line cap, mirroring
# the scripts/lib/golden-corpus-lock-cases.sh split.
#
# Cases 24-26 pin the fix for a P1 one layer upstream of the blank-path and
# blank-metric P1s (cases 21-23): the row-SELECTION regex in
# verify-telemetry-coverage.sh required the first (stage-name) cell to be
# non-blank, so a row like "|  | path | metric | category |" never entered
# the candidate row set at all -- invisible to every downstream check,
# including the (3b) stale-target check that would otherwise catch its
# nonexistent path. Fixing row VALIDATION (as cases 21-23 did) cannot help a
# row that never reaches validation in the first place.

# Case 19 (#5855 review follow-up): a stale stage-table row placed AFTER
# the histogram-buckets section, with no section marker of its own. A
# check that excludes rows by a line-number cutoff derived from the
# histogram-buckets marker would silently skip this row; row shape (4
# columns vs. the histogram table's 2) must be what excludes histogram
# rows, not textual position, so this must still fail. The bucket table
# has header/separator only (no data row), so this cannot also trip the
# unrelated bucket-boundary check (4) and mask the signal under test.
case_row_after_histogram="$(init_repo case-row-after-histogram)"
cat >>"${case_row_after_histogram}/docs/public/observability/telemetry-coverage.md" <<'MD'

<!-- eshu:metric:section=histogram-buckets -->
## Histogram Bucket Boundaries

| set_name | boundary_values |
| --- | --- |

| ghost row after histogram section | go/internal/reducer/deleted_after_histogram.go:1 | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_row_after_histogram}" add .
git -C "${case_row_after_histogram}" commit -q -m "add stale stage row after the histogram-buckets section"
expect_fail "fails when a stale row is placed after the histogram-buckets section" "${case_row_after_histogram}"

# Case 20 (#5855 review follow-up): a stage-table row missing its trailing
# metric and category columns collapses to the same 2-column shape as a
# histogram-buckets row when split on '|'. Classifying by column count
# alone would silently skip it (invisible to 3b as the wrong shape,
# invisible to checks 1/2 with no eshu_dp_* metric, invisible to check 3
# since it names no new file). It must fail loud as malformed instead of
# vanishing — the narrower way to reach the same blind spot case 19
# closes.
case_malformed_row="$(init_repo case-malformed-row)"
cat >>"${case_malformed_row}/docs/public/observability/telemetry-coverage.md" <<'MD'

| malformed missing cols | go/internal/reducer/does_not_exist_malformed.go |
MD
git -C "${case_malformed_row}" add .
git -C "${case_malformed_row}" commit -q -m "add row missing its trailing metric and category columns"
expect_fail "fails when a row is missing its trailing metric/category columns" "${case_malformed_row}"

# Case 21 (#5855 P1): a row with the FULL column count (real stage, real
# metric, real category) but a BLANK path cell. `IFS=',' read -ra path_parts
# <<<""` yields zero array elements, so the per-token existence loop runs
# zero times and neither the path-exists check nor a malformed-row report
# ever fires -- the row silently passes forever, un-anchored from any real
# dispatcher. This is the exact "vanish instead of fail loud" mode case 20
# closed for missing trailing columns, reached instead through a blank
# middle cell.
case_blank_path="$(init_repo case-blank-path)"
cat >>"${case_blank_path}/docs/public/observability/telemetry-coverage.md" <<'MD'

| blank path stage |  | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_blank_path}" add .
git -C "${case_blank_path}" commit -q -m "add row with blank path cell"
expect_fail "fails when a full-column row's path cell is blank" "${case_blank_path}"

# Case 22 (#5855 P1 follow-up): a row whose path cell is present but
# consists only of commas (no real token between them). Each comma-split
# part trims to empty and is skipped by the per-token
# `[ -n "$part" ] || continue`, so the loop completes zero real checks
# without ever reporting drift -- the same vanish as case 21, reached
# through comma-only content instead of an empty string.
case_comma_only_path="$(init_repo case-comma-only-path)"
cat >>"${case_comma_only_path}/docs/public/observability/telemetry-coverage.md" <<'MD'

| comma only path stage | , , | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_comma_only_path}" add .
git -C "${case_comma_only_path}" commit -q -m "add row whose path cell is comma-only"
expect_fail "fails when a full-column row's path cell is comma-only" "${case_comma_only_path}"

# Case 23 (#5855 P1 follow-up): a row with a real, existing path but a
# BLANK metric column. Case 7 only covers a blank/TODO metric column on a
# row naming a *new* stage file (caught via the has_signal check in check
# (3)); an EXISTING row's metric column going blank -- e.g. after a bad
# rebase or merge -- has no equivalent guard, so it would vanish from the
# gate exactly like the blank-path case, just anchored on a different
# column.
case_blank_metric_existing="$(init_repo case-blank-metric-existing)"
cat >>"${case_blank_metric_existing}/docs/public/observability/telemetry-coverage.md" <<'MD'

| blank metric stage | go/internal/reducer/service.go:1 |  | reducer runtime |
MD
git -C "${case_blank_metric_existing}" add .
git -C "${case_blank_metric_existing}" commit -q -m "add row whose metric column is blank"
expect_fail "fails when an existing row's metric column is blank" "${case_blank_metric_existing}"

# Case 24 (#5855 P1): the exact reproduction from the finding -- a doc row
# whose stage-name cell is blank AND whose path names a file that does not
# exist. Before the selection-regex fix, the verifier exits 0 despite the
# named file not existing; the row is invisible, not merely unvalidated.
case_blank_stage="$(init_repo case-blank-stage)"
cat >>"${case_blank_stage}/docs/public/observability/telemetry-coverage.md" <<'MD'

|  | go/internal/reducer/does_not_exist.go:1 | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_blank_stage}" add .
git -C "${case_blank_stage}" commit -q -m "add row with blank stage-name cell and nonexistent path"
expect_fail "fails when a row's stage-name cell is blank and its path does not exist" "${case_blank_stage}"

# Case 25 (#5855 P1 follow-up): isolates the blank-stage-name defect from the
# stale-path defect case 24 exercises together. Path and metric are both
# valid/existing here, so ONLY an explicit "stage name is blank" check can
# catch this row -- the path-existence and metric-blank checks alone would
# both pass it silently.
case_blank_stage_valid_path="$(init_repo case-blank-stage-valid-path)"
cat >>"${case_blank_stage_valid_path}/docs/public/observability/telemetry-coverage.md" <<'MD'

|  | go/internal/reducer/service.go:1 | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_blank_stage_valid_path}" add .
git -C "${case_blank_stage_valid_path}" commit -q -m "add row with blank stage-name cell but an otherwise valid path and metric"
expect_fail "fails when a row's stage-name cell is blank even with a valid path and metric" "${case_blank_stage_valid_path}"

# Case 26 (#5855 P1 follow-up): broadening the selection regex to admit a
# blank-stage-name row must NOT also start admitting the stage table's own
# header ("| stage | file:line | required metric name(s) | category |") or
# GFM separator ("| --- | --- | --- | --- |") rows as malformed data rows --
# both are present in every fixture doc, including this failing one, so this
# proves they stay correctly ignored even in the presence of a real drift
# finding, not just in the clean-pass case.
if run_verifier "${case_blank_stage}"; then
  record_fail "header/separator rows stay ignored (fixture unexpectedly passed)"
else
  if rg --fixed-strings --quiet -- 'doc row "stage"' /tmp/eshu-telemetry-coverage.err 2>/dev/null ||
    rg --fixed-strings --quiet -- 'doc row "---"' /tmp/eshu-telemetry-coverage.err 2>/dev/null; then
    record_fail "header/separator rows must never be reported as malformed"
  else
    record_pass "header row and GFM separator row stay correctly ignored, not treated as data rows"
  fi
fi
