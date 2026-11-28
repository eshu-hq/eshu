#!/usr/bin/env bash
# Regression and seeded-violation tests for scripts/verify-no-ephemeral-docs.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-no-ephemeral-docs.sh"

tmp_root="$(mktemp -d)"
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/docs/public/decisions" "${dir}/go/internal/demo" "${dir}/docs/internal/evidence"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  printf '# Demo package\n\nOwns demo logic.\n' >"${dir}/go/internal/demo/README.md"
  printf '# Legacy note\n' >"${dir}/docs/internal/evidence/legacy.md"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m "initial"
  printf '%s\n' "${dir}"
}

run_case() {
  local label="$1" expected="$2" dir="$3"
  shift 3
  local rc=0
  ESHU_NO_EPHEMERAL_DOCS_REPO_ROOT="${dir}" \
    ESHU_NO_EPHEMERAL_DOCS_BASE=HEAD~1 \
    bash "${verifier}" "$@" >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
  if [ "$rc" -ne "$expected" ]; then
    printf 'FAIL [%s]: expected exit %s, got %s\n' "$label" "$expected" "$rc" >&2
    cat "${tmp_root}/out" "${tmp_root}/err" >&2
    exit 1
  fi
  printf 'PASS [%s]\n' "$label"
}

# 1. Clean architecture README edit and durable ADR pass (GREEN).
clean_dir="$(init_repo clean)"
printf '\n## Invariants\n\nAll writes are idempotent.\n' >>"${clean_dir}/go/internal/demo/README.md"
printf '# ADR 001\n\nDurable decision.\n' >"${clean_dir}/docs/public/decisions/001-demo.md"
git -C "${clean_dir}" add . && git -C "${clean_dir}" commit -q -m "clean docs"
run_case "clean README and ADR pass" 0 "${clean_dir}"
run_case "clean README and ADR pass in --files mode" 0 "${clean_dir}" --files go/internal/demo/README.md docs/public/decisions/001-demo.md

# 2. New file under docs/internal/evidence/ fails (RED).
ev_dir="$(init_repo new-evidence)"
printf '# PR shim\n\n## Summary\n\nMeasured locally.\n' >"${ev_dir}/docs/internal/evidence/1234-shim.md"
git -C "${ev_dir}" add . && git -C "${ev_dir}" commit -q -m "add evidence"
run_case "new docs/internal/evidence file rejected" 1 "${ev_dir}"
run_case "new docs/internal/evidence file rejected in --files mode" 1 "${ev_dir}" --files docs/internal/evidence/1234-shim.md

# 3. New file under docs/internal/arbiter/ fails (RED).
arb_dir="$(init_repo new-arbiter)"
mkdir -p "${arb_dir}/docs/internal/arbiter"
printf '# Ruling\n' >"${arb_dir}/docs/internal/arbiter/1234-ruling.md"
git -C "${arb_dir}" add . && git -C "${arb_dir}" commit -q -m "add arbiter file"
run_case "new docs/internal/arbiter file rejected" 1 "${arb_dir}"
run_case "new docs/internal/arbiter file rejected in --files mode" 1 "${arb_dir}" --files docs/internal/arbiter/1234-ruling.md

# 4. New file under docs/superpowers/ and docs/plans/ fails (RED).
plan_dir="$(init_repo new-plan)"
mkdir -p "${plan_dir}/docs/superpowers" "${plan_dir}/docs/plans"
printf '# Scratch\n' >"${plan_dir}/docs/superpowers/scratch.md"
printf '# Plan\n' >"${plan_dir}/docs/plans/scratch.md"
git -C "${plan_dir}" add . && git -C "${plan_dir}" commit -q -m "add plans"
run_case "new docs/superpowers and docs/plans rejected" 1 "${plan_dir}"

# 5. New go/**/evidence-*.md fails (RED).
go_ev_dir="$(init_repo go-evidence)"
printf '# Local proof\n' >"${go_ev_dir}/go/internal/demo/evidence-1234-proof.md"
git -C "${go_ev_dir}" add . && git -C "${go_ev_dir}" commit -q -m "add go evidence"
run_case "new go/**/evidence-*.md rejected" 1 "${go_ev_dir}"
run_case "new go/**/evidence-*.md rejected in --files mode" 1 "${go_ev_dir}" --files go/internal/demo/evidence-1234-proof.md

# 6. Appending Performance/No-Regression/Observability markers to go/**/README.md fails (RED).
marker_dir="$(init_repo go-markers)"
printf '\nNo-Regression Evidence (#1234): unchanged query shape.\n' >>"${marker_dir}/go/internal/demo/README.md"
git -C "${marker_dir}" add . && git -C "${marker_dir}" commit -q -m "append marker"
run_case "PR marker added to go/**/README.md rejected" 1 "${marker_dir}"
run_case "PR marker added to go/**/README.md rejected in --files mode" 1 "${marker_dir}" --files go/internal/demo/README.md

# 7. Deleting legacy evidence files passes cleanly (GREEN).
del_dir="$(init_repo delete-legacy)"
git -C "${del_dir}" rm -q docs/internal/evidence/legacy.md
git -C "${del_dir}" commit -q -m "remove legacy evidence"
run_case "deleting legacy evidence file passes" 0 "${del_dir}"

# 8. GitHub pull_request merge ref (2-parent merge commit) catches PR violation (RED).
merge_dir="$(init_repo pr-merge-ref)"
git -C "${merge_dir}" checkout -q -b pr-branch
printf '# PR note\n' >"${merge_dir}/docs/internal/evidence/9999-pr.md"
git -C "${merge_dir}" add . && git -C "${merge_dir}" commit -q -m "pr commit"
git -C "${merge_dir}" checkout -q -
printf 'main moved\n' >"${merge_dir}/main.txt"
git -C "${merge_dir}" add . && git -C "${merge_dir}" commit -q -m "main advances"
git -C "${merge_dir}" merge -q --no-ff pr-branch -m "synthetic merge ref"
rc=0
GITHUB_EVENT_NAME=pull_request \
  ESHU_NO_EPHEMERAL_DOCS_REPO_ROOT="${merge_dir}" \
  bash "${verifier}" >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
if [ "$rc" -ne 1 ]; then
  printf 'FAIL [pull_request merge ref catches new evidence]: expected 1, got %s\n' "$rc" >&2
  cat "${tmp_root}/out" "${tmp_root}/err" >&2
  exit 1
fi
printf 'PASS [pull_request merge ref catches new evidence]\n'

printf 'test-verify-no-ephemeral-docs: all cases passed\n'
