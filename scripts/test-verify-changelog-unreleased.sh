#!/usr/bin/env bash
#
# test-verify-changelog-unreleased.sh - seeded RED/GREEN proof for
# scripts/verify-changelog-unreleased.sh (#9096, part of #9092).
#
# The clean fixture passes. Each planted violation must fail with its own
# message, so a validator that always fails or never checks a rule is caught:
#
#   two Unreleased headings   duplicate-heading-of-the-section
#   conflict markers          <<<<<<<, =======, >>>>>>> at line start
#   duplicate entry           same bold title, including a title wrapped over
#                             two lines (the joined-title key)
#   duplicate ### heading     same section heading twice
#   unterminated code fence   a fence that would hide the checks below it
#
# Two keying cases pin why the title is joined across wrapped lines:
#   - two DIFFERENT wrapped titles share a first line: a first-line key would
#     report a false duplicate; the validator must pass.
#   - the SAME wrapped title appears twice, wrapped at different columns: a
#     first-line key would miss it; the validator must fail.
#
# Run against the real tree, the validator must pass.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
validator="${repo_root}/scripts/verify-changelog-unreleased.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# write_fixture <name> <line>... writes one changelog fixture, one line per
# argument, and prints its path.
write_fixture() {
  local name="$1"
  shift
  printf '%s\n' "$@" >"${tmp_root}/${name}.md"
  printf '%s' "${tmp_root}/${name}.md"
}

# expect_pass <label> <fixture>
expect_pass() {
  local out rc=0
  out="$(bash "${validator}" "$2" 2>&1)" || rc=$?
  if [[ "${rc}" -eq 0 ]]; then
    record_pass "$1"
  else
    record_fail "$1 (expected exit 0, got ${rc})"
    printf '%s\n' "${out}" >&2
  fi
}

# expect_fail <label> <fixture> <message-fragment>
expect_fail() {
  local out rc=0
  out="$(bash "${validator}" "$2" 2>&1)" || rc=$?
  if [[ "${rc}" -eq 0 ]]; then
    record_fail "$1 (expected a failure, got exit 0)"
  elif [[ "${out}" != *"$3"* ]]; then
    record_fail "$1 (failed without the message: $3)"
    printf '%s\n' "${out}" >&2
  else
    record_pass "$1"
  fi
}

clean="$(write_fixture clean \
  '# Changelog' '' '## Unreleased' '' \
  '### Section one' '' \
  '- **Alpha entry.** Text.' '' \
  '- **A wrapped title that runs across' \
  '  two lines.** Text.' '' \
  '### Section two' '' \
  '- **Bravo entry.** Text.' '' \
  '```text' '<<<<<<< fenced example, not a marker' '```')"
expect_pass "clean file passes" "${clean}"

expect_fail "two Unreleased headings fail" "$(write_fixture two-unreleased \
  '## Unreleased' '' '### One' '' '- **Alpha.** x' '' '## Unreleased' '' '### Two' '' '- **Bravo.** y')" \
  "exactly one '## Unreleased' heading"

expect_fail "no Unreleased heading fails" "$(write_fixture no-unreleased \
  '# Changelog' '' '### One' '' '- **Alpha.** x')" \
  "exactly one '## Unreleased' heading"

expect_fail "start marker fails" "$(write_fixture marker-start \
  '## Unreleased' '' '<<<<<<< HEAD' '- **Alpha.** x')" "conflict marker"
expect_fail "separator marker fails" "$(write_fixture marker-mid \
  '## Unreleased' '' '- **Alpha.** x' '=======' '- **Bravo.** y')" "conflict marker"
expect_fail "end marker fails" "$(write_fixture marker-end \
  '## Unreleased' '' '- **Alpha.** x' '>>>>>>> feature')" "conflict marker"

expect_fail "duplicate single-line entry fails" "$(write_fixture dup-entry \
  '## Unreleased' '' '### One' '' '- **Alpha.** x' '' '### Two' '' '- **Alpha.** y')" \
  "duplicate entry"

expect_fail "duplicate wrapped entry fails" "$(write_fixture dup-wrapped \
  '## Unreleased' '' '### One' '' \
  '- **A wrapped title that runs across' '  two lines.** x' '' '### Two' '' \
  '- **A wrapped title that runs across two lines.** y')" "duplicate entry"

expect_fail "duplicate wrapped entry at different wraps fails" "$(write_fixture dup-rewrap \
  '## Unreleased' '' '### One' '' \
  '- **A wrapped title that runs' '  across two lines.** x' '' '### Two' '' \
  '- **A wrapped title that runs across' '  two lines.** y')" "duplicate entry"

expect_pass "different wrapped titles with a shared first line pass" "$(write_fixture shared-first-line \
  '## Unreleased' '' '### One' '' \
  '- **A shared opening that runs across' '  one ending.** x' '' '### Two' '' \
  '- **A shared opening that runs across' '  another ending.** y')"

expect_fail "duplicate ### heading fails" "$(write_fixture dup-heading \
  '## Unreleased' '' '### Same heading' '' '- **Alpha.** x' '' '### Same heading' '' '- **Bravo.** y')" \
  "duplicate heading"

expect_fail "unterminated code fence fails" "$(write_fixture open-fence \
  '## Unreleased' '' '### One' '' '```text' '- **Alpha.** x')" "unterminated code fence"

expect_fail "a violation inside a long file still fails" "$(write_fixture dup-after-fence \
  '## Unreleased' '' '### One' '' '```text' '- **Fenced.** ignored' '```' '' \
  '- **Alpha.** x' '' '- **Alpha.** y')" "duplicate entry"

expect_pass "a conflict marker inside a ~~~ fence is skipped" "$(write_fixture tilde-fence \
  '## Unreleased' '' '### One' '' '~~~text' '<<<<<<< fenced example, not a marker' '~~~' '' \
  '- **Alpha.** x')"

# An entry whose bold title never closes ends at a blank line or a heading,
# so the opener keys on its own text and cannot swallow the next paragraph.
expect_fail "a blank line ends an entry whose title never closes" "$(write_fixture blank-ends-entry \
  '## Unreleased' '' '### One' '' '- **Same key' '' 'text.** later' '' '- **Same key** y')" \
  "duplicate entry"

expect_fail "a heading ends an entry whose title never closes" "$(write_fixture heading-ends-entry \
  '## Unreleased' '' '### One' '' '- **Same key' '### Two' '- **Same key** y')" \
  "duplicate entry"

# A missing file is a usage error, not a pass.
if bash "${validator}" "${tmp_root}/does-not-exist.md" >/dev/null 2>&1; then
  record_fail "missing file fails"
else
  record_pass "missing file fails"
fi

# The repository's own CHANGELOG.md passes.
expect_pass "repository CHANGELOG.md passes" "${repo_root}/CHANGELOG.md"

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
