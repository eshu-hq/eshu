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
  local ceiling="${ESHU_TEST_BASELINE_CEILING_PATH:-${baseline}}"
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
  assert_contains "nested-fence.md" "${out}" "nested shell fence is checked"
  assert_contains "fence-close.md" "${out}" "closing fence suffix does not hide flags"
  assert_contains "fence-close-nbsp.md" "${out}" "non-ASCII fence suffix does not hide flags"
  assert_contains "fence-close-over-indented.md" "${out}" "over-indented pseudo-close does not hide flags"
  assert_contains "quoted-operators.md" "${out}" "quoted and escaped operators remain in scanner scope"
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
    'cat input.json | eshu service-report --not-a-real-flag' \
    'eshu docs verify --json | eshu definitely-not-a-command --json' \
    'eshu docs verify --json | eshu definitely-not-a-command --not-a-real-flag' \
    'eshu docs verify --json && eshu definitely-not-a-command --not-a-real-flag' \
    'eshu docs verify --json ; eshu definitely-not-a-command --not-a-real-flag' \
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
  write_doc "${root}" "guide.md" \
    'Use `ESHU_NOT_REGISTERED`.' \
    '```console' \
    '$ eshu docs verify --not-a-real-flag' \
    '```'
  mkdir -p "$(dirname "${baseline}")"
  printf '%s\n' \
    '# baseline' \
    'env guide.md ESHU_NOT_REGISTERED' \
    'flag guide.md docs/verify::--not-a-real-flag' >"${baseline}"
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
  local ceiling="${tmp_root}/atomic-growth/ceiling.txt"
  local out="${tmp_root}/atomic-growth.out"
  write_doc "${root}" "guide.md" 'Use `ESHU_ATOMIC_NEW_DEBT`.'
  mkdir -p "$(dirname "${baseline}")"
  printf 'env guide.md ESHU_ATOMIC_NEW_DEBT\n' >"${baseline}"
  : >"${ceiling}"
  if ESHU_TEST_BASELINE_CEILING_PATH="${ceiling}" run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "atomic docs and baseline debt addition fails"
  else
    record_pass "atomic docs and baseline debt addition fails"
  fi
  assert_contains "ESHU_ATOMIC_NEW_DEBT" "${out}" "atomic baseline growth diagnostic names new debt"
}

test_real_tree_matches_committed_baseline() {
  local baseline="${repo_root}/scripts/docs-cli-env-refs-baseline.txt"
  local out="${tmp_root}/real-tree.out"
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
}

build_real_cli
test_registered_references_pass
test_new_unknowns_fail
test_hostile_command_and_markdown_forms_fail
test_precision_exclusions_pass
test_baseline_and_update_are_burn_down_safe
test_malformed_baseline_fails_closed
test_atomic_baseline_growth_fails
test_real_tree_matches_committed_baseline

if [[ "${FAIL}" -ne 0 ]]; then
  printf 'test-verify-docs-cli-env-refs FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi
printf 'test-verify-docs-cli-env-refs passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
