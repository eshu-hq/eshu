#!/usr/bin/env bash
# Rename regression cases for test-verify-telemetry-coverage.sh (#6696).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's init_repo(), expect_pass(), expect_fail(), run_verifier(),
# record_pass(), record_fail(). Extracted to keep
# scripts/test-verify-telemetry-coverage.sh under the repo's 500-line cap,
# mirroring the row-selection/multifile/shallow splits.
#
# A pure rename (git mv with no content change) plus a lockstep doc repoint
# is not a new pipeline stage: the verifier must pair the rename instead of
# reporting every moved file as uncovered (case R1). A rename WITHOUT the
# doc repoint must still fail on the stale row (case R2 pins the (3b)
# stale-target half, which rename pairing must not mask).

# Case R1 (#6696 leaf 1): rename a stage file and repoint its doc rows.
# Must pass: no new stage exists, only a moved one.
case_rename_repointed="$(init_repo case-rename-repointed)"
git -C "${case_rename_repointed}" mv go/internal/reducer/service.go go/internal/reducer/service_renamed.go
# `sed -i.bak` is the form both BSD (macOS) and GNU sed accept; a bare `sed -i`
# takes the script as the backup suffix on BSD sed (#9316). Drop the backup so
# `git add .` never commits it.
rename_doc="${case_rename_repointed}/docs/public/observability/telemetry-coverage.md"
sed -i.bak 's|go/internal/reducer/service\.go|go/internal/reducer/service_renamed.go|g' "${rename_doc}"
rm -f "${rename_doc}.bak"
git -C "${case_rename_repointed}" add .
git -C "${case_rename_repointed}" commit -q -m "rename stage file with lockstep doc repoint"
expect_pass "passes when a stage file is renamed with its doc rows repointed" "${case_rename_repointed}"

# Case R2 (#6696 leaf 1): rename a stage file but leave its doc rows behind.
# Must fail: the rows name a path that no longer exists.
case_rename_stale="$(init_repo case-rename-stale)"
git -C "${case_rename_stale}" mv go/internal/reducer/service.go go/internal/reducer/service_renamed.go
git -C "${case_rename_stale}" add .
git -C "${case_rename_stale}" commit -q -m "rename stage file without doc repoint"
expect_fail "fails when a renamed stage file's doc rows are left behind" "${case_rename_stale}"
