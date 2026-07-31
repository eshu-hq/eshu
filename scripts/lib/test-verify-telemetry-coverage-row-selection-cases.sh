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

# Case 27 (#5855, fourth instance of the same "row vanishes before
# validation" defect across three review rounds -- blank stage cell (case
# 24/25), blank/comma-only path cell (cases 21/22), now a bare single-byte
# `|` row): the row-selection regex `^\|` matches a line that is exactly
# one pipe character and nothing else, so it enters all_rows_tmp, but
# `IFS='|' read -ra cols` on that exact line yields a ONE-element array
# (n=1) -- proven empirically, since a lone delimiter with no trailing
# byte leaves read nothing to emit for a second field. The old
# `[ "$n" -ge 2 ] || continue` guard treated n=1 as "not a row" and skipped
# it before any check ever ran, so this exact line passes the gate silently
# forever. It must now fail loud as a malformed row, the same outcome a
# truncated stage-table row gets.
case_bare_pipe="$(init_repo case-bare-pipe)"
printf '\n|\n' >>"${case_bare_pipe}/docs/public/observability/telemetry-coverage.md"
git -C "${case_bare_pipe}" add .
git -C "${case_bare_pipe}" commit -q -m "add a bare single-pipe row to the doc"
expect_fail "fails when a doc row is exactly a bare single pipe byte" "${case_bare_pipe}"

# Case 28 (#5855 follow-up to case 27): a row consisting of only pipe
# characters and no content at all (e.g. "|||"). Splitting on '|' gives it
# enough fields to reach the same malformed-row content classification the
# n-lt-5 branch already applies to a truncated stage-table row; this pins
# that it is reported rather than silently accepted as histogram-buckets
# data (its second column, empty, does not match the boundary-values
# numeric-list shape).
case_all_pipes="$(init_repo case-all-pipes)"
printf '\n|||\n' >>"${case_all_pipes}/docs/public/observability/telemetry-coverage.md"
git -C "${case_all_pipes}" add .
git -C "${case_all_pipes}" commit -q -m "add a row of only pipe characters to the doc"
expect_fail "fails when a doc row consists of only pipe characters" "${case_all_pipes}"

# Case 29 (#5855 follow-up to case 27): a row whose cells are present but
# contain only whitespace between the pipes (e.g. "|   |   |"). Every cell
# trims to empty, so this must be reported as malformed for the same reason
# case 28 is -- a blank second column that is neither a legitimate
# file:line/boundary_values header nor a numeric bucket-boundary list.
case_whitespace_only="$(init_repo case-whitespace-only)"
printf '\n|   |   |\n' >>"${case_whitespace_only}/docs/public/observability/telemetry-coverage.md"
git -C "${case_whitespace_only}" add .
git -C "${case_whitespace_only}" commit -q -m "add a row of only whitespace between pipes to the doc"
expect_fail "fails when a doc row has only whitespace between its pipes" "${case_whitespace_only}"

# Case 30 (#5855 false-positive guard for the case 27-29 fix): the
# broadened acceptance of short rows must NOT start flagging the doc's own
# legitimate rows. Re-run the clean fixture (case 1, header + separator +
# two full 4-column data rows) and the real repo doc's histogram-buckets
# header/separator rows are already exercised by every other expect_pass
# case above; this case adds a genuine bare-pipe-in-prose row (mirroring
# the real doc's `reconciliation_status=not_requested|applied|...` row,
# which pushes a legitimate stage-table row to 7 fields when split on '|')
# to confirm a row that legitimately contains bare pipes in its prose still
# passes.
case_prose_pipes="$(init_repo case-prose-pipes)"
cat >>"${case_prose_pipes}/docs/public/observability/telemetry-coverage.md" <<'MD'

| reconciliation stage | go/internal/reducer/service.go:1 | `No-Observability-Change: status is one of not_requested|applied|suppressed_input_invalid, covered by eshu_dp_reducer_run_duration_seconds` | reducer runtime |
MD
git -C "${case_prose_pipes}" add .
git -C "${case_prose_pipes}" commit -q -m "add a legitimate row whose metric prose contains bare pipes"
expect_pass "passes when a legitimate row's prose contains bare pipe characters" "${case_prose_pipes}"

# Case 31 (#5855, fourth review round): an EXISTING row (naming
# go/internal/reducer/service.go, already present at $base, so check (3)'s
# new-stage has_signal guard never fires) whose metric column is the literal
# placeholder "TODO". The doc-authoring comment above (3b) and the failure
# message it emits both promise "expected an eshu_dp_* metric or a
# No-Observability-Change: marker", but until this fix the code only tested
# `[ -z "$metric_cell" ]` -- true blank-only. A non-blank placeholder like
# "TODO" satisfied that bare test and passed the gate silently, exactly the
# malformed-cell-silently-accepted defect class this whole file exists to
# close, now landed on the metric column of an EXISTING row instead of a new
# stage's path/stage-name cell.
case_metric_todo="$(init_repo case-metric-todo)"
cat >>"${case_metric_todo}/docs/public/observability/telemetry-coverage.md" <<'MD'

| todo metric stage | go/internal/reducer/service.go:1 | TODO | reducer runtime |
MD
git -C "${case_metric_todo}" add .
git -C "${case_metric_todo}" commit -q -m "add existing row whose metric column is the placeholder TODO"
expect_fail "fails when an existing row's metric column is the placeholder TODO" "${case_metric_todo}"

# Case 32 (#5855, fourth review round): same defect as case 31, reached
# through arbitrary non-placeholder prose instead of the word "TODO" -- proves
# the fix matches the actual eshu_dp_*/No-Observability-Change: signal
# pattern rather than a hardcoded "TODO" string literal.
case_metric_garbage="$(init_repo case-metric-garbage)"
cat >>"${case_metric_garbage}/docs/public/observability/telemetry-coverage.md" <<'MD'

| garbage metric stage | go/internal/reducer/service.go:1 | garbage-not-a-real-metric | reducer runtime |
MD
git -C "${case_metric_garbage}" add .
git -C "${case_metric_garbage}" commit -q -m "add existing row whose metric column is unrecognized prose"
expect_fail "fails when an existing row's metric column is arbitrary non-signal prose" "${case_metric_garbage}"

# Case 33 (#5855, fourth review round, regression guard): an EXISTING row
# whose metric column is a real, registered eshu_dp_* name must keep passing
# after the case 31/32 fix tightens the check.
case_metric_valid_name="$(init_repo case-metric-valid-name)"
cat >>"${case_metric_valid_name}/docs/public/observability/telemetry-coverage.md" <<'MD'

| valid metric stage | go/internal/reducer/service.go:1 | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_metric_valid_name}" add .
git -C "${case_metric_valid_name}" commit -q -m "add existing row whose metric column names a real registered metric"
expect_pass "passes when an existing row's metric column names a real eshu_dp_* metric" "${case_metric_valid_name}"

# Case 34 (#5855, fourth review round, regression guard): an EXISTING row
# whose metric column is a proper No-Observability-Change: marker must keep
# passing after the case 31/32 fix.
case_metric_valid_marker="$(init_repo case-metric-valid-marker)"
cat >>"${case_metric_valid_marker}/docs/public/observability/telemetry-coverage.md" <<'MD'

| marked stage | go/internal/reducer/service.go:1 | `No-Observability-Change: covered by eshu_dp_reducer_run_duration_seconds` | reducer runtime |
MD
git -C "${case_metric_valid_marker}" add .
git -C "${case_metric_valid_marker}" commit -q -m "add existing row whose metric column is a valid No-Observability-Change marker"
expect_pass "passes when an existing row's metric column is a valid No-Observability-Change marker" "${case_metric_valid_marker}"

# Case 35 (#5855, fourth review round, real-doc regression guard): pins the
# real telemetry-coverage.md convention at lines 755 and 760, where a
# structured-log-key row's metric column reads
# "(no metric; structured log fields)" / "(no metric; safe log fields)" for
# log keys that are pure correlation identifiers with no metric of their own
# -- pre-existing content (present on origin/main before this branch) using a
# second, narrower marker dialect alongside No-Observability-Change:. An
# audit of every stage-shaped row in the real doc found exactly these two
# non-blank rows that a strict eshu_dp_*/No-Observability-Change:-only
# pattern would newly flag as a regression, so the shared signal pattern
# also accepts a cell that STARTS WITH the literal "(no metric" prefix.
case_metric_no_metric_marker="$(init_repo case-metric-no-metric-marker)"
cat >>"${case_metric_no_metric_marker}/docs/public/observability/telemetry-coverage.md" <<'MD'

| log key stage | go/internal/reducer/service.go:1 | (no metric; structured log fields) | reducer runtime |
MD
git -C "${case_metric_no_metric_marker}" add .
git -C "${case_metric_no_metric_marker}" commit -q -m "add existing row whose metric column uses the (no metric; ...) log-key convention"
expect_pass "passes when an existing row's metric column uses the (no metric; ...) log-key convention" "${case_metric_no_metric_marker}"

# Case 36 (#5855, fourth review round, real-doc regression guard): pins the
# real telemetry-coverage.md row at line 296, whose metric column is prose
# that does not start with the canonical No-Observability-Change: marker at
# all, but names real registered eshu_dp_* metrics inline before the first
# embedded bare pipe (the same bare-pipe-in-prose shape case 30 covers, now
# combined with a non-canonical lead-in). The signal pattern must match
# eshu_dp_* anywhere in the cell, not only at its start, so this legitimate
# row keeps passing.
case_metric_inline_names="$(init_repo case-metric-inline-names)"
cat >>"${case_metric_inline_names}/docs/public/observability/telemetry-coverage.md" <<'MD'

| inline metric names stage | go/internal/reducer/service.go:1 | `No-New-Metric: covered by eshu_dp_queue_claim_duration_seconds and eshu_dp_reducer_run_duration_seconds; this stage emits no metric of its own` | reducer runtime |
MD
git -C "${case_metric_inline_names}" add .
git -C "${case_metric_inline_names}" commit -q -m "add existing row whose metric column names real metrics inline without the canonical marker prefix"
expect_pass "passes when an existing row's metric column names real eshu_dp_* metrics inline without the canonical marker prefix" "${case_metric_inline_names}"
