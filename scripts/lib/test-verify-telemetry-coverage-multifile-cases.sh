#!/usr/bin/env bash
# Multi-file doc-row regression cases for test-verify-telemetry-coverage.sh
# (#6681).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's init_repo(), expect_pass(), expect_fail(), run_verifier(),
# record_pass(), record_fail(). Extracted to keep
# scripts/test-verify-telemetry-coverage.sh under the repo's 500-line cap,
# mirroring the scripts/lib/test-verify-telemetry-coverage-row-selection-cases.sh
# split.
#
# #6681: check (3) ("a new stage file must be covered by a doc row with a
# real signal") built its file-to-signal table with a single-token regex
# that could not parse a comma-separated, multi-file doc-row cell (e.g.
# "a.go, b.go, c.go, d.go") at all -- the match failed outright, so the row
# contributed NOTHING to the table, and every file the row names, not just
# the ones after the first comma, was reported "not covered" even though
# (3b) already validated the same row correctly. Cases 39-41 are the
# positive fix: a new stage file named in any position of a multi-file row,
# including as a bare filename that inherits a prior token's directory, now
# counts as covered.
#
# Fixing that required replacing the old `rg -F " $file"` unanchored
# substring coverage lookup with an exact/glob-aware one, which surfaced two
# adjacent defects sharing the same "not anchored to the whole token" root
# cause. Cases 42-44 pin those:
#   (a) a row naming an unrelated file that happens to be a literal PREFIX
#       of a new file's name could report the new file "covered" (case 42).
#   (b) a glob row's '*' was compared literally, not expanded, so a new file
#       that genuinely satisfies the glob was reported uncovered (case 43) --
#       and the fix for that must match compgen -G's own "'*' does not cross
#       a '/' boundary" semantics exactly, or a new file one directory below
#       an existing glob row gets wrongly counted as covered, reopening the
#       same (3)/(3b) divergence class from a different angle (case 44,
#       review finding).

# Case 39 (#6681, "#6634 shape"): a new stage file named THIRD of FOUR in a
# comma-separated multi-file doc row. multifile_a/b/d.go already exist at
# base; only multifile_c.go is added in the diff commit, together with the
# doc row naming all four. Before the fix, the row's file_path extraction
# failed outright (multi-token cell), so multifile_c.go had no entry in
# doc_row_signals_tmp at all and was reported "not covered".
case_multifile_third="$(init_repo case-multifile-third)"
mkdir -p "${case_multifile_third}/go/internal/reducer/multifile"
printf 'package multifile\n' >"${case_multifile_third}/go/internal/reducer/multifile/multifile_a.go"
printf 'package multifile\n' >"${case_multifile_third}/go/internal/reducer/multifile/multifile_b.go"
printf 'package multifile\n' >"${case_multifile_third}/go/internal/reducer/multifile/multifile_d.go"
git -C "${case_multifile_third}" add .
git -C "${case_multifile_third}" commit -q -m "seed three pre-existing multifile siblings"
printf 'package multifile\n' >"${case_multifile_third}/go/internal/reducer/multifile/multifile_c.go"
cat >>"${case_multifile_third}/docs/public/observability/telemetry-coverage.md" <<'MD'

| multifile stage | go/internal/reducer/multifile/multifile_a.go, go/internal/reducer/multifile/multifile_b.go, go/internal/reducer/multifile/multifile_c.go, go/internal/reducer/multifile/multifile_d.go | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_multifile_third}" add .
git -C "${case_multifile_third}" commit -q -m "add new stage file named third of four in a multi-file doc row"
expect_pass "covers a new stage file named third of four in a multi-file doc row" "${case_multifile_third}"

# Case 40 (#6681): a new stage file named FIRST of TWO in a multi-file row --
# pins that coverage does not depend on the new file being the LAST token
# either (the one shape a naive "does the cell end with $file" fix could
# have special-cased instead of genuinely parsing every position).
case_multifile_first="$(init_repo case-multifile-first)"
mkdir -p "${case_multifile_first}/go/internal/reducer/multifile2"
printf 'package multifile2\n' >"${case_multifile_first}/go/internal/reducer/multifile2/sibling.go"
git -C "${case_multifile_first}" add .
git -C "${case_multifile_first}" commit -q -m "seed one pre-existing multifile2 sibling"
printf 'package multifile2\n' >"${case_multifile_first}/go/internal/reducer/multifile2/leader.go"
cat >>"${case_multifile_first}/docs/public/observability/telemetry-coverage.md" <<'MD'

| multifile2 stage | go/internal/reducer/multifile2/leader.go, go/internal/reducer/multifile2/sibling.go | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_multifile_first}" add .
git -C "${case_multifile_first}" commit -q -m "add new stage file named first of two in a multi-file doc row"
expect_pass "covers a new stage file named first of two in a multi-file doc row" "${case_multifile_first}"

# Case 41 (#6681): the new file is named as a BARE filename (no directory) in
# the second comma-separated part, which must inherit the directory of the
# first part -- the exact shape the real doc uses at line 890 ("go/internal
# /query/investigation_workflow*.go, investigation_packet*.go"). Proves the
# shared resolve_row_cell_paths_into directory-inheritance logic (previously
# (3b)-only) now also feeds check (3)'s coverage table.
case_multifile_inherit="$(init_repo case-multifile-inherit)"
mkdir -p "${case_multifile_inherit}/go/internal/reducer/multifile3"
printf 'package multifile3\n' >"${case_multifile_inherit}/go/internal/reducer/multifile3/workflow_seed.go"
git -C "${case_multifile_inherit}" add .
git -C "${case_multifile_inherit}" commit -q -m "seed one pre-existing multifile3 workflow file"
printf 'package multifile3\n' >"${case_multifile_inherit}/go/internal/reducer/multifile3/packet_new.go"
cat >>"${case_multifile_inherit}/docs/public/observability/telemetry-coverage.md" <<'MD'

| multifile3 stage | go/internal/reducer/multifile3/workflow_seed.go, packet_new.go | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_multifile_inherit}" add .
git -C "${case_multifile_inherit}" commit -q -m "add new stage file as a bare filename inheriting the previous token's directory"
expect_pass "covers a new stage file named as a bare filename inheriting the prior token's directory" "${case_multifile_inherit}"

# Case 42 (#6681 defect (a)): an unrelated, PRE-EXISTING row names
# "sync.gox" (not a stage file itself -- it does not match the *.go
# stage-owner allowlist, so it is never a candidate new stage). A brand new,
# genuinely undocumented "sync.go" is added alongside it. The old
# `rg -F " $file"` lookup searched for the literal substring
# " go/internal/reducer/multifile4/sync.go" inside
# doc_row_signals_tmp, which is unanchored on the right: that substring IS a
# literal prefix of the stored line for the "sync.gox" row
# (" .../sync.gox 1" starts with " .../sync.go" before its own trailing
# "x 1"), so the unrelated row wrongly "covered" the new file and the
# verifier passed when it should have failed -- a false negative that hides
# a real, undocumented stage. Must fail after the fix.
case_substring_collision="$(init_repo case-substring-collision)"
mkdir -p "${case_substring_collision}/go/internal/reducer/multifile4"
printf 'package multifile4\n' >"${case_substring_collision}/go/internal/reducer/multifile4/sync.gox"
cat >>"${case_substring_collision}/docs/public/observability/telemetry-coverage.md" <<'MD'

| unrelated gox stage | go/internal/reducer/multifile4/sync.gox | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_substring_collision}" add .
git -C "${case_substring_collision}" commit -q -m "seed unrelated row naming a sync.gox file"
printf 'package multifile4\n' >"${case_substring_collision}/go/internal/reducer/multifile4/sync.go"
git -C "${case_substring_collision}" add .
git -C "${case_substring_collision}" commit -q -m "add a new sync.go stage file with no doc row of its own"
expect_fail "does not let an unrelated row's file, which merely prefixes a new file's name, report it covered" "${case_substring_collision}"

# Case 43 (#6681 defect (b)): an EXISTING glob-form row
# ("go/internal/reducer/globstage5/*.go") already covers a seed file. A
# brand new file matching that same glob, in the SAME directory, is added.
# The old lookup stored the glob text literally and compared it as a fixed
# string, so it could never match a new file's real name; the fix expands
# the glob (compgen -G, the same mechanism (3b) already uses to validate the
# row's own target) and checks the new file is really in that expansion.
case_glob_covers_new="$(init_repo case-glob-covers-new)"
mkdir -p "${case_glob_covers_new}/go/internal/reducer/globstage5"
printf 'package globstage5\n' >"${case_glob_covers_new}/go/internal/reducer/globstage5/seed.go"
cat >>"${case_glob_covers_new}/docs/public/observability/telemetry-coverage.md" <<'MD'

| globstage5 stage | go/internal/reducer/globstage5/*.go | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_glob_covers_new}" add .
git -C "${case_glob_covers_new}" commit -q -m "seed globstage5 with an existing glob-covered row"
printf 'package globstage5\n' >"${case_glob_covers_new}/go/internal/reducer/globstage5/new_addition.go"
git -C "${case_glob_covers_new}" add .
git -C "${case_glob_covers_new}" commit -q -m "add a new file matching the existing glob-form doc row"
expect_pass "covers a new stage file that matches an existing glob-form doc row" "${case_glob_covers_new}"

# Case 44 (#6681 defect (b), review follow-up): the boundary the case 43 fix
# must respect. The SAME glob row ("globstage6/*.go") exists, but the new
# file is added ONE DIRECTORY BELOW it (globstage6/subdir/nested.go), not
# directly in globstage6/. Shell filename globbing (compgen -G, what (3b)
# uses to validate this exact row) does not let '*' cross a '/' boundary, so
# this file is NOT really in the glob's expansion -- unlike bash `[[ ]]`
# pattern matching, where an unqualified '*' DOES match '/' and would have
# wrongly "covered" it. (The new-stage allowlist itself, a bash `case`
# pattern, still classifies the nested file as a candidate stage per case
# 4c's precedent, so it reaches check (3) at all.) Must fail: the file is
# genuinely undocumented.
case_glob_boundary="$(init_repo case-glob-boundary)"
mkdir -p "${case_glob_boundary}/go/internal/reducer/globstage6"
printf 'package globstage6\n' >"${case_glob_boundary}/go/internal/reducer/globstage6/seed.go"
cat >>"${case_glob_boundary}/docs/public/observability/telemetry-coverage.md" <<'MD'

| globstage6 stage | go/internal/reducer/globstage6/*.go | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
MD
git -C "${case_glob_boundary}" add .
git -C "${case_glob_boundary}" commit -q -m "seed globstage6 with an existing glob-covered row"
mkdir -p "${case_glob_boundary}/go/internal/reducer/globstage6/subdir"
printf 'package subdir\n' >"${case_glob_boundary}/go/internal/reducer/globstage6/subdir/nested.go"
git -C "${case_glob_boundary}" add .
git -C "${case_glob_boundary}" commit -q -m "add a new file one directory below an existing glob-form doc row"
expect_fail "does not let a glob row cover a new file one directory below it" "${case_glob_boundary}"
