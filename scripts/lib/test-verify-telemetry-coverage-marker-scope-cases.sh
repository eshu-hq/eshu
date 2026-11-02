#!/usr/bin/env bash
# Marker-scope regression cases for test-verify-telemetry-coverage.sh (#9790).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's init_repo(), expect_pass(), expect_fail(). Extracted to keep
# scripts/test-verify-telemetry-coverage.sh under the repo's 500-line cap.
#
# The "must be registered" check skips a row only when the No-Observability-
# Change: marker starts the row's metric column. A marker anywhere else must
# not hide the row's eshu_dp_* names from the check.

# Case M1: the marker follows a real metric name inside the metric cell and
# the row also names a bogus eshu_dp_ metric. Must fail: the cell does not
# start with the marker, so the bogus name has to be registered.
case_marker_mixed_bogus="$(init_repo case-marker-mixed-bogus)"
cat >>"${case_marker_mixed_bogus}/docs/public/observability/telemetry-coverage.md" <<'MD'
| mixed stage | go/internal/reducer/service.go:3 | `eshu_dp_queue_claim_duration_seconds` plus No-Observability-Change: helper moved verbatim, still counted by `eshu_dp_ghost_metric_total` | reducer runtime |
MD
git -C "${case_marker_mixed_bogus}" add .
git -C "${case_marker_mixed_bogus}" commit -q -m "add mixed marker row with an unregistered metric"
expect_fail "fails when a mid-cell marker row names an unregistered metric" "${case_marker_mixed_bogus}"

# Case M2: the marker sits in the category cell, not the metric cell, next to
# a bogus metric in the metric cell. Must fail for the same reason.
case_marker_other_cell="$(init_repo case-marker-other-cell)"
cat >>"${case_marker_other_cell}/docs/public/observability/telemetry-coverage.md" <<'MD'
| other-cell stage | go/internal/reducer/service.go:3 | `eshu_dp_ghost_metric_total` | No-Observability-Change: reducer runtime |
MD
git -C "${case_marker_other_cell}" add .
git -C "${case_marker_other_cell}" commit -q -m "add marker in category cell with an unregistered metric"
expect_fail "fails when the marker is outside the metric cell and the metric is unregistered" "${case_marker_other_cell}"

# Case M3: the same mid-cell marker row, but every metric it names is
# registered. Must pass: the stricter filter rejects only unregistered names.
case_marker_mixed_clean="$(init_repo case-marker-mixed-clean)"
cat >>"${case_marker_mixed_clean}/docs/public/observability/telemetry-coverage.md" <<'MD'
| mixed clean stage | go/internal/reducer/service.go:3 | `eshu_dp_queue_claim_duration_seconds` plus No-Observability-Change: helper moved verbatim, still counted by `eshu_dp_reducer_run_duration_seconds` | reducer runtime |
MD
git -C "${case_marker_mixed_clean}" add .
git -C "${case_marker_mixed_clean}" commit -q -m "add mixed marker row with registered metrics only"
expect_pass "passes when a mid-cell marker row names only registered metrics" "${case_marker_mixed_clean}"

# Case M4: the marker starts the metric cell without a leading backtick and
# retires a name that is not registered. Must pass: the supported bare form.
case_marker_bare="$(init_repo case-marker-bare)"
cat >>"${case_marker_bare}/docs/public/observability/telemetry-coverage.md" <<'MD'
| bare marker stage | go/internal/reducer/service.go:3 | No-Observability-Change: retired eshu_dp_retired_counter_total no longer emits | reducer runtime |
MD
git -C "${case_marker_bare}" add .
git -C "${case_marker_bare}" commit -q -m "add bare marker row retiring a metric"
expect_pass "passes when the marker starts the metric cell without a backtick" "${case_marker_bare}"
