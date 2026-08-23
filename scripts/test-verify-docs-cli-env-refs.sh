#!/usr/bin/env bash
#
# Hermetic companion for verify-docs-cli-env-refs.sh. The suite builds the
# real Eshu CLI once, then points the production verifier at scratch docs and
# baselines so every case exercises envregistry and the live Cobra tree.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-docs-cli-env-refs.sh"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

assert_contains() {
  local needle="$1"
  local file="$2"
  local label="$3"
  if rg -q --fixed-strings -- "${needle}" "${file}"; then
    record_pass "${label}"
  else
    record_fail "${label} (missing ${needle})"
    sed -n '1,160p' "${file}" >&2
  fi
}

# assert_output_line matches a WHOLE diagnostic line, not a substring anywhere
# in the file. A per-segment attribution bug still prints the document name and
# the flag, so a substring assertion on either would pass while the command
# scope is wrong; only the full line pins which command owns the flag.
assert_output_line() {
  local regex="$1"
  local file="$2"
  local label="$3"
  if rg -q --regexp "${regex}" "${file}"; then
    record_pass "${label}"
  else
    record_fail "${label} (no line matching ${regex})"
    sed -n '1,160p' "${file}" >&2
  fi
}

assert_absent() {
  local needle="$1"
  local file="$2"
  local label="$3"
  if rg -q --fixed-strings -- "${needle}" "${file}"; then
    record_fail "${label} (unexpected ${needle})"
    sed -n '1,160p' "${file}" >&2
  else
    record_pass "${label}"
  fi
}

write_doc() {
  local root="$1"
  local rel="$2"
  shift 2
  mkdir -p "$(dirname "${root}/${rel}")"
  printf '%s\n' "$@" >"${root}/${rel}"
}

run_verifier() {
  local docs_root="$1"
  local baseline="$2"
  local out="$3"
  shift 3
  local ceiling="${ESHU_TEST_BASELINE_CEILING_PATH:-${repo_root}/scripts/docs-cli-env-refs-ceiling.txt}"
  ESHU_DOCS_CLI_ENV_PINNED_SKIPPED_LINES="${ESHU_TEST_PINNED_SKIPPED-0}" \
    ESHU_DOCS_CLI_ENV_MIN_ATTRIBUTED_SEGMENTS="${ESHU_TEST_MIN_ATTRIBUTED-0}" \
    ESHU_DOCS_CLI_ENV_DOCS_ROOT="${docs_root}" \
    ESHU_DOCS_CLI_ENV_BASELINE_PATH="${baseline}" \
    ESHU_DOCS_CLI_ENV_BASELINE_CEILING_PATH="${ceiling}" \
    ESHU_DOCS_CLI_ENV_ESHU_BINARY="${tmp_root}/eshu" \
    ESHU_DOCS_CLI_ENV_CHECKER_BINARY="${tmp_root}/docs-cli-env-refs" \
    ESHU_DOCS_CLI_ENV_GOCACHE="${tmp_root}/gocache-checker" \
    "${BASH:-bash}" "${verifier}" "$@" >"${out}" 2>&1
}

build_real_cli() {
  GOCACHE="${tmp_root}/gocache-eshu" go -C "${repo_root}/go" build \
    -o "${tmp_root}/eshu" ./cmd/eshu
  GOCACHE="${tmp_root}/gocache-checker-build" go -C "${repo_root}/go" build \
    -o "${tmp_root}/docs-cli-env-refs" ./cmd/docs-cli-env-refs
}

test_registered_references_pass() {
  local root="${tmp_root}/registered/docs/public"
  local baseline="${tmp_root}/registered/baseline.txt"
  local out="${tmp_root}/registered.out"
  write_doc "${root}" "guide.md" \
    'Use `ESHU_API_KEY`.' \
    '```bash' \
    'eshu docs verify docs/public --json' \
    '```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    assert_contains "docs-cli-env-refs: OK" "${out}" \
      "registered env and flag references pass"
  else
    record_fail "registered env and flag references pass"
    sed -n '1,160p' "${out}" >&2
  fi
}

test_new_unknowns_fail() {
  local root="${tmp_root}/unknown/docs/public"
  local baseline="${tmp_root}/unknown/baseline.txt"
  local out="${tmp_root}/unknown.out"
  write_doc "${root}" "guide.md" \
    'Use `ESHU_NOT_REGISTERED`.' \
    '```shell' \
    'eshu docs verify --not-a-real-flag --workspace-root /repo' \
    '```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "new unknown env and flag references fail"
  else
    record_pass "new unknown env and flag references fail"
  fi
  assert_contains "ESHU_NOT_REGISTERED" "${out}" "failure names unknown env"
  assert_contains "--not-a-real-flag" "${out}" "failure names unknown flag"
  assert_contains "--workspace-root" "${out}" \
    "flag registered on another command still fails"
  assert_contains "guide.md" "${out}" "failure names docs page"
}

test_hostile_command_and_markdown_forms_fail() {
  local root="${tmp_root}/hostile/docs/public"
  local baseline="${tmp_root}/hostile/baseline.txt"
  local out="${tmp_root}/hostile.out"
  write_doc "${root}" "unknown-command.md" \
    '```bash' \
    'eshu definitely-not-a-command --help' \
    'eshu docs definitely-not-a-command --help' \
    '```'
  write_doc "${root}" "quoted-flag.md" \
    '```bash' \
    'eshu docs verify "--quoted-invalid"' \
    '```'
  write_doc "${root}" "quoted-flag-value.md" \
    '```bash' \
    'eshu docs verify "--quoted-value-invalid=two words"' \
    '```'
  write_doc "${root}" "unmatched-quote.md" \
    '```bash' \
    'eshu docs verify --before-unmatched-invalid "unterminated' \
    '```'
  write_doc "${root}" "quoted-hash.md" \
    '```bash' \
    'eshu docs verify "#literal" --after-quoted-hash-invalid' \
    'eshu docs verify \#literal --after-escaped-hash-invalid' \
    '```'
  write_doc "${root}" "comment-operator.md" \
    '```bash' \
    'eshu docs verify --before-comment-operator-invalid # explanation | example' \
    '```'
  write_doc "${root}" "nested-fence.md" \
    '1. Nested example:' \
    '    ```bash' \
    '    eshu docs verify --nested-invalid' \
    '    ```'
  write_doc "${root}" "fence-close.md" \
    '```bash' \
    '```not-a-close' \
    'eshu docs verify --after-suffix-invalid' \
    '```'
  write_doc "${root}" "fence-close-nbsp.md" \
    '```bash' \
    $'```\u00a0' \
    'eshu docs verify --after-nbsp-invalid' \
    '```'
  write_doc "${root}" "fence-close-over-indented.md" \
    '```bash' \
    '    ```' \
    'eshu docs verify --after-over-indent-invalid' \
    '```'
  write_doc "${root}" "quoted-operators.md" \
    '```bash' \
    'eshu docs verify "a|b" --quoted-pipe-invalid' \
    "eshu docs verify 'a;b' --quoted-semicolon-invalid" \
    'eshu docs verify a\&b --escaped-ampersand-invalid' \
    '```'
  write_doc "${root}" "literal-block.md" \
    '    ```bash' \
    '    eshu docs verify --literal-block-ignored' \
    '    ```'
  write_doc "${root}" "literal-list-block.md" \
    '    1. list-looking literal' \
    '       ```bash' \
    '       eshu docs verify --literal-list-block-ignored' \
    '       ```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "unknown command, quoted flag, and nested fence fail closed"
  else
    record_pass "unknown command, quoted flag, and nested fence fail closed"
  fi
  assert_contains "unknown-command.md" "${out}" "unknown command is rejected"
  assert_contains "quoted-flag.md" "${out}" "quoted long flag is checked"
  assert_contains "quoted-flag-value.md" "${out}" "quoted long flag value with whitespace is checked"
  assert_contains "unmatched-quote.md" "${out}" "unmatched quote does not hide an earlier flag"
  assert_contains "quoted-hash.md" "${out}" "quoted or escaped hash does not hide a later flag"
  assert_contains "comment-operator.md" "${out}" "operator inside shell comment does not hide an earlier flag"
  assert_contains "nested-fence.md" "${out}" "nested shell fence is checked"
  assert_contains "fence-close.md" "${out}" "closing fence suffix does not hide flags"
  assert_contains "fence-close-nbsp.md" "${out}" "non-ASCII fence suffix does not hide flags"
  assert_contains "fence-close-over-indented.md" "${out}" "over-indented pseudo-close does not hide flags"
  assert_contains "quoted-operators.md" "${out}" "quoted and escaped operators remain in scanner scope"
  # Whole-line, not substring: splitting on any of these operators would drop the
  # flag on the wrong side of an unbalanced quote and print no line at all.
  for quoted_case in quoted-pipe quoted-semicolon escaped-ampersand; do
    assert_output_line \
      "^docs-cli-env-refs: quoted-operators\\.md cites unknown flag --${quoted_case}-invalid on command .eshu docs verify. \\(not in .*\\)$" \
      "${out}" "${quoted_case} stays inside one command segment"
  done
  if rg --fixed-strings --quiet -- "literal-block.md" "${out}"; then
    record_fail "top-level indented literal stays outside fenced-command scope"
  else
    record_pass "top-level indented literal stays outside fenced-command scope"
  fi
  if rg --fixed-strings --quiet -- "literal-list-block.md" "${out}"; then
    record_fail "list-looking indented literal stays outside fenced-command scope"
  else
    record_pass "list-looking indented literal stays outside fenced-command scope"
  fi
}

test_precision_exclusions_pass() {
  local root="${tmp_root}/excluded/docs/public"
  local baseline="${tmp_root}/excluded/baseline.txt"
  local out="${tmp_root}/excluded.out"
  write_doc "${root}" "guide.md" \
    'Family `ESHU_WORKFLOW_COORDINATOR_*` is a prefix, not a variable.' \
    'Prose mentions --not-a-real-flag.' \
    '```text' \
    'eshu docs verify --not-a-real-flag' \
    '```' \
    '```bash' \
    'eshu docs verify \"--not-a-real-flag\"' \
    '```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_pass "documented precision exclusions pass"
  else
    record_fail "documented precision exclusions pass"
    sed -n '1,160p' "${out}" >&2
  fi
}

test_baseline_and_update_are_burn_down_safe() {
  local root="${tmp_root}/baseline/docs/public"
  local baseline="${tmp_root}/baseline/baseline.txt"
  local out="${tmp_root}/baseline.out"
  write_doc "${root}" "contributing-language-support.md" \
    'Use `ESHU_PARSE_WORKERS`.'
  mkdir -p "$(dirname "${baseline}")"
  printf '%s\n' \
    '# baseline' \
    'env contributing-language-support.md ESHU_PARSE_WORKERS' >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_pass "baselined unknown references pass"
  else
    record_fail "baselined unknown references pass"
    sed -n '1,160p' "${out}" >&2
  fi

  run_verifier "${root}" "${baseline}" "${out}" -update
  cp "${baseline}" "${baseline}.first"
  run_verifier "${root}" "${baseline}" "${out}" -update
  if cmp -s "${baseline}.first" "${baseline}"; then
    record_pass "baseline update is byte-idempotent"
  else
    record_fail "baseline update is byte-idempotent"
  fi

  write_doc "${root}" "new-debt.md" 'Use `ESHU_NEW_BASELINE_DEBT`.'
  cp "${baseline}" "${baseline}.before-growth"
  if run_verifier "${root}" "${baseline}" "${out}" -update; then
    record_fail "baseline update rejects new debt"
  else
    record_pass "baseline update rejects new debt"
  fi
  if cmp -s "${baseline}.before-growth" "${baseline}"; then
    record_pass "rejected baseline growth leaves the baseline unchanged"
  else
    record_fail "rejected baseline growth leaves the baseline unchanged"
  fi
  assert_contains "ESHU_NEW_BASELINE_DEBT" "${out}" "baseline growth diagnostic names new debt"

  write_doc "${root}" "contributing-language-support.md" 'No unresolved references.'
  write_doc "${root}" "new-debt.md" 'No unresolved references.'
  cp "${repo_root}/scripts/docs-cli-env-refs-ceiling.txt" "${baseline}.ceiling-before"
  run_verifier "${root}" "${baseline}" "${out}" -update
  if rg --quiet '^(env|flag) ' "${baseline}"; then
    record_fail "mutable baseline burns down after references resolve"
  else
    record_pass "mutable baseline burns down after references resolve"
  fi
  if cmp -s "${baseline}.ceiling-before" "${repo_root}/scripts/docs-cli-env-refs-ceiling.txt"; then
    record_pass "baseline burn-down leaves frozen ceiling unchanged"
  else
    record_fail "baseline burn-down leaves frozen ceiling unchanged"
  fi
}

test_malformed_baseline_fails_closed() {
  local root="${tmp_root}/malformed/docs/public"
  local baseline="${tmp_root}/malformed/baseline.txt"
  local out="${tmp_root}/malformed.out"
  write_doc "${root}" "guide.md" 'No contract references.'
  mkdir -p "$(dirname "${baseline}")"
  printf 'env missing-fields\n' >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "malformed baseline fails closed"
  else
    record_pass "malformed baseline fails closed"
  fi
  assert_contains "malformed" "${out}" "malformed baseline diagnostic is explicit"
}

test_atomic_baseline_growth_fails() {
  local root="${tmp_root}/atomic-growth/docs/public"
  local baseline="${tmp_root}/atomic-growth/baseline.txt"
  local out="${tmp_root}/atomic-growth.out"
  write_doc "${root}" "guide.md" 'Use `ESHU_ATOMIC_NEW_DEBT`.'
  mkdir -p "$(dirname "${baseline}")"
  printf 'env guide.md ESHU_ATOMIC_NEW_DEBT\n' >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "atomic docs and baseline debt addition fails"
  else
    record_pass "atomic docs and baseline debt addition fails"
  fi
  assert_contains "ESHU_ATOMIC_NEW_DEBT" "${out}" "atomic baseline growth diagnostic names new debt"
}

test_frozen_ceiling_growth_fails() {
  local root="${tmp_root}/ceiling-growth/docs/public"
  local baseline="${tmp_root}/ceiling-growth/baseline.txt"
  local ceiling="${tmp_root}/ceiling-growth/ceiling.txt"
  local out="${tmp_root}/ceiling-growth.out"
  write_doc "${root}" "guide.md" 'Use `ESHU_CEILING_GROWTH_DEBT`.'
  mkdir -p "$(dirname "${baseline}")"
  printf 'env guide.md ESHU_CEILING_GROWTH_DEBT\n' >"${baseline}"
  cp "${repo_root}/scripts/docs-cli-env-refs-ceiling.txt" "${ceiling}"
  printf 'env guide.md ESHU_CEILING_GROWTH_DEBT\n' >>"${ceiling}"
  if ESHU_TEST_BASELINE_CEILING_PATH="${ceiling}" run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "frozen ceiling growth fails"
  else
    record_pass "frozen ceiling growth fails"
  fi
  assert_contains "code-owned reference count" "${out}" "ceiling growth diagnostic names frozen authority"
}

test_frozen_ceiling_shrink_plus_injection_fails() {
  local root="${tmp_root}/ceiling-replacement/docs/public"
  local baseline="${tmp_root}/ceiling-replacement/baseline.txt"
  local ceiling="${tmp_root}/ceiling-replacement/ceiling.txt"
  local out="${tmp_root}/ceiling-replacement.out"
  write_doc "${root}" "guide.md" 'Use `ESHU_CEILING_REPLACEMENT_DEBT`.'
  mkdir -p "$(dirname "${baseline}")"
  printf 'env guide.md ESHU_CEILING_REPLACEMENT_DEBT\n' >"${baseline}"
  awk '
    $0 == "env contributing-language-support.md ESHU_PARSE_WORKERS" { next }
    $0 == "env deploy/eks/index.md ESHU_MCP_URL" { next }
    { print }
  ' "${repo_root}/scripts/docs-cli-env-refs-ceiling.txt" >"${ceiling}"
  printf 'env guide.md ESHU_CEILING_REPLACEMENT_DEBT\n' >>"${ceiling}"
  if ESHU_TEST_BASELINE_CEILING_PATH="${ceiling}" run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "shrunk frozen ceiling with injected debt fails"
  else
    record_pass "shrunk frozen ceiling with injected debt fails"
  fi
  assert_contains "code-owned" "${out}" "shrink-plus-injection diagnostic names code-owned authority"
}

test_frozen_ceiling_same_count_replacement_fails() {
  local root="${tmp_root}/ceiling-same-count/docs/public"
  local baseline="${tmp_root}/ceiling-same-count/baseline.txt"
  local ceiling="${tmp_root}/ceiling-same-count/ceiling.txt"
  local out="${tmp_root}/ceiling-same-count.out"
  write_doc "${root}" "guide.md" 'Use `ESHU_CEILING_SAME_COUNT_DEBT`.'
  mkdir -p "$(dirname "${baseline}")"
  printf 'env guide.md ESHU_CEILING_SAME_COUNT_DEBT\n' >"${baseline}"
  awk '
    $0 == "env contributing-language-support.md ESHU_PARSE_WORKERS" { next }
    { print }
  ' "${repo_root}/scripts/docs-cli-env-refs-ceiling.txt" >"${ceiling}"
  printf 'env guide.md ESHU_CEILING_SAME_COUNT_DEBT\n' >>"${ceiling}"
  if ESHU_TEST_BASELINE_CEILING_PATH="${ceiling}" run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "same-count frozen ceiling replacement fails"
  else
    record_pass "same-count frozen ceiling replacement fails"
  fi
  assert_contains "code-owned digest" "${out}" "same-count replacement diagnostic names digest authority"
}

test_real_tree_matches_committed_baseline() {
  local baseline="${repo_root}/scripts/docs-cli-env-refs-baseline.txt"
  local out="${tmp_root}/real-tree.out"
  # Empty overrides mean the wrapper omits the flags, so this case runs against
  # the checker's code-owned pin and floor. It is the only case that does.
  export ESHU_TEST_PINNED_SKIPPED= ESHU_TEST_MIN_ATTRIBUTED=
  if run_verifier "${repo_root}/docs/public" "${baseline}" "${out}"; then
    record_pass "real public docs pass with committed baseline"
  else
    record_fail "real public docs pass with committed baseline"
    sed -n '1,160p' "${out}" >&2
  fi

  local regenerated="${tmp_root}/regenerated-baseline.txt"
  cp "${baseline}" "${regenerated}"
  run_verifier "${repo_root}/docs/public" "${regenerated}" "${out}" -update
  if cmp -s "${regenerated}" "${baseline}"; then
    record_pass "committed baseline matches fresh regeneration"
  else
    record_fail "committed baseline matches fresh regeneration"
    diff "${regenerated}" "${baseline}" >&2 || true
  fi
  unset ESHU_TEST_PINNED_SKIPPED ESHU_TEST_MIN_ATTRIBUTED
}

# shellcheck source=lib/test-verify-docs-cli-env-refs-segment-cases.sh
source "$(dirname "$0")/lib/test-verify-docs-cli-env-refs-segment-cases.sh"

build_real_cli
test_registered_references_pass
test_new_unknowns_fail
test_hostile_command_and_markdown_forms_fail
test_precision_exclusions_pass
test_pipeline_and_chain_segments_are_checked_per_command
test_unsupported_shell_forms_stay_skipped
test_scan_coverage_pins_are_enforced
test_baseline_and_update_are_burn_down_safe
test_malformed_baseline_fails_closed
test_atomic_baseline_growth_fails
test_frozen_ceiling_growth_fails
test_frozen_ceiling_same_count_replacement_fails
test_frozen_ceiling_shrink_plus_injection_fails
test_real_tree_matches_committed_baseline

if [[ "${FAIL}" -ne 0 ]]; then
  printf 'test-verify-docs-cli-env-refs FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi
printf 'test-verify-docs-cli-env-refs passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
