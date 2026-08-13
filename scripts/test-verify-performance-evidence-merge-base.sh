#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-performance-evidence.sh, split out to keep
# that file under the repo's 500-line cap. Invoked from its tail.
#
# Regression coverage for the local base fallback. Every other test in this
# suite pins ESHU_PERFORMANCE_EVIDENCE_BASE (usually to HEAD~1) or sets
# GITHUB_BASE_REF, so none of them exercise the path a developer's
# `make pre-pr` actually takes: neither variable set. That path used to
# default straight to HEAD~1, which scopes the gate to the last commit
# instead of the branch.
#
# The consequence is a gate that passes without checking anything. A branch
# that changes hot-path Cypher, graph writes, queues, or leases in an earlier
# commit and ends on an innocuous docs or generated-file commit saw only that
# tip commit, reported "no hot Cypher/concurrency/runtime files changed", and
# exited 0 with no performance evidence anywhere on the branch. Observed on a
# real branch: the HEAD~1 fallback saw 1 file and passed; the merge base with
# origin/main saw 26 and found the hot-path changes.
#
# Case 1 below is the one that matters -- it FAILS against the old HEAD~1
# fallback and passes only once the verifier resolves the merge base. Cases 2
# and 3 guard the opposite direction, so the widened window cannot start
# firing on branches that are genuinely fine.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

# Builds a fixture with a real origin/main to resolve a merge base against,
# and a feature branch checked out on top of it. Returns the branch dir.
init_branch_repo() {
  local name="$1"
  local origin_dir="${tmp_root}/${name}-origin"
  local branch_dir="${tmp_root}/${name}"

  mkdir -p "${origin_dir}"
  git -C "${origin_dir}" init -q
  git -C "${origin_dir}" config user.email "test@example.invalid"
  git -C "${origin_dir}" config user.name "Eshu Test"
  git -C "${origin_dir}" checkout -q -b main
  mkdir -p "${origin_dir}/docs/public/reference" \
    "${origin_dir}/go/internal/storage/cypher"
  printf '# Local Performance\n' \
    >"${origin_dir}/docs/public/reference/local-performance-envelope.md"
  printf '# Code Coverage\n' \
    >"${origin_dir}/docs/public/reference/code-coverage.md"
  printf 'package cypher\n' >"${origin_dir}/go/internal/storage/cypher/doc.go"
  git -C "${origin_dir}" add .
  git -C "${origin_dir}" commit -q -m 'origin main baseline'

  git clone -q "${origin_dir}" "${branch_dir}"
  git -C "${branch_dir}" config user.email "test@example.invalid"
  git -C "${branch_dir}" config user.name "Eshu Test"
  git -C "${branch_dir}" checkout -q -b feature
  printf '%s\n' "${branch_dir}"
}

# Writes the hot-path change: a Cypher MERGE/UNWIND write in a location the
# gate treats as hot by both location and content.
add_hot_path_commit() {
  local dir="$1"
  printf 'package cypher\n\nconst writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
    >"${dir}/go/internal/storage/cypher/writer.go"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m 'branch commit A: hot-path Cypher write'
}

# Writes the innocuous tip commit that used to hide the branch from the gate:
# a docs edit, exactly the shape observed on the real branch.
add_innocuous_tip_commit() {
  local dir="$1"
  printf '# Code Coverage\n\nRegenerated coverage table.\n' \
    >"${dir}/docs/public/reference/code-coverage.md"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m 'branch commit B: regenerate coverage docs'
}

# Runs the verifier the way `make pre-pr` does: no explicit base override and
# no GITHUB_BASE_REF, so the local fallback under test is what resolves.
run_local_shaped() {
  local dir="$1"
  env -u ESHU_PERFORMANCE_EVIDENCE_BASE -u GITHUB_BASE_REF \
    ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${dir}" \
    "${verifier}" >/tmp/eshu-perf-gate-merge-base.out \
    2>/tmp/eshu-perf-gate-merge-base.err
}

dump_run() {
  printf -- '--- stdout ---\n' >&2
  sed -n '1,120p' /tmp/eshu-perf-gate-merge-base.out >&2
  printf -- '--- stderr ---\n' >&2
  sed -n '1,120p' /tmp/eshu-perf-gate-merge-base.err >&2
}

# Case 1: the defect. Hot-path change in commit A, innocuous docs commit at the
# tip, no performance evidence anywhere on the branch. Under the old HEAD~1
# fallback the gate saw only the docs commit and exited 0. It must now fail.
case1="$(init_branch_repo case1-unproven)"
add_hot_path_commit "${case1}"
add_innocuous_tip_commit "${case1}"
if run_local_shaped "${case1}"; then
  printf 'expected the gate to FAIL: the branch changes hot-path Cypher in an\n' >&2
  printf 'earlier commit and carries no performance evidence, but the tip\n' >&2
  printf 'commit is innocuous. A pass here means the base fell back to HEAD~1\n' >&2
  printf 'and the gate scoped to the last commit instead of the branch.\n' >&2
  dump_run
  exit 1
fi
if ! rg -q 'Performance Evidence' /tmp/eshu-perf-gate-merge-base.out \
  /tmp/eshu-perf-gate-merge-base.err; then
  printf 'gate failed, but not for the missing-evidence reason under test\n' >&2
  dump_run
  exit 1
fi

# Case 2: no hot-path change anywhere on the branch. The widened window must
# not invent a reason to fire -- a docs-only branch still passes.
case2="$(init_branch_repo case2-docs-only)"
printf '# Local Performance\n\nFirst docs edit.\n' \
  >"${case2}/docs/public/reference/local-performance-envelope.md"
git -C "${case2}" add .
git -C "${case2}" commit -q -m 'branch commit A: docs edit'
add_innocuous_tip_commit "${case2}"
if ! run_local_shaped "${case2}"; then
  printf 'expected a docs-only branch to PASS; the widened base must not make\n' >&2
  printf 'the gate fire on branches with no hot-path change at all\n' >&2
  dump_run
  exit 1
fi

# Case 3: hot-path change in an earlier commit WITH the required markers added
# by that same branch, innocuous tip commit. The wider window must find the
# markers too, not just the hot files -- otherwise the fix would turn every
# properly-evidenced multi-commit branch red.
case3="$(init_branch_repo case3-evidenced)"
cat >"${case3}/go/internal/storage/cypher/README.md" <<'MD'
# Cypher Storage

Performance Evidence: writerQuery UNWIND/MERGE benchmarked flat against the
prior batch loop on the 20-repo local corpus; p50 and p95 unchanged, terminal
queue depth 0.

No-Observability-Change: the existing writer span and metric coverage already
instruments this MERGE path.
MD
add_hot_path_commit "${case3}"
add_innocuous_tip_commit "${case3}"
if ! run_local_shaped "${case3}"; then
  printf 'expected an evidenced branch to PASS: the markers were added by this\n' >&2
  printf 'branch in the same commit as the hot-path change\n' >&2
  dump_run
  exit 1
fi

printf 'verify-performance-evidence merge-base resolution test passed\n'
