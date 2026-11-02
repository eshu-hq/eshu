#!/usr/bin/env bash
# Marker-scope regression cases for test-verify-telemetry-coverage.sh (#9790).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's init_repo(), expect_pass(), expect_fail(). Extracted to keep
# scripts/test-verify-telemetry-coverage.sh under the repo's 500-line cap.
#
# The "must be registered" check skips only the metric cell of a row whose
# metric column starts with the No-Observability-Change: marker. A marker
# anywhere else hides nothing, and the rest of a marker-first row (its
# category cell, any glued row) is still checked.

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

# Case M5 (#9790 review): the marker starts the metric cell, but the category
# cell names a bogus eshu_dp_ metric. Must fail: only the metric cell is exempt.
case_marker_first_later_bogus="$(init_repo case-marker-first-later-bogus)"
cat >>"${case_marker_first_later_bogus}/docs/public/observability/telemetry-coverage.md" <<'MD'
| marker-first stage | go/internal/reducer/service.go:3 | `No-Observability-Change: retired counter prose only` | reducer runtime (see eshu_dp_ghost_metric_total) |
MD
git -C "${case_marker_first_later_bogus}" add .
git -C "${case_marker_first_later_bogus}" commit -q -m "add marker-first row with a bogus metric in the category cell"
expect_fail "fails when a marker-first row names an unregistered metric outside the metric cell" "${case_marker_first_later_bogus}"

# Case M6: the marker starts the metric cell and that cell retires a bogus
# name. Must pass: retirement prose in the metric cell stays exempt.
case_marker_first_retired="$(init_repo case-marker-first-retired)"
cat >>"${case_marker_first_retired}/docs/public/observability/telemetry-coverage.md" <<'MD'
| marker-first retired | go/internal/reducer/service.go:3 | `No-Observability-Change: eshu_dp_retired_counter_total was removed; eshu_dp_queue_claim_duration_seconds still covers the path` | reducer runtime |
MD
git -C "${case_marker_first_retired}" add .
git -C "${case_marker_first_retired}" commit -q -m "add marker-first row retiring a metric in the metric cell"
expect_pass "passes when a marker-first row retires an unregistered name inside the metric cell" "${case_marker_first_retired}"

# Case M7: the marker starts the metric cell and the category cell names only
# registered metrics. Must pass.
case_marker_first_later_clean="$(init_repo case-marker-first-later-clean)"
cat >>"${case_marker_first_later_clean}/docs/public/observability/telemetry-coverage.md" <<'MD'
| marker-first clean | go/internal/reducer/service.go:3 | `No-Observability-Change: no new signal` | reducer runtime (see eshu_dp_reducer_run_duration_seconds) |
MD
git -C "${case_marker_first_later_clean}" add .
git -C "${case_marker_first_later_clean}" commit -q -m "add marker-first row with a registered metric in the category cell"
expect_pass "passes when a marker-first row names only registered metrics outside the metric cell" "${case_marker_first_later_clean}"

# Case M8: two rows glued onto one line (as telemetry-coverage.md has today).
# The first is marker-first; the second names a bogus metric in its own metric
# cell. Must fail: a row separator ends the exemption.
case_marker_glued_bogus="$(init_repo case-marker-glued-bogus)"
cat >>"${case_marker_glued_bogus}/docs/public/observability/telemetry-coverage.md" <<'MD'
| glued first | go/internal/reducer/service.go:3 | `No-Observability-Change: prose only` | reducer runtime || glued second | go/internal/reducer/service.go:4 | `eshu_dp_ghost_metric_total` | reducer runtime |
MD
git -C "${case_marker_glued_bogus}" add .
git -C "${case_marker_glued_bogus}" commit -q -m "add glued rows with a bogus metric in the second row"
expect_fail "fails when a glued row after a marker-first row names an unregistered metric" "${case_marker_glued_bogus}"
