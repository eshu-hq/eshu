#!/usr/bin/env bash
# Regression and seeded-violation tests for scripts/verify-evidence-hygiene.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-evidence-hygiene.sh"

tmp_root="$(mktemp -d)"
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/docs/internal/evidence"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  cat >"${dir}/docs/internal/evidence/README.md" <<'MD'
# Internal Evidence Records

Never link to `.claude/` or `/tmp/` here.
MD
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m "initial"
  printf '%s\n' "${dir}"
}

run_case() {
  local label="$1" expected="$2" dir="$3"
  shift 3
  local rc=0
  ESHU_EVIDENCE_HYGIENE_REPO_ROOT="${dir}" \
    ESHU_EVIDENCE_HYGIENE_BASE=HEAD~1 \
    bash "${verifier}" "$@" >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
  if [ "$rc" -ne "$expected" ]; then
    printf 'FAIL [%s]: expected exit %s, got %s\n' "$label" "$expected" "$rc" >&2
    cat "${tmp_root}/out" "${tmp_root}/err" >&2
    exit 1
  fi
  printf 'PASS [%s]\n' "$label"
}

# 1. Clean compliant note passes (GREEN).
clean_dir="$(init_repo clean)"
cat >"${clean_dir}/docs/internal/evidence/1234-clean-proof.md" <<'MD'
# Clean live check

Refs #1234. Date: 2026-11-26.

## Summary

Checked the live query latency after adding the index. p95 dropped from 4.2s to 120ms.
MD
git -C "${clean_dir}" add . && git -C "${clean_dir}" commit -q -m "add clean note"
run_case "clean note passes" 0 "${clean_dir}"
run_case "clean note passes in --files mode" 0 "${clean_dir}" --files docs/internal/evidence/1234-clean-proof.md

# 2. Non-Markdown raw dump fails (RED).
raw_file_dir="$(init_repo raw-file)"
printf 'QUERY PLAN\nSeq Scan\n' >"${raw_file_dir}/docs/internal/evidence/explain.txt"
git -C "${raw_file_dir}" add . && git -C "${raw_file_dir}" commit -q -m "add raw dump"
run_case "raw .txt file rejected" 1 "${raw_file_dir}"
run_case "raw .txt file rejected in --files mode" 1 "${raw_file_dir}" --files docs/internal/evidence/explain.txt

# 3. *-raw/ directory even with .md tombstone fails (RED).
raw_dir="$(init_repo raw-dir)"
mkdir -p "${raw_dir}/docs/internal/evidence/10972-h2lane2-raw"
printf '# Removed\n\n## Summary\n\nRemoved.\n' >"${raw_dir}/docs/internal/evidence/10972-h2lane2-raw/README.md"
git -C "${raw_dir}" add . && git -C "${raw_dir}" commit -q -m "add -raw dir"
run_case "*-raw directory rejected" 1 "${raw_dir}"

# 4. Link to untracked .claude/ file fails (RED).
local_link_dir="$(init_repo local-link)"
cat >"${local_link_dir}/docs/internal/evidence/1234-local-link.md" <<'MD'
# Ruling check

## Summary

See [ruling](../../../.claude/arbiter-1234.md).
MD
git -C "${local_link_dir}" add . && git -C "${local_link_dir}" commit -q -m "add local link"
run_case "untracked .claude link rejected" 1 "${local_link_dir}"
run_case "untracked .claude link rejected in --files mode" 1 "${local_link_dir}" --files docs/internal/evidence/1234-local-link.md

# 5. Missing ## Summary heading fails (RED).
no_summary_dir="$(init_repo no-summary)"
cat >"${no_summary_dir}/docs/internal/evidence/1234-no-summary.md" <<'MD'
# Raw notes

## Substrate

Just a wall of notes without a summary.
MD
git -C "${no_summary_dir}" add . && git -C "${no_summary_dir}" commit -q -m "missing summary"
run_case "missing ## Summary rejected" 1 "${no_summary_dir}"

# 6. New note exceeding 150 lines fails (RED).
long_dir="$(init_repo long-note)"
{
  printf '# Too long\n\n## Summary\n\nStill too long.\n\n'
  i=1
  while [ "$i" -le 155 ]; do
    printf 'line %d\n' "$i"
    i=$((i + 1))
  done
} >"${long_dir}/docs/internal/evidence/1234-long.md"
git -C "${long_dir}" add . && git -C "${long_dir}" commit -q -m "add long note"
run_case "new note over 150 lines rejected" 1 "${long_dir}"

# 7. Editing an existing pre-base long file with pre-existing .claude/ ref passes (GREEN),
#    while adding a new .claude/ ref to it fails (RED) in both modes.
legacy_dir="$(init_repo legacy)"
{
  printf '# Legacy note\n\nLegacy ref: .claude/old-ruling.md.\n'
  i=1
  while [ "$i" -le 180 ]; do
    printf 'legacy line %d\n' "$i"
    i=$((i + 1))
  done
} >"${legacy_dir}/docs/internal/evidence/5000-legacy.md"
git -C "${legacy_dir}" add . && git -C "${legacy_dir}" commit -q -m "legacy baseline"
printf 'small wording fix\n' >>"${legacy_dir}/docs/internal/evidence/5000-legacy.md"
git -C "${legacy_dir}" add . && git -C "${legacy_dir}" commit -q -m "edit legacy"
run_case "editing legacy note passes" 0 "${legacy_dir}"
run_case "editing legacy note passes in --files mode" 0 "${legacy_dir}" --files docs/internal/evidence/5000-legacy.md
printf 'new bad ref: .claude/new-ruling.md\n' >>"${legacy_dir}/docs/internal/evidence/5000-legacy.md"
git -C "${legacy_dir}" add . && git -C "${legacy_dir}" commit -q -m "add bad ref to legacy"
run_case "adding local ref to legacy note rejected" 1 "${legacy_dir}"
run_case "adding local ref to legacy note rejected in --files mode" 1 "${legacy_dir}" --files docs/internal/evidence/5000-legacy.md

# 8. Pre-commit --files mode on a fresh branch (HEAD == origin/main, base unset)
#    must treat a legacy note committed in HEAD as pre-existing, not newly added.
fresh_dir="$(init_repo fresh-branch)"
{
  printf '# Tip legacy note\n\nLegacy ref: .claude/tip-ruling.md.\n'
  i=1
  while [ "$i" -le 180 ]; do
    printf 'tip line %d\n' "$i"
    i=$((i + 1))
  done
} >"${fresh_dir}/docs/internal/evidence/5001-tip.md"
git -C "${fresh_dir}" add . && git -C "${fresh_dir}" commit -q -m "tip commit on main"
git -C "${fresh_dir}" update-ref refs/remotes/origin/main HEAD
printf 'harmless wording tweak\n' >>"${fresh_dir}/docs/internal/evidence/5001-tip.md"
git -C "${fresh_dir}" add docs/internal/evidence/5001-tip.md
rc=0
ESHU_EVIDENCE_HYGIENE_REPO_ROOT="${fresh_dir}" \
  bash "${verifier}" --files docs/internal/evidence/5001-tip.md >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
if [ "$rc" -ne 0 ]; then
  printf 'FAIL [fresh branch staged legacy edit passes]: expected 0, got %s\n' "$rc" >&2
  cat "${tmp_root}/out" "${tmp_root}/err" >&2
  exit 1
fi
printf 'PASS [fresh branch staged legacy edit passes]\n'

printf 'new bad ref: .claude/staged-bad.md\n' >>"${fresh_dir}/docs/internal/evidence/5001-tip.md"
git -C "${fresh_dir}" add docs/internal/evidence/5001-tip.md
rc=0
ESHU_EVIDENCE_HYGIENE_REPO_ROOT="${fresh_dir}" \
  bash "${verifier}" --files docs/internal/evidence/5001-tip.md >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
if [ "$rc" -ne 1 ]; then
  printf 'FAIL [fresh branch staged bad local ref rejected]: expected 1, got %s\n' "$rc" >&2
  cat "${tmp_root}/out" "${tmp_root}/err" >&2
  exit 1
fi
printf 'PASS [fresh branch staged bad local ref rejected]\n'

# 9. Passing verifier script path in --files mode still checks docs/internal/evidence/.
rc=0
ESHU_EVIDENCE_HYGIENE_REPO_ROOT="${raw_file_dir}" \
  ESHU_EVIDENCE_HYGIENE_BASE=HEAD~1 \
  bash "${verifier}" --files scripts/verify-evidence-hygiene.sh >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
if [ "$rc" -ne 1 ]; then
  printf 'FAIL [script-only --files catches raw file]: expected 1, got %s\n' "$rc" >&2
  cat "${tmp_root}/out" "${tmp_root}/err" >&2
  exit 1
fi
printf 'PASS [script-only --files catches raw file]\n'

# 10. GitHub pull_request merge ref (2-parent merge commit, no explicit base env)
#     uses HEAD^1 via eshu_gate_merge_ref_parent and catches a bad note added by the PR.
merge_dir="$(init_repo pr-merge-ref)"
git -C "${merge_dir}" checkout -q -b pr-branch
printf '# Missing summary on PR\n\nNo summary heading.\n' >"${merge_dir}/docs/internal/evidence/9001-pr.md"
git -C "${merge_dir}" add . && git -C "${merge_dir}" commit -q -m "pr commit"
git -C "${merge_dir}" checkout -q -
printf 'main moved\n' >"${merge_dir}/main-moved.txt"
git -C "${merge_dir}" add . && git -C "${merge_dir}" commit -q -m "main advances"
git -C "${merge_dir}" merge -q --no-ff pr-branch -m "synthetic merge ref"
rc=0
GITHUB_EVENT_NAME=pull_request \
  ESHU_EVIDENCE_HYGIENE_REPO_ROOT="${merge_dir}" \
  bash "${verifier}" >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
if [ "$rc" -ne 1 ]; then
  printf 'FAIL [pull_request merge ref catches missing summary]: expected 1, got %s\n' "$rc" >&2
  cat "${tmp_root}/out" "${tmp_root}/err" >&2
  exit 1
fi
printf 'PASS [pull_request merge ref catches missing summary]\n'

printf 'test-verify-evidence-hygiene: all cases passed\n'
