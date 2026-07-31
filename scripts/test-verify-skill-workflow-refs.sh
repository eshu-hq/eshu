#!/usr/bin/env bash
#
# test-verify-skill-workflow-refs.sh - hermetic tests for
# scripts/verify-skill-workflow-refs.sh, the gate that fails a skill doc
# under .agents/skills/**/*.md naming a .github/workflows/*.yml file that
# does not exist (#5855).
#
# Every case builds a scratch skills dir + workflows dir under mktemp and
# points the verifier at them via ESHU_SKILL_WORKFLOW_REFS_SKILLS_DIR /
# ESHU_SKILL_WORKFLOW_REFS_WORKFLOWS_DIR, so the committed
# .agents/skills/**/.github/workflows/** trees are never touched by a test
# run. The real-tree case (7) proves the CURRENT committed tree is already
# clean, so this gate cannot regress silently the day it lands. Cases 8-11
# cover the bare-PROSE shape (no backticks, no path prefix) added to close
# the #5855 P2 follow-up: a SKILL.md naming a workflow in plain prose (e.g.
# "editing security-scan.yml") was invisible to both the full-path and the
# backtick-anchored bare-name scan.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-skill-workflow-refs.sh"

command -v rg >/dev/null 2>&1 || {
  echo "test-verify-skill-workflow-refs: rg is required" >&2
  exit 1
}

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
  if rg -q --fixed-strings "${needle}" "${file}"; then
    record_pass "${label}"
  else
    record_fail "${label} (expected to find: ${needle})"
    cat "${file}" >&2
  fi
}

# write_skill creates a scratch skill doc with the given body lines.
write_skill() {
  local root="$1"
  local rel="$2"
  shift 2
  local file="${root}/skills/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf '%s\n' "$@" >"${file}"
}

# write_workflow creates a scratch workflow file so existence checks can
# resolve it as a real, present file.
write_workflow() {
  local root="$1"
  local name="$2"
  local file="${root}/workflows/${name}"
  mkdir -p "$(dirname "${file}")"
  printf 'name: %s\non:\n  push:\n' "${name}" >"${file}"
}

run_verifier() {
  local root="$1"
  local out="$2"
  ESHU_SKILL_WORKFLOW_REFS_SKILLS_DIR="${root}/skills" \
  ESHU_SKILL_WORKFLOW_REFS_WORKFLOWS_DIR="${root}/workflows" \
    "${BASH:-bash}" "${verifier}" >"${out}" 2>&1
}

# Case 1: a full-path citation of an existing workflow passes.
test_full_path_existing_passes() {
  local root="${tmp_root}/case1"
  local out="${tmp_root}/case1.out"
  write_workflow "${root}" "verify-real-gate.yml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'The CI workflow `.github/workflows/verify-real-gate.yml` runs this gate.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case1: full-path citation of existing workflow passes"
  else
    record_fail "case1: full-path citation of existing workflow passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 2: a full-path citation of a MISSING workflow fails, naming the
# dangling path. Mirrors the real #5855 telemetry-coverage-discipline defect.
test_full_path_missing_fails() {
  local root="${tmp_root}/case2"
  local out="${tmp_root}/case2.out"
  write_workflow "${root}" "verify-other-gate.yml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'The CI workflow `.github/workflows/verify-ghost-gate.yml` runs this gate.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case2: full-path citation of missing workflow fails (verifier exited zero)"
    cat "${out}" >&2
  else
    assert_contains ".github/workflows/verify-ghost-gate.yml" "${out}" \
      "case2: full-path citation of missing workflow names the dangling path"
  fi
}

# Case 3: a bare backtick-wrapped lowercase filename citing an EXISTING
# workflow passes. Mirrors the generator-script-discipline "Mirror the
# existing `verify-skill-roundtrip.yml`" prose shape.
test_bare_name_existing_passes() {
  local root="${tmp_root}/case3"
  local out="${tmp_root}/case3.out"
  write_workflow "${root}" "verify-real-gate.yml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'Mirror the existing `verify-real-gate.yml` workflow.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case3: bare-name citation of existing workflow passes"
  else
    record_fail "case3: bare-name citation of existing workflow passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 4: a bare backtick-wrapped lowercase filename citing a MISSING
# workflow fails. This is the shape a full-path-only scan misses (the #5855
# report's generator-script-discipline SKILL.md:186 reference).
test_bare_name_missing_fails() {
  local root="${tmp_root}/case4"
  local out="${tmp_root}/case4.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'Mirror the existing `verify-skill-roundtrip.yml` and' \
    '`verify-telemetry-coverage.yml` workflows:'
  if run_verifier "${root}" "${out}"; then
    record_fail "case4: bare-name citation of missing workflow fails (verifier exited zero)"
    cat "${out}" >&2
  else
    assert_contains ".github/workflows/verify-skill-roundtrip.yml" "${out}" \
      "case4: bare-name citation of missing workflow names the first dangling path"
    assert_contains ".github/workflows/verify-telemetry-coverage.yml" "${out}" \
      "case4: bare-name citation of missing workflow names the second dangling path"
  fi
}

# Case 5: a generic, non-workflow bare `.yml` mention (uppercase-leading or
# dot-leading, e.g. Taskfile.yml / .golangci.yml) is left alone even with no
# matching file under workflows/. This is the golang-engineering
# verification-and-linting.md "Discover the Repo's Verification Entrypoint"
# checklist shape, where "CI workflows" sits one bullet away from
# `Taskfile.yml` with no blank line to bound a context-window heuristic —
# the reason this gate uses a leading-lowercase character class instead of
# proximity to the word "workflow".
test_generic_yml_mention_not_flagged() {
  local root="${tmp_root}/case5"
  local out="${tmp_root}/case5.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    '- `Makefile`, `Taskfile.yml`, `justfile`, `magefile.go`, or repo scripts' \
    '- CI workflows such as `.github/workflows/*`, `.circleci/*`, or other pipeline config' \
    '- `.golangci.yml`, `.golangci.yaml`, or linter commands referenced in CI'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case5: generic Taskfile.yml/.golangci.yml mentions are not flagged"
  else
    record_fail "case5: generic Taskfile.yml/.golangci.yml mentions are not flagged (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 6: a skills dir with no .yml mentions at all passes trivially (zero
# citations checked is not an error).
test_no_citations_passes() {
  local root="${tmp_root}/case6"
  local out="${tmp_root}/case6.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'No workflow citations here.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case6: no citations passes trivially"
  else
    record_fail "case6: no citations passes trivially (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 7: the real, committed .agents/skills tree against the real,
# committed .github/workflows tree passes today. This is the regression
# proof: it fails on the pre-fix tree (three telemetry-coverage-discipline
# references plus two generator-script-discipline references to workflow
# files consolidated away by #4218) and passes once #5855 corrects them.
test_real_tree_passes() {
  local out="${tmp_root}/case7.out"
  if ESHU_SKILL_WORKFLOW_REFS_SKILLS_DIR="${repo_root}/.agents/skills" \
     ESHU_SKILL_WORKFLOW_REFS_WORKFLOWS_DIR="${repo_root}/.github/workflows" \
     "${BASH:-bash}" "${verifier}" >"${out}" 2>&1; then
    assert_contains "OK:" "${out}" "case7: real committed skill docs pass against real committed workflows"
  else
    record_fail "case7: real committed skill docs pass against real committed workflows"
    cat "${out}" >&2
  fi
}

# Case 8: a bare, un-backticked, un-pathed prose mention of an EXISTING
# workflow passes. Mirrors the real
# eshu-security-scan-gates/SKILL.md:6 "ACTIVATE when editing
# security-scan.yml" shape.
test_bare_prose_existing_passes() {
  local root="${tmp_root}/case8"
  local out="${tmp_root}/case8.out"
  write_workflow "${root}" "verify-real-gate.yml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'ACTIVATE when editing verify-real-gate.yml, bumping the toolchain.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case8: bare-prose citation of existing workflow passes"
  else
    record_fail "case8: bare-prose citation of existing workflow passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 9: a bare, un-backticked, un-pathed prose mention of a MISSING
# workflow fails, naming the dangling path. This is the exact #5855 P2
# blind spot: eshu-security-scan-gates/SKILL.md:135's
# 'the test.yml "Verify hot-path evidence" step' shape, reproduced here by
# renaming the referenced workflow out of existence.
test_bare_prose_missing_fails() {
  local root="${tmp_root}/case9"
  local out="${tmp_root}/case9.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'the ghost-prose-gate.yml "Verify hot-path evidence" step requires a note.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case9: bare-prose citation of missing workflow fails (verifier exited zero)"
    cat "${out}" >&2
  else
    assert_contains ".github/workflows/ghost-prose-gate.yml" "${out}" \
      "case9: bare-prose citation of missing workflow names the dangling path"
  fi
}

# Case 10: a bare prose mention of an ALLOWLISTED non-workflow name (mkdocs
# is documentation config, not a GH Actions workflow) is not flagged even
# though no matching file exists under workflows/. Proves
# bare_yaml_name_is_allowlisted suppresses the known non-workflow basenames
# without needing a backtick or path prefix to hide them.
test_bare_prose_allowlisted_not_flagged() {
  local root="${tmp_root}/case10"
  local out="${tmp_root}/case10.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'Regenerate mkdocs.yml after editing the nav, then rerun docker-compose.yml.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case10: allowlisted bare mkdocs.yml/docker-compose.yml mentions are not flagged"
  else
    record_fail "case10: allowlisted bare mkdocs.yml/docker-compose.yml mentions are not flagged (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 11: a full-path citation of an EXISTING hyphenated workflow
# (docker-publish.yml) does not also trip the bare-prose scan on the
# SUFFIX after the hyphen ("publish.yml"). This is a regression guard for a
# real bug hit while building the bare-prose scan: a lookbehind that only
# excluded `/` and backtick let the regex re-enter after the `-` in
# `docker-publish.yml` (a non-word character, so `\b` matches right before
# "publish") and misreport "publish.yml" as its own dangling bare citation.
# Mirrors the real eshu-release/SKILL.md:177 full-URL citation shape.
test_bare_prose_hyphen_suffix_not_misdetected() {
  local root="${tmp_root}/case11"
  local out="${tmp_root}/case11.out"
  write_workflow "${root}" "docker-publish.yml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'WORKFLOW_IDENTITY="https://github.com/eshu-hq/eshu/.github/workflows/docker-publish.yml@refs/tags/v1"'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case11: hyphenated full-path citation does not misdetect a bare suffix"
  else
    record_fail "case11: hyphenated full-path citation does not misdetect a bare suffix (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 12: a full-path citation of an existing `.yaml` (not `.yml`) workflow
# passes. GitHub Actions accepts both extensions
# (https://docs.github.com/actions/using-workflows/about-workflows); the
# original three shapes hardcoded `\.yml`, so a `.yaml` workflow was never
# checked at all.
test_full_path_existing_yaml_passes() {
  local root="${tmp_root}/case12"
  local out="${tmp_root}/case12.out"
  write_workflow "${root}" "verify-real-gate.yaml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'The CI workflow `.github/workflows/verify-real-gate.yaml` runs this gate.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case12: full-path citation of existing .yaml workflow passes"
  else
    record_fail "case12: full-path citation of existing .yaml workflow passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 13: a full-path citation of a MISSING `.yaml` workflow fails, naming
# the dangling path.
test_full_path_missing_yaml_fails() {
  local root="${tmp_root}/case13"
  local out="${tmp_root}/case13.out"
  write_workflow "${root}" "verify-other-gate.yaml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'The CI workflow `.github/workflows/verify-ghost-gate.yaml` runs this gate.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case13: full-path citation of missing .yaml workflow fails (verifier exited zero)"
    cat "${out}" >&2
  else
    assert_contains ".github/workflows/verify-ghost-gate.yaml" "${out}" \
      "case13: full-path citation of missing .yaml workflow names the dangling path"
  fi
}

# Case 14: a bare backtick-wrapped lowercase `.yaml` filename citing an
# EXISTING workflow passes.
test_bare_name_existing_yaml_passes() {
  local root="${tmp_root}/case14"
  local out="${tmp_root}/case14.out"
  write_workflow "${root}" "verify-real-gate.yaml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'Mirror the existing `verify-real-gate.yaml` workflow.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case14: bare-name citation of existing .yaml workflow passes"
  else
    record_fail "case14: bare-name citation of existing .yaml workflow passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 15: a bare backtick-wrapped lowercase `.yaml` filename citing a
# MISSING workflow fails.
test_bare_name_missing_yaml_fails() {
  local root="${tmp_root}/case15"
  local out="${tmp_root}/case15.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'Mirror the existing `verify-ghost-gate.yaml` workflow.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case15: bare-name citation of missing .yaml workflow fails (verifier exited zero)"
    cat "${out}" >&2
  else
    assert_contains ".github/workflows/verify-ghost-gate.yaml" "${out}" \
      "case15: bare-name citation of missing .yaml workflow names the dangling path"
  fi
}

# Case 16: a bare, un-backticked, un-pathed prose mention of an EXISTING
# `.yaml` workflow passes.
test_bare_prose_existing_yaml_passes() {
  local root="${tmp_root}/case16"
  local out="${tmp_root}/case16.out"
  write_workflow "${root}" "verify-real-gate.yaml"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'ACTIVATE when editing verify-real-gate.yaml, bumping the toolchain.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case16: bare-prose citation of existing .yaml workflow passes"
  else
    record_fail "case16: bare-prose citation of existing .yaml workflow passes (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# Case 17: a bare, un-backticked, un-pathed prose mention of a MISSING
# `.yaml` workflow fails, naming the dangling path.
test_bare_prose_missing_yaml_fails() {
  local root="${tmp_root}/case17"
  local out="${tmp_root}/case17.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'the ghost-prose-gate.yaml "Verify hot-path evidence" step requires a note.'
  if run_verifier "${root}" "${out}"; then
    record_fail "case17: bare-prose citation of missing .yaml workflow fails (verifier exited zero)"
    cat "${out}" >&2
  else
    assert_contains ".github/workflows/ghost-prose-gate.yaml" "${out}" \
      "case17: bare-prose citation of missing .yaml workflow names the dangling path"
  fi
}

# Case 18: a backtick-wrapped bare `.yaml` mention of an ALLOWLISTED
# non-workflow name (values.yaml is a Helm chart file, not a GH Actions
# workflow -- see the real eshu-release/SKILL.md:104 reference) is not
# flagged even though no matching file exists under workflows/. Regression
# guard for the false positive this exact #5855 `.yaml` fix uncovered
# against the real committed tree on the first run: shape 2 (backticked bare
# name) previously had no allowlist at all, because no real non-workflow
# `.yml` file matched it; `.yaml` support immediately exposed one.
test_bare_name_allowlisted_yaml_not_flagged() {
  local root="${tmp_root}/case18"
  local out="${tmp_root}/case18.out"
  write_skill "${root}" "example/SKILL.md" \
    '# example' \
    '' \
    'Use this when Helm templates, `values.yaml`, or `values.schema.json` change.'
  if run_verifier "${root}" "${out}"; then
    assert_contains "OK:" "${out}" "case18: allowlisted backtick-wrapped values.yaml mention is not flagged"
  else
    record_fail "case18: allowlisted backtick-wrapped values.yaml mention is not flagged (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

test_full_path_existing_passes
test_full_path_missing_fails
test_bare_name_existing_passes
test_bare_name_missing_fails
test_generic_yml_mention_not_flagged
test_no_citations_passes
test_real_tree_passes
test_bare_prose_existing_passes
test_bare_prose_missing_fails
test_bare_prose_allowlisted_not_flagged
test_bare_prose_hyphen_suffix_not_misdetected
test_full_path_existing_yaml_passes
test_full_path_missing_yaml_fails
test_bare_name_existing_yaml_passes
test_bare_name_missing_yaml_fails
test_bare_prose_existing_yaml_passes
test_bare_prose_missing_yaml_fails
test_bare_name_allowlisted_yaml_not_flagged

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
if [ "${FAIL}" -gt 0 ]; then
  exit 1
fi
printf 'test-verify-skill-workflow-refs tests passed\n'
