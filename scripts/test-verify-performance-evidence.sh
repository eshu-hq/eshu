#!/usr/bin/env bash
# Regression and seeded-violation tests for scripts/verify-performance-evidence.sh
# (PR-body enforcement; tracked files must NOT satisfy the gate).
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local dir="${tmp_root}/$1"
  mkdir -p "${dir}/go/internal/reducer" "${dir}/docs/internal/evidence"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  cat >"${dir}/go/internal/reducer/worker.go" <<'GO'
package reducer

func Run() int { return 1 }
GO
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m "initial"
  printf '%s\n' "${dir}"
}

expect_rc() {
  local label="$1" want="$2"
  shift 2
  local rc=0
  "$@" >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
  if [ "$rc" -ne "$want" ]; then
    printf 'FAIL [%s]: want exit %s, got %s\n' "$label" "$want" "$rc" >&2
    cat "${tmp_root}/out" "${tmp_root}/err" >&2
    exit 1
  fi
  printf 'PASS [%s]\n' "$label"
}

# 1. Hot-path change with empty PR body fails (RED).
hot_dir="$(init_repo hot)"
cat >"${hot_dir}/go/internal/reducer/worker.go" <<'GO'
package reducer

func Run() int { return 2 }
GO
git -C "${hot_dir}" add . && git -C "${hot_dir}" commit -q -m "change reducer"
empty_body="${tmp_root}/empty.md"
printf 'Refs #1234.\n\nJust a summary.\n' >"${empty_body}"
expect_rc "hot change with empty PR body fails" 1 \
  env ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${hot_dir}" ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
  bash "${verifier}" --pr-body "${empty_body}"

# 2. Hot-path change with markers ONLY in tracked markdown file still fails when PR body lacks them (RED).
printf 'Performance Evidence: 10ms -> 5ms\nObservability Evidence: metric x\n' >"${hot_dir}/go/internal/reducer/README.md"
git -C "${hot_dir}" add . && git -C "${hot_dir}" commit -q -m "add tracked marker"
expect_rc "tracked markdown marker does not satisfy PR-body gate" 1 \
  env ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${hot_dir}" ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~2 \
  bash "${verifier}" --pr-body "${empty_body}"

# 3. Hot-path change with both markers in --pr-body passes (GREEN).
good_body="${tmp_root}/good.md"
cat >"${good_body}" <<'MD'
Refs #1234.

No-Regression Evidence: unit bench unchanged at 1.2ms on Postgres 16.
No-Observability-Change: existing eshu_dp_reducer_items_total covers this path.
MD
expect_rc "hot change with both markers in --pr-body passes" 0 \
  env ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${hot_dir}" ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~2 \
  bash "${verifier}" --pr-body "${good_body}"

# 4. Hot-path change with both markers in ESHU_PERFORMANCE_EVIDENCE_PR_BODY env var passes (GREEN).
expect_rc "hot change with env PR body passes" 0 \
  env ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${hot_dir}" ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~2 \
  ESHU_PERFORMANCE_EVIDENCE_PR_BODY="$(cat "${good_body}")" \
  bash "${verifier}"

# 5. Missing observability marker fails (RED).
perf_only="${tmp_root}/perf-only.md"
printf 'Performance Evidence: 10ms -> 4ms\n' >"${perf_only}"
expect_rc "missing observability marker fails" 1 \
  env ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${hot_dir}" ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~2 \
  bash "${verifier}" --pr-body "${perf_only}"

# 6. Non-PR event (push to main) with no PR body skips cleanly (GREEN).
expect_rc "non-PR event with no PR body skips cleanly" 0 \
  env ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${hot_dir}" ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~2 \
  GITHUB_EVENT_NAME=push \
  bash "${verifier}"

# 7. Comment-only change passes even with empty PR body (GREEN).
comment_dir="$(init_repo comment-only)"
cat >"${comment_dir}/go/internal/reducer/worker.go" <<'GO'
// Clarify comment only.
package reducer

func Run() int { return 1 }
GO
git -C "${comment_dir}" add . && git -C "${comment_dir}" commit -q -m "comment only"
expect_rc "comment-only hot file passes" 0 \
  env ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${comment_dir}" ESHU_PERFORMANCE_EVIDENCE_BASE=HEAD~1 \
  bash "${verifier}" --pr-body "${empty_body}"

printf 'test-verify-performance-evidence: all cases passed\n'
