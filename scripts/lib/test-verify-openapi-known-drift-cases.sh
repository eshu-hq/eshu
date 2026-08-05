#!/usr/bin/env bash
# Known-drift self-validation regression cases for test-verify-openapi.sh
# (#5762: the original TODO/unjustified/justified trio, plus the follow-up
# cases that closed the marker-plural, prose-deferral, per-route
# justification, and bare-# gaps).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's setup_repo(), write_handler(), write_openapi_path(),
# write_known_drift(), and run_verifier(). Extracted to keep the orchestrator
# under the repo's 500-line cap, mirroring the
# scripts/lib/test-verify-telemetry-coverage-row-selection-cases.sh split.

# ══════════════════════════════════════════════════════════════════════════════
# Test 11 — red: known-drift entry carrying a TODO deferral marker exits
# non-zero (#5762). A TODO asserts "fixable, later," which contradicts
# "permanent, intentional exclusion" -- this is exactly the shape that hid
# POST /api/v0/code/visualize's real gap behind "TODO(#3781): add ... fragment"
# for the six weeks since #3781 was filed. The fixture is deliberately
# prose-free (no "later"/"pending"/etc)
# so it exercises only the marker rule, not the separate prose rule (#5762
# round 3, P1-A) -- a fixture that trips both rules cannot prove the marker
# rule alone is doing anything.
test_known_drift_deferral_marker_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral")"

  write_known_drift "$dir" \
    '# TODO(#9999): add the real fragment for this route.' \
    'GET /api/v0/fake-deferred-route'

  run_verifier "$dir" "known-drift TODO deferral marker exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 11b/11c/11d — red: the HACK, TBD, and WIP deferral markers each exit
# non-zero on their own (#5762 round 3, P1-A). Before this round, none of
# these three markers had fixture coverage at all -- reverting the marker
# pattern to drop them was invisible to the suite.
test_known_drift_deferral_marker_hack_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-hack")"

  write_known_drift "$dir" \
    '# HACK: exclude this route.' \
    'GET /api/v0/fake-hack-route'

  run_verifier "$dir" "known-drift HACK deferral marker exits non-zero" "fail"
}

test_known_drift_deferral_marker_tbd_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-tbd")"

  write_known_drift "$dir" \
    '# TBD: exclude this route.' \
    'GET /api/v0/fake-tbd-route'

  run_verifier "$dir" "known-drift TBD deferral marker exits non-zero" "fail"
}

test_known_drift_deferral_marker_wip_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-wip")"

  write_known_drift "$dir" \
    '# WIP: exclude this route.' \
    'GET /api/v0/fake-wip-route'

  run_verifier "$dir" "known-drift WIP deferral marker exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 11e — red: the hyphenated "TO-DO" spelling is the same token as "TODO"
# and must trip the marker rule too (#5762 round 3, P2-3). The #5762 commit
# message claimed this was already caught; it was not until this round added
# TO[-_ ]?DO to the pattern.
test_known_drift_deferral_marker_todo_dash_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-todo-dash")"

  write_known_drift "$dir" \
    '# TO-DO(#9999): exclude this route.' \
    'GET /api/v0/fake-todo-dash-route'

  run_verifier "$dir" "known-drift TO-DO deferral marker exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 12 — red: known-drift route entry with no preceding justification
# comment exits non-zero (#5762). A blank line resets the justification state,
# so the entry below is bare even though a comment appears earlier in the
# file.
test_known_drift_unjustified_entry_red() {
  local dir
  dir="$(setup_repo "known-drift-unjustified")"

  write_known_drift "$dir" \
    '# Header comment, but a blank line separates it from the entry below.' \
    '' \
    'GET /api/v0/fake-bare-route'

  run_verifier "$dir" "known-drift entry with no preceding comment exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 13 — green: a well-justified known-drift entry with no deferral marker
# exits 0, and still subtracts its route from the HandleFunc/OpenAPI diff
# (#5762).
test_known_drift_justified_green() {
  local dir
  dir="$(setup_repo "known-drift-justified")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /api/v0/docs-ui", h.docsUI)' \
    '}'

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui'

  run_verifier "$dir" "known-drift justified entry with no deferral marker exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 14 — red: known-drift deferral marker in its plural form still trips
# (#5762 follow-up). The original marker regex used a trailing \b, which a
# plural like "TODOs" defeats -- the trailing boundary never matches because
# 's' is a word character too, so "s?"-shaped drift slipped through under the
# same "TODO(#3781): add ... fragment" camouflage the singular form is meant
# to catch. The comment is a real, multi-token, prose-free justification (no
# "later"/etc) so this fixture is rescued ONLY by rule 1 (the marker), never
# by rule 3 (justification) -- #5762 round 4 found the original single-token
# fixture ("# TODOs.") fails rule 3 on its own (fewer than two tokens), so
# dropping "s?" from the marker pattern entirely still left the suite red via
# UNJUSTIFIED_ENTRY, and the marker regression went undetected. A fixture that
# can be rescued by a different rule proves nothing about the rule it names.
test_known_drift_deferral_marker_plural_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-plural")"

  write_known_drift "$dir" \
    '# TODOs are tracked in the issue backlog system.' \
    'GET /api/v0/fake-deferred-plural-route'

  run_verifier "$dir" "known-drift plural TODOs deferral marker exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 15 — red: a prose deferral phrase with no TODO/FIXME/XXX-style marker
# still trips (#5762 follow-up). This fixture is a synthetic prose-only
# deferral, not a quote of history: POST /api/v0/code/visualize's real
# known-drift entry used a TODO marker (`# TODO(#3781): add
# openapi_paths_code_visualize fragment for this route.`), so rule 1 (the
# marker check) alone would already have caught it -- rule 2 exists for the
# prospective case, a future entry that defers itself in prose without ever
# using a marker word (#5762 round 6, F11 correction).
test_known_drift_prose_deferral_not_written_yet_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-not-written-yet")"

  write_known_drift "$dir" \
    '# Fragment not written yet, tracked in #3781.' \
    'GET /api/v0/fake-prose-not-written-route'

  run_verifier "$dir" "known-drift prose 'not written yet' deferral exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 16 — red: a second prose deferral phrase (no TODO/FIXME/XXX marker,
# and a different shape than test 15) still trips.
test_known_drift_prose_deferral_pending_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-pending")"

  write_known_drift "$dir" \
    '# Pending: fragment not authored.' \
    'GET /api/v0/fake-prose-pending-route'

  run_verifier "$dir" "known-drift prose 'pending' deferral exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 17 — red: a route appended directly after an already-justified route,
# with no blank line and no comment of its own, exits non-zero (#5762
# follow-up). Justification is scoped to exactly the one route line that
# follows a comment; a second route riding in on the first route's comment is
# exactly the "bare route appended silently" shape the file's own header
# claims cannot happen.
test_known_drift_route_after_justified_route_red() {
  local dir
  dir="$(setup_repo "known-drift-route-after-justified")"

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui' \
    'GET /api/v0/sneaky-undocumented-route'

  run_verifier "$dir" "known-drift route appended after a justified route exits non-zero" "fail"
}

# Test 18 (a bare "#" comment counts as unjustified) was removed in #5762
# round 6, F6: it used the identical fixture shape as test 24 below (a bare
# "#" followed by a route) with strictly less coverage -- test 24 additionally
# pins the exact UNJUSTIFIED_ENTRY message text against the same fixture, so
# test 18 asserted nothing test 24 does not already assert.

# ══════════════════════════════════════════════════════════════════════════════
# Test 19 — green: a route whose own PATH contains a marker word (here,
# "todo") is not blocked by the marker rule when it carries a real,
# marker-free justification comment (#5762 round 3, P2-2). Before this round
# the marker scan read every line of the file, including route lines, so a
# route like this could never be legitimately excluded -- the marker rule is
# now anchored to comment lines only.
test_known_drift_route_path_contains_marker_word_green() {
  local dir
  dir="$(setup_repo "known-drift-route-path-todo")"

  write_known_drift "$dir" \
    '# Serves a static kanban board page, not an API operation.' \
    'GET /api/v0/todo-board'

  run_verifier "$dir" "known-drift route path containing 'todo' with real justification exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 20 — green: a justification comment containing "wipes" (which embeds
# the WIP marker as a substring) does not trip the marker rule, because the
# marker pattern requires a trailing word boundary after the optional plural
# "s" (#5762 round 3, P2-2). This is the exact false positive the round-3
# review named: "Route wipes the render cache, not an API operation." must be
# usable as a real justification for GET /api/v0/cache/wipe.
test_known_drift_justification_contains_wipes_green() {
  local dir
  dir="$(setup_repo "known-drift-justification-wipes")"

  write_known_drift "$dir" \
    '# Route wipes the render cache, not an API operation.' \
    'GET /api/v0/cache/wipe'

  run_verifier "$dir" "known-drift justification containing 'wipes' exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 21 — red: a decoration-only comment ("####") does not count as a
# substantive justification (#5762 round 3, P2-1). The prior rule only
# counted non-space characters (>= 3), which "###" (after stripping one
# leading "#") satisfied.
test_known_drift_decoration_only_hashes_red() {
  local dir
  dir="$(setup_repo "known-drift-decoration-hashes")"

  write_known_drift "$dir" \
    '####' \
    'GET /api/v0/fake-decoration-hashes-route'

  run_verifier "$dir" "known-drift '####' decoration-only comment exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 22 — red: a decoration-only comment ("# ---") does not count as a
# substantive justification either (#5762 round 3, P2-1).
test_known_drift_decoration_only_dashes_red() {
  local dir
  dir="$(setup_repo "known-drift-decoration-dashes")"

  write_known_drift "$dir" \
    '# ---' \
    'GET /api/v0/fake-decoration-dashes-route'

  run_verifier "$dir" "known-drift '# ---' decoration-only comment exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 23 — green: a known-drift route line with leading whitespace is still
# recognized and subtracted from the HandleFunc/OpenAPI diff (#5762 round 4,
# P2-1). The self-validation loop (verify-openapi.sh:170) trims each line
# before classifying it as a comment or a route, but the separate consumer
# pass that builds the subtraction set (verify-openapi.sh:250-256) must trim
# identically -- an untrimmed route line never string-equals its HandleFunc
# counterpart under `comm -23`, so an indented entry would pass self-
# validation as justified yet silently fail to suppress the route, and the
# gate would report it as live drift instead of a known exclusion.
test_known_drift_indented_route_trimmed_green() {
  local dir
  dir="$(setup_repo "known-drift-indented-route")"

  write_handler "$dir" "h.go" \
    'func (h *H) Mount(mux *http.ServeMux) {' \
    '	mux.HandleFunc("GET /api/v0/indented-thing", h.indentedThing)' \
    '}'

  write_known_drift "$dir" \
    '# Computed at runtime in a package the scanner does not walk.' \
    '   GET /api/v0/indented-thing'

  run_verifier "$dir" "known-drift indented route line is trimmed and subtracted, exits 0" "pass"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 24 — red: the UNJUSTIFIED_ENTRY failure message states the actual
# justification rule (#5762 round 4, P2-2). The message previously read
# "substantive (3+ character) comment", but the enforced rule
# (verify-openapi.sh:182-188) is "at least two whitespace-separated tokens,
# one of them 4+ alphabetic letters" -- a comment like "# ---" already meets a
# literal 3+ character threshold, so the old message told a rejected author
# their input already satisfied the stated bar, misdirecting them away from
# the real fix. Nothing asserted the message text, so it drifted silently in
# the same round (#5762 round 3, P2-1) that tightened the rule; this test
# pins the wording directly against the verifier's own stdout so it cannot
# drift unnoticed again. Invoked outside run_verifier() (which only checks the
# exit code) because this needs the stdout body too, so it reaches directly
# into $tmp_root and $verifier, defined by the sourcing script
# (test-verify-openapi.sh) alongside the other shared helpers this file's
# header already documents reusing.
# shellcheck disable=SC2154 # tmp_root, verifier: defined by the sourcing script
test_known_drift_unjustified_message_states_actual_rule_red() {
  local dir verifier_tmp code
  dir="$(setup_repo "known-drift-message-wording")"
  verifier_tmp="${tmp_root}/verifier-tmp-message-wording"
  mkdir -p "$verifier_tmp"

  write_known_drift "$dir" \
    '#' \
    'GET /api/v0/fake-message-wording-route'

  set +e
  ESHU_OPENAPI_VERIFY_REPO_ROOT="$dir" \
    ESHU_OPENAPI_VERIFY_TMPDIR="$verifier_tmp" \
    bash "$verifier" \
    > "${tmp_root}/message-stdout" 2> "${tmp_root}/message-stderr"
  code=$?
  set -e

  if [ "$code" -ne 0 ] \
    && rg -q 'at least two words, one of them 4\+ letters' "${tmp_root}/message-stdout" \
    && ! rg -q '3\+ character' "${tmp_root}/message-stdout"; then
    record_pass "UNJUSTIFIED_ENTRY message states the actual two-word/4-letter rule"
  else
    record_fail "UNJUSTIFIED_ENTRY message states the actual two-word/4-letter rule (code=$code)"
  fi
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 46/47 — red: the seventh prose alternative, "to be added" / "to be
# written", is named in the pattern, in the PROSE_DEFERRAL failure message,
# and in docs/internal/design/3738-openapi-discipline.md, but nothing pinned
# it: deleting `|to be (added|written)` from known_drift_prose_pattern left
# the whole suite green (#5762 round 8, P1-1). Every other alternative got an
# isolating fixture in round 6; this one was missed. Each fixture below
# contains exactly one half of the alternation and trips no other alternative
# ("added"/"written" here never form "not written" or "written yet"), so
# dropping either half of `(added|written)` reds exactly one case.
test_known_drift_prose_deferral_to_be_added_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-to-be-added")"

  write_known_drift "$dir" \
    '# Fragment to be added for this operation.' \
    'GET /api/v0/fake-prose-to-be-added-route'

  run_verifier "$dir" "known-drift prose 'to be added' deferral exits non-zero" "fail"
}

test_known_drift_prose_deferral_to_be_written_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-to-be-written")"

  write_known_drift "$dir" \
    '# Fragment to be written for this operation.' \
    'GET /api/v0/fake-prose-to-be-written-route'

  run_verifier "$dir" "known-drift prose 'to be written' deferral exits non-zero" "fail"
}
