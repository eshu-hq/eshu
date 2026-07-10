#!/usr/bin/env bash
#
# test-verify-docs-prose-quality.sh - hermetic tests for the advisory docs
# prose-quality gate.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-docs-prose-quality.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

assert_contains() {
  local needle="$1"
  local file="$2"
  if ! rg -q --fixed-strings "${needle}" "${file}"; then
    printf 'test-docs-prose: expected %s to contain: %s\n' "${file}" "${needle}" >&2
    cat "${file}" >&2
    exit 1
  fi
}

write_doc() {
  local root="$1"
  local rel="$2"
  local type="$3"
  local body="$4"
  local file="${root}/docs/public/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf '%s\n' \
    '<!-- docs-catalog' \
    "title: ${rel}" \
    "description: Test metadata for ${rel}." \
    "type: ${type}" \
    "audience: test" \
    "entrypoint: false" \
    '-->' \
    '' \
    "# ${rel}" \
    '' \
    "${body}" >"${file}"
}

run_gate() {
  local root="$1"
  local enforce="$2"
  local out="$3"
  DOCS_PROSE_ENFORCE="${enforce}" ESHU_DOCS_PROSE_REPO_ROOT="${root}" \
    bash "${verifier}" >"${out}" 2>&1
}

test_clean_human_docs_pass() {
  local root="${tmp_root}/clean"
  local out="${tmp_root}/clean.out"
  write_doc "${root}" "tutorials/trace.md" "tutorial" $'Use this tutorial when you need a short, repeatable path.\n\n```bash\neshu first-run\n```\n\nThe command prints the first successful run summary.'
  run_gate "${root}" "true" "${out}"
  assert_contains "no prose-quality findings" "${out}"
}

test_advisory_mode_reports_without_failing() {
  local root="${tmp_root}/advisory"
  local out="${tmp_root}/advisory.out"
  write_doc "${root}" "use/index.md" "how-to" "This powerful and seamless page helps teams leverage a world-class workflow."
  run_gate "${root}" "false" "${out}"
  assert_contains "banned-filler" "${out}"
  assert_contains "ADVISORY" "${out}"
}

test_enforce_mode_fails_on_human_doc_findings() {
  local root="${tmp_root}/enforce"
  local out="${tmp_root}/enforce.out"
  write_doc "${root}" "operate/troubleshoot.md" "operate" $'Run the command.\n\n$ eshu status'
  if run_gate "${root}" "true" "${out}"; then
    printf 'test-docs-prose: expected enforce mode to fail\n' >&2
    exit 1
  fi
  assert_contains "prompt-prefix" "${out}"
}

test_missing_h1_reports_one_purpose() {
  local root="${tmp_root}/missing-h1"
  local out="${tmp_root}/missing-h1.out"
  local file="${root}/docs/public/concepts/no-heading.md"
  mkdir -p "$(dirname "${file}")"
  printf '%s\n' \
    '<!-- docs-catalog' \
    'title: Missing H1' \
    'description: Test metadata for a missing heading.' \
    'type: concept' \
    'audience: test' \
    'entrypoint: false' \
    '-->' \
    '' \
    'This page is missing its only H1.' >"${file}"
  if run_gate "${root}" "true" "${out}"; then
    printf 'test-docs-prose: expected missing-H1 case to fail in enforce mode\n' >&2
    exit 1
  fi
  assert_contains "one-purpose" "${out}"
}

test_reference_proof_and_generated_docs_are_exempt() {
  local root="${tmp_root}/exempt"
  local out="${tmp_root}/exempt.out"
  write_doc "${root}" "reference/index.md" "reference" "This powerful reference table is intentionally exempt."
  write_doc "${root}" "reference/proof.md" "proof" "This seamless proof artifact is intentionally exempt."
  write_doc "${root}" "tutorials/generated.md" "tutorial" $'<!-- Generated from a fixture. Do not edit by hand. -->\n\nThis powerful generated tutorial is exempt.'
  run_gate "${root}" "true" "${out}"
  assert_contains "no prose-quality findings" "${out}"
}

test_tutorial_readability_and_code_fence_rules_report() {
  local root="${tmp_root}/readability"
  local out="${tmp_root}/readability.out"
  write_doc "${root}" "tutorials/long.md" "tutorial" $'This tutorial sentence intentionally keeps running with many extra words so the checker can prove the tutorial reading-level guard catches lines that are too dense for a first pass reader who needs direct short instructions instead of a packed paragraph.\n\n```\neshu first-run\n```'
  if run_gate "${root}" "true" "${out}"; then
    printf 'test-docs-prose: expected readability case to fail in enforce mode\n' >&2
    exit 1
  fi
  assert_contains "long-line" "${out}"
  assert_contains "code-fence-language" "${out}"
}

test_clean_human_docs_pass
test_advisory_mode_reports_without_failing
test_enforce_mode_fails_on_human_doc_findings
test_missing_h1_reports_one_purpose
test_reference_proof_and_generated_docs_are_exempt
test_tutorial_readability_and_code_fence_rules_report

printf 'test-docs-prose: all tests passed\n'
