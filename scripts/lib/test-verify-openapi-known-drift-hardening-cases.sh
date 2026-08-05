#!/usr/bin/env bash
# Round-6 hardening cases for the known-drift self-validation in
# verify-openapi.sh (#5762 round 6, findings F2/F3/F4/F5/F7/F12/F14).
#
# Sourced, never executed: it runs in the caller's shell and reuses the
# caller's setup_repo(), write_known_drift(), run_verifier(), $tmp_root, and
# $verifier from scripts/test-verify-openapi.sh, the same way
# scripts/lib/test-verify-openapi-known-drift-cases.sh does. Split into a
# second file so neither file crosses the repo's 500-line cap, mirroring the
# scripts/lib/test-verify-telemetry-coverage-row-selection-cases.sh split.
#
# Every case below was proven against a real mutation before it was written
# here: apply the described mutation to a copy of verify-openapi.sh, confirm
# the fixture flips from blocked (exit 1) to allowed (exit 0), then revert.
# The mutation and the flip are not repeated per-case in this file; they are
# the PR's own verification evidence.

# ══════════════════════════════════════════════════════════════════════════════
# Test 25/26 — rule 3 ("every entry needs its own substantive justification",
# verify-openapi.sh:213-214) is a conjunction of two independent checks: at
# least 2 whitespace-separated tokens, AND at least one 4+ letter alphabetic
# word. Every existing decoration fixture ("#", "####", "# ---") fails BOTH
# halves at once, so either half could be deleted alone and all prior tests
# stayed green (#5762 round 6, F2). These two fixtures each satisfy exactly
# one half, isolating it: deleting the alpha-word check alone allows test 25;
# deleting the token-count check alone allows test 26.
test_known_drift_rule3_two_tokens_no_long_word_red() {
  local dir
  dir="$(setup_repo "known-drift-rule3-two-short-tokens")"

  write_known_drift "$dir" \
    '# ab cd' \
    'GET /api/v0/fake-rule3-two-short-tokens-route'

  run_verifier "$dir" "known-drift comment with 2 tokens but no 4+ letter word exits non-zero" "fail"
}

test_known_drift_rule3_single_long_word_red() {
  local dir
  dir="$(setup_repo "known-drift-rule3-single-long-word")"

  write_known_drift "$dir" \
    '# unresolvable' \
    'GET /api/v0/fake-rule3-single-long-word-route'

  run_verifier "$dir" "known-drift comment with one 4+ letter word but only 1 token exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 27/28 — the FIXME and XXX deferral markers have no fixture, unlike
# TODO/TO-DO/HACK/TBD/WIP, even though the marker pattern
# (verify-openapi.sh:161) names both and the failure message
# (verify-openapi.sh:222) and both docs advertise them as covered (#5762
# round 6, F3). Dropping either token from the pattern left the suite green.
test_known_drift_deferral_marker_fixme_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-fixme")"

  write_known_drift "$dir" \
    '# FIXME(#9999): exclude this route.' \
    'GET /api/v0/fake-fixme-route'

  run_verifier "$dir" "known-drift FIXME deferral marker exits non-zero" "fail"
}

test_known_drift_deferral_marker_xxx_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-xxx")"

  write_known_drift "$dir" \
    '# XXX: exclude this route.' \
    'GET /api/v0/fake-xxx-route'

  run_verifier "$dir" "known-drift XXX deferral marker exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 29 — every existing marker fixture is uppercase, so `rg -in` (the `-i`
# case-insensitive flag) could regress to `rg -n` with no fixture noticing,
# even though verify-openapi.sh:136 explicitly documents the marker check as
# case-insensitive (#5762 round 6, F4). This fixture is lowercase and would
# otherwise pass rule 3 (real words, 4+ letters), so it is rescued only by
# case-insensitive marker matching.
test_known_drift_deferral_marker_lowercase_red() {
  local dir
  dir="$(setup_repo "known-drift-deferral-lowercase")"

  write_known_drift "$dir" \
    '# todo(#9999): exclude this route.' \
    'GET /api/v0/fake-lowercase-todo-route'

  run_verifier "$dir" "known-drift lowercase todo deferral marker exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 30-34 — 5 of the 7 prose-deferral alternatives (verify-openapi.sh:162)
# had no fixture that pins them independently: "not written", "written yet",
# "not yet written", "predate(s)", and "later" (#5762 round 6, F5). The only
# existing fixture, "Fragment not written yet, tracked in #3781.", trips
# "not written" and "written yet" simultaneously, so deleting either
# alternative alone left the other one still matching and the suite green.
# Each fixture below contains exactly one alternative's text and none of the
# others, so it can only be rescued by the one alternative it names.
test_known_drift_prose_deferral_not_written_only_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-not-written-only")"

  write_known_drift "$dir" \
    '# Fragment not written for this route, tracked separately.' \
    'GET /api/v0/fake-prose-not-written-only-route'

  run_verifier "$dir" "known-drift prose 'not written' (alone) deferral exits non-zero" "fail"
}

test_known_drift_prose_deferral_written_yet_only_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-written-yet-only")"

  write_known_drift "$dir" \
    '# Docs written yet incomplete for this route.' \
    'GET /api/v0/fake-prose-written-yet-only-route'

  run_verifier "$dir" "known-drift prose 'written yet' (alone) deferral exits non-zero" "fail"
}

test_known_drift_prose_deferral_not_yet_written_only_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-not-yet-written-only")"

  write_known_drift "$dir" \
    '# Fragment not yet written for the new operation.' \
    'GET /api/v0/fake-prose-not-yet-written-only-route'

  run_verifier "$dir" "known-drift prose 'not yet written' deferral exits non-zero" "fail"
}

# The pattern was `\bpredates\b` (plural-only): probed directly, "predates"
# blocked (exit 1) but the singular "predate" -- the actual wording in this
# repo's own known-drift file header on main ("routes ... that predate this
# verifier") -- was allowed (exit 0) (#5762 round 6, F12). Fixed to
# `\bpredate[sd]?\b`. This fixture uses the singular form so it pins both the
# alternative's presence (F5) and the singular/plural fix (F12) in one case.
test_known_drift_prose_deferral_predate_singular_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-predate-singular")"

  write_known_drift "$dir" \
    '# Routes that predate the OpenAPI verifier lack a fragment here.' \
    'GET /api/v0/fake-prose-predate-singular-route'

  run_verifier "$dir" "known-drift prose 'predate' (singular) deferral exits non-zero" "fail"
}

test_known_drift_prose_deferral_later_only_red() {
  local dir
  dir="$(setup_repo "known-drift-prose-later-only")"

  write_known_drift "$dir" \
    '# Fragment will be documented later, once time allows.' \
    'GET /api/v0/fake-prose-later-only-route'

  run_verifier "$dir" "known-drift prose 'later' (alone) deferral exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 35 — red: an rg hard error (exit 2 -- an unreadable file, or a pattern
# an installed rg version rejects) scanning the known-drift file must fail
# the gate closed, not report a clean surface (#5762 round 6, F7). The old
# code captured both scans with `|| true`, which swallows exit 1 (no match,
# expected) and exit 2 (hard error) identically. Proven with an `rg` shim
# that fails only the two known-drift scans and delegates everything else to
# the real `rg`: the pre-fix code printed "OpenAPI surface clean" and exited
# 0 on a fixture that correctly exits 1 with a working rg; the fixed code
# exits 1 reporting the scan failure. Reaches directly into $tmp_root and
# $verifier (defined by the sourcing script) the same way test 24 in the
# sibling cases file does, because this needs a shimmed PATH that
# run_verifier() has no parameter for.
# shellcheck disable=SC2154 # tmp_root, verifier: defined by the sourcing script
test_known_drift_rg_hard_error_fails_closed_red() {
  local dir shim_dir verifier_tmp code real_rg
  dir="$(setup_repo "known-drift-rg-hard-error")"

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui'

  shim_dir="${tmp_root}/rg-shim-hard-error"
  mkdir -p "$shim_dir"
  real_rg="$(command -v rg)"
  # The single-quoted lines below are literal shell source being written to
  # the shim file, not variables to expand in THIS script.
  # shellcheck disable=SC2016
  {
    printf '#!/usr/bin/env bash\n'
    printf 'for arg in "$@"; do\n'
    printf '  case "$arg" in\n'
    printf "    *'TO[-_ ]?DO'*|*'not[[:space:]]+written'*) exit 2 ;;\n"
    printf '  esac\n'
    printf 'done\n'
    printf 'exec %q "$@"\n' "$real_rg"
  } > "${shim_dir}/rg"
  chmod +x "${shim_dir}/rg"

  verifier_tmp="${tmp_root}/verifier-tmp-rg-hard-error"
  mkdir -p "$verifier_tmp"

  set +e
  PATH="${shim_dir}:${PATH}" \
    ESHU_OPENAPI_VERIFY_REPO_ROOT="$dir" \
    ESHU_OPENAPI_VERIFY_TMPDIR="$verifier_tmp" \
    bash "$verifier" \
    > "${tmp_root}/rg-hard-error-stdout" 2> "${tmp_root}/rg-hard-error-stderr"
  code=$?
  set -e

  if [ "$code" -ne 0 ] && rg -q 'KNOWN-DRIFT SCAN FAILED' "${tmp_root}/rg-hard-error-stdout"; then
    record_pass "rg hard error (exit 2) scanning known-drift makes the gate fail closed"
  else
    record_fail "rg hard error (exit 2) scanning known-drift makes the gate fail closed (code=$code)"
  fi
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 36 — red: a justification comment byte-identical to the one
# immediately before it does not count as its own justification (#5762 round
# 6, F14). "Cannot be shared across a group of routes" was enforced only by
# position (rule 3 resets on every route/blank line), so a copy-pasted
# duplicate still counted as each route having "its own" preceding comment --
# this commit's own .github/openapi-known-drift.txt demonstrated the gap on
# its first use, with two byte-identical "# Documentation UIs -- ..." lines
# for /api/v0/docs and /api/v0/redoc.
test_known_drift_duplicate_justification_red() {
  local dir
  dir="$(setup_repo "known-drift-duplicate-justification")"

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui-one' \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui-two'

  run_verifier "$dir" "known-drift byte-identical consecutive justifications exits non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 37 — red: rule 4's duplicate check compares MEANING, not bytes -- two
# justifications differing only by one doubled internal space still count as
# the same justification (#5762 follow-up P2-1). Probed directly against the
# pre-fix code: byte-identical was blocked, but this single-internal-space
# variant was ALLOWED (exit 0), because the old check compared
# $known_drift_trimmed byte-for-byte with no whitespace normalization.
test_known_drift_duplicate_justification_internal_space_red() {
  local dir
  dir="$(setup_repo "known-drift-duplicate-internal-space")"

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui-one' \
    '# Documentation UI --  serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui-two'

  run_verifier "$dir" "known-drift justifications differing by one internal space exit non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 38 — red: rule 4's duplicate check survives the "#Foo" vs "# Foo"
# spacing variant too (#5762 follow-up P2-1). The comment-text extraction
# strips exactly one leading "#" (verify-openapi.sh's
# known_drift_comment_text="${known_drift_trimmed#\#}"), so "#Foo" and "# Foo"
# produce "Foo" and " Foo" respectively -- a leading-space difference that
# byte comparison treated as two different justifications. Probed directly:
# pre-fix, this pair was ALLOWED (exit 0).
test_known_drift_duplicate_justification_hash_spacing_red() {
  local dir
  dir="$(setup_repo "known-drift-duplicate-hash-spacing")"

  write_known_drift "$dir" \
    '#Same underlying reason text here.' \
    'GET /api/v0/docs-ui-one' \
    '# Same underlying reason text here.' \
    'GET /api/v0/docs-ui-two'

  run_verifier "$dir" "known-drift justifications differing only by '#Foo' vs '# Foo' spacing exit non-zero" "fail"
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 39/40 — the F7 exit-code check (verify-openapi.sh:
# "[ \"$known_drift_marker_rc\" -gt 1 ] || [ \"$known_drift_prose_rc\" -gt 1 ]")
# is a disjunction of two independent rc checks. Test 35's rg shim fails BOTH
# the marker scan and the prose scan at once, so deleting either half of the
# "||" alone left the suite green: mutant N5 (drop the prose-rc half) and
# mutant N6 (drop the marker-rc half) both SURVIVED the full 39-case suite
# (#5762 round 7, P1-2/P1-3) -- the same "fixture trips both halves of a
# conjunction/disjunction at once, proving neither half" class the round-6 F2
# finding named for rule 3. These two shims each fail exactly ONE scan,
# leaving the other to run against the real `rg`, so each can only be
# rescued by the specific rc check it names.
#
# Sourced fixture uses a clean, well-justified known-drift entry (no marker
# or prose deferral words), so the untouched scan legitimately returns rc=1
# (no match) through the real `rg` -- the ONLY way either shim's gate failure
# can fire is the targeted scan's simulated rc=2.
# shellcheck disable=SC2154 # tmp_root, verifier: defined by the sourcing script
test_known_drift_rg_hard_error_marker_only_fails_closed_red() {
  local dir shim_dir verifier_tmp code real_rg
  dir="$(setup_repo "known-drift-rg-hard-error-marker-only")"

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui'

  shim_dir="${tmp_root}/rg-shim-marker-only"
  mkdir -p "$shim_dir"
  real_rg="$(command -v rg)"
  # shellcheck disable=SC2016
  {
    printf '#!/usr/bin/env bash\n'
    printf 'for arg in "$@"; do\n'
    printf '  case "$arg" in\n'
    printf "    *'TO[-_ ]?DO'*) exit 2 ;;\n"
    printf '  esac\n'
    printf 'done\n'
    printf 'exec %q "$@"\n' "$real_rg"
  } > "${shim_dir}/rg"
  chmod +x "${shim_dir}/rg"

  verifier_tmp="${tmp_root}/verifier-tmp-rg-hard-error-marker-only"
  mkdir -p "$verifier_tmp"

  set +e
  PATH="${shim_dir}:${PATH}" \
    ESHU_OPENAPI_VERIFY_REPO_ROOT="$dir" \
    ESHU_OPENAPI_VERIFY_TMPDIR="$verifier_tmp" \
    bash "$verifier" \
    > "${tmp_root}/rg-hard-error-marker-only-stdout" 2> "${tmp_root}/rg-hard-error-marker-only-stderr"
  code=$?
  set -e

  if [ "$code" -ne 0 ] && rg -q 'KNOWN-DRIFT SCAN FAILED' "${tmp_root}/rg-hard-error-marker-only-stdout"; then
    record_pass "rg hard error on ONLY the marker scan makes the gate fail closed"
  else
    record_fail "rg hard error on ONLY the marker scan makes the gate fail closed (code=$code)"
  fi
}

# shellcheck disable=SC2154 # tmp_root, verifier: defined by the sourcing script
test_known_drift_rg_hard_error_prose_only_fails_closed_red() {
  local dir shim_dir verifier_tmp code real_rg
  dir="$(setup_repo "known-drift-rg-hard-error-prose-only")"

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui'

  shim_dir="${tmp_root}/rg-shim-prose-only"
  mkdir -p "$shim_dir"
  real_rg="$(command -v rg)"
  # shellcheck disable=SC2016
  {
    printf '#!/usr/bin/env bash\n'
    printf 'for arg in "$@"; do\n'
    printf '  case "$arg" in\n'
    printf "    *'not[[:space:]]+written'*) exit 2 ;;\n"
    printf '  esac\n'
    printf 'done\n'
    printf 'exec %q "$@"\n' "$real_rg"
  } > "${shim_dir}/rg"
  chmod +x "${shim_dir}/rg"

  verifier_tmp="${tmp_root}/verifier-tmp-rg-hard-error-prose-only"
  mkdir -p "$verifier_tmp"

  set +e
  PATH="${shim_dir}:${PATH}" \
    ESHU_OPENAPI_VERIFY_REPO_ROOT="$dir" \
    ESHU_OPENAPI_VERIFY_TMPDIR="$verifier_tmp" \
    bash "$verifier" \
    > "${tmp_root}/rg-hard-error-prose-only-stdout" 2> "${tmp_root}/rg-hard-error-prose-only-stderr"
  code=$?
  set -e

  if [ "$code" -ne 0 ] && rg -q 'KNOWN-DRIFT SCAN FAILED' "${tmp_root}/rg-hard-error-prose-only-stdout"; then
    record_pass "rg hard error on ONLY the prose scan makes the gate fail closed"
  else
    record_fail "rg hard error on ONLY the prose scan makes the gate fail closed (code=$code)"
  fi
}

# ══════════════════════════════════════════════════════════════════════════════
# Test 41 — red: an unjustified comment between two real justifications does
# NOT reset $known_drift_prev_justification, so a repeat of the earlier
# justification is still caught as a duplicate across the gap (#5762 follow-up
# P1-4). "# ---" here is decoration-only (fails rule 3) but is immediately
# followed by another COMMENT line, not a route line, so it never trips the
# separate UNJUSTIFIED_ENTRY check on its own -- this fixture is red for
# DUPLICATE_JUSTIFICATION alone, proving the specific invariant documented at
# verify-openapi.sh ("an unjustified comment never updates
# $known_drift_prev_justification, so it cannot mask a real duplicate two
# entries later"), not any other rule.
test_known_drift_duplicate_justification_across_unjustified_gap_red() {
  local dir
  dir="$(setup_repo "known-drift-duplicate-across-gap")"

  write_known_drift "$dir" \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui-a' \
    '# ---' \
    '# Documentation UI -- serves HTML, not an API surface, permanently excluded.' \
    'GET /api/v0/docs-ui-b'

  run_verifier "$dir" "known-drift duplicate justification across an unjustified decoration comment exits non-zero" "fail"
}
