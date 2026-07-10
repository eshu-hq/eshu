#!/usr/bin/env bash
#
# test-verify-docs-catalog.sh - hermetic tests for the docs catalog verifier.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-docs-catalog.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

required_pages=(
  "index.md"
  "start-here.md"
  "getting-started/first-successful-run.md"
  "mcp/index.md"
  "tutorials/index.md"
  "tutorials/trace-vulnerable-dependency.md"
  "tutorials/ask-from-assistant.md"
  "tutorials/index-repositories.md"
  "tutorials/deploy-kubernetes.md"
  "tutorials/debug-stale-answers.md"
  "use/index.md"
  "use/code-questions.md"
  "use/index-repositories.md"
  "use/trace-infrastructure.md"
  "concepts/how-it-works.md"
  "understand/index.md"
  "reference/index.md"
  "reference/contracts.md"
  "reference/proof-and-validation.md"
  "operate/index.md"
  "operate/health-checks.md"
  "operate/freshness-convergence.md"
  "operate/troubleshooting.md"
)

assert_contains() {
  local needle="$1"
  local file="$2"
  if ! rg -q --fixed-strings "${needle}" "${file}"; then
    echo "test-docs-catalog: expected ${file} to contain: ${needle}" >&2
    cat "${file}" >&2
    exit 1
  fi
}

write_doc() {
  local root="$1"
  local rel="$2"
  local type="$3"
  local entrypoint="$4"
  local landing="$5"
  local body="${6:-}"
  local file="${root}/docs/public/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf '%s\n' \
    '<!-- docs-catalog' \
    "title: ${rel}" \
    "description: Test metadata for ${rel}." \
    "type: ${type}" \
    "audience: test" \
    "entrypoint: ${entrypoint}" \
    "landing: ${landing}" \
    '-->' \
    '' \
    "# ${rel}" \
    '' \
    "${body}" >"${file}"
}

seed_repo() {
  local root="$1"
  mkdir -p "${root}/docs/public"
  local page
  for page in "${required_pages[@]}"; do
    write_doc "${root}" "${page}" "reference" "true" "false"
  done

  local links=""
  for page in "${required_pages[@]}"; do
    [[ "${page}" == "index.md" ]] && continue
    links+=$'\n'"- [${page}](${page})"
  done
  write_doc "${root}" "index.md" "project" "false" "true" "${links}"
  write_doc "${root}" "tutorials/index.md" "tutorial" "false" "true"
  write_doc "${root}" "use/index.md" "how-to" "false" "true"
  write_doc "${root}" "understand/index.md" "concept" "false" "true"
  write_doc "${root}" "reference/index.md" "reference" "false" "true"
  write_doc "${root}" "operate/index.md" "operate" "false" "true"
}

run_verifier() {
  local root="$1"
  local out="$2"
  ESHU_DOCS_CATALOG_REPO_ROOT="${root}" bash "${verifier}" >"${out}" 2>&1
}

test_valid_catalog_passes() {
  local root="${tmp_root}/valid"
  local out="${tmp_root}/valid.out"
  seed_repo "${root}"
  run_verifier "${root}" "${out}"
  assert_contains "metadata check passed" "${out}"
}

test_missing_required_file_fails() {
  local root="${tmp_root}/missing"
  local out="${tmp_root}/missing.out"
  seed_repo "${root}"
  rm "${root}/docs/public/start-here.md"
  if run_verifier "${root}" "${out}"; then
    echo "test-docs-catalog: expected missing file case to fail" >&2
    exit 1
  fi
  assert_contains "missing required docs page start-here.md" "${out}"
}

test_bad_type_fails() {
  local root="${tmp_root}/bad-type"
  local out="${tmp_root}/bad-type.out"
  seed_repo "${root}"
  perl -0pi -e 's/type: project/type: landing-page/' "${root}/docs/public/index.md"
  if run_verifier "${root}" "${out}"; then
    echo "test-docs-catalog: expected bad type case to fail" >&2
    exit 1
  fi
  assert_contains "invalid docs-catalog type landing-page" "${out}"
}

test_unreachable_entrypoint_fails() {
  local root="${tmp_root}/unreachable"
  local out="${tmp_root}/unreachable.out"
  seed_repo "${root}"
  perl -0pi -e 's/\n- \[start-here\.md\]\(start-here\.md\)//' "${root}/docs/public/index.md"
  if run_verifier "${root}" "${out}"; then
    echo "test-docs-catalog: expected unreachable entrypoint case to fail" >&2
    exit 1
  fi
  assert_contains "start-here.md: entrypoint is not reachable" "${out}"
}

test_unterminated_metadata_block_fails() {
  local root="${tmp_root}/unterminated"
  local out="${tmp_root}/unterminated.out"
  seed_repo "${root}"
  perl -0pi -e 's/\n-->\n\n# start-here\.md/\n\n# start-here.md/' "${root}/docs/public/start-here.md"
  if run_verifier "${root}" "${out}"; then
    echo "test-docs-catalog: expected unterminated metadata block case to fail" >&2
    exit 1
  fi
  assert_contains "start-here.md: docs-catalog block is missing closing -->" "${out}"
}

test_valid_catalog_passes
test_missing_required_file_fails
test_bad_type_fails
test_unreachable_entrypoint_fails
test_unterminated_metadata_block_fails

echo "test-docs-catalog: all tests passed"
