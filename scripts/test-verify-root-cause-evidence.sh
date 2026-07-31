#!/usr/bin/env bash
# Behaviour tests for verify-root-cause-evidence.sh.
#
# Each case builds a throwaway git repo, commits a base, then commits a change
# and runs the gate against it. Testing through real commits rather than a
# stubbed diff matters: the gate's whole contract is "added lines only", and a
# stub cannot catch a regression that starts reading whole-file content.
#
# The gate is advisory, so it exits 0 in every case. These tests assert on the
# REPORTED VERDICT in its output, not the exit code. When the gate is promoted
# to blocking, the exit-code assertions become meaningful and should be added
# here in the same change.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="$script_dir/verify-root-cause-evidence.sh"

failures=0
cases_run=0

report_pass() { printf '  ok  %s\n' "$1"; }
report_fail() { printf '  FAIL %s\n    %s\n' "$1" "$2"; failures=$((failures + 1)); }

# Runs the gate over a repo whose HEAD commit adds $2 to $1, and echoes output.
run_case() {
  local rel_path="$1" content="$2" tmp
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/eshu-rce-test.XXXXXX")"
  (
    cd "$tmp"
    git init -q .
    git config user.email t@example.com
    git config user.name t
    mkdir -p "$(dirname "$rel_path")"
    printf 'base\n' >"$rel_path"
    git add -A && git commit -q -m base
    printf '%s\n' "$content" >>"$rel_path"
    git add -A && git commit -q -m change
  )
  ESHU_ROOT_CAUSE_EVIDENCE_REPO_ROOT="$tmp" \
    ESHU_ROOT_CAUSE_EVIDENCE_BASE="HEAD~1" \
    bash "$gate" 2>&1 || true
  rm -rf "$tmp"
}

expect_contains() {
  local name="$1" haystack="$2" needle="$3"
  cases_run=$((cases_run + 1))
  if printf '%s' "$haystack" | rg -q -F -- "$needle"; then
    report_pass "$name"
  else
    report_fail "$name" "expected output to contain: $needle"
  fi
}

expect_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  cases_run=$((cases_run + 1))
  if printf '%s' "$haystack" | rg -q -F -- "$needle"; then
    report_fail "$name" "expected output NOT to contain: $needle"
  else
    report_pass "$name"
  fi
}

printf 'test-verify-root-cause-evidence: running\n'

# A bare causal claim is the case this gate exists for.
out="$(run_case 'docs/internal/evidence/case.md' 'The root cause is a stale index.')"
expect_contains 'flags a causal claim with no marker' "$out" 'causal claim without recorded evidence'

# The same claim with a substantive marker passes.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The root cause is a stale index.
Root-Cause Evidence: reproduced 3 of 4 runs; EXPLAIN shows a seq scan on the
same query that used the index before the migration landed.')"
expect_contains 'accepts a causal claim with substantive evidence' "$out" 'carry observed evidence'

# A marker with nothing behind it is the obvious way to game a marker gate.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The root cause is a stale index.
Root-Cause Evidence: confirmed.')"
expect_contains 'rejects a marker with too little substance' "$out" 'too little after it'

# Hedged language must NOT trip the gate. Punishing honest labelling would
# invert the behaviour this is meant to encourage, so this is a core case.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The suspected cause is a stale index, but this is one theory and unproven.')"
expect_contains 'ignores hedged, explicitly-unproven wording' "$out" 'carry observed evidence'

# The parenthetical qualifier convention the performance markers already use.
out="$(run_case 'docs/internal/evidence/case.md' \
  'The root cause is a stale index.
Root-Cause Evidence (#5717): reproduced across four runs with the node present
in every one and the edge absent in three of them.')"
expect_contains 'accepts a parenthetical qualifier before the colon' "$out" 'carry observed evidence'

# Out of scope: same prose, wrong directory. Keeps the gate off ordinary
# explanatory writing in public docs.
out="$(run_case 'docs/public/reference/thing.md' 'The root cause is a stale index.')"
expect_not_contains 'ignores files outside the evidence directory' "$out" 'causal claim without recorded evidence'

# Fixtures must never satisfy or trip the gate.
out="$(run_case 'docs/internal/evidence/testdata/sample.md' 'The root cause is a stale index.')"
expect_not_contains 'ignores testdata fixtures' "$out" 'causal claim without recorded evidence'

printf '\ntest-verify-root-cause-evidence: %d case(s), %d failure(s)\n' "$cases_run" "$failures"
[ "$failures" -eq 0 ] || exit 1
