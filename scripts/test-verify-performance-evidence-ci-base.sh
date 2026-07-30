#!/usr/bin/env bash
set -euo pipefail

# Companion to scripts/test-verify-performance-evidence.sh, split out to keep
# that file under the repo's 500-line cap. Invoked from its tail.
#
# Regression coverage for eshu-hq/eshu#5542 P0: every OTHER test in this
# suite (both this file's siblings) sets ESHU_PERFORMANCE_EVIDENCE_BASE
# directly, which bypasses the GITHUB_BASE_REF -> origin/$GITHUB_BASE_REF
# resolution entirely -- the actual path real CI runs. A green run of the
# rest of the suite proves nothing about that path. This file exercises it
# for real, with two genuine git behaviors (not mocks):
#
#  1. `git fetch <remote> <branch>` with no destination refspec on the
#     command line only ever updates FETCH_HEAD, never
#     refs/remotes/<remote>/<branch> -- documented git behavior, independent
#     of any configured remote.<name>.fetch. actions/checkout@v5's actual
#     narrow/shallow setup hits exactly this, so the old
#     `git fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF"` never
#     created origin/$GITHUB_BASE_REF and `base` silently fell back to
#     HEAD~1 (last commit only) in every real CI run.
#  2. Even with (1) fixed, a resolved base ref from a genuinely disconnected
#     history -- standing in for a shallow checkout where the fetched base
#     tip and the PR's local commit graph share no common ancestor object --
#     makes the three-dot diff form fail with "no merge base". Both the
#     shared _perf_diff_cache and, through it, added_lines_for_evidence_file
#     must fall back to the two-dot form.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-performance-evidence.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

# Part 1: prove the raw git behavior the fix depends on, independent of the
# verifier script, so a regression in the fetch command is caught even if
# this file's own plumbing changes.
origin_probe="${tmp_root}/origin-probe"
mkdir -p "${origin_probe}"
git -C "${origin_probe}" init -q
git -C "${origin_probe}" config user.email "test@example.invalid"
git -C "${origin_probe}" config user.name "Eshu Test"
git -C "${origin_probe}" checkout -q -b main
printf 'baseline\n' >"${origin_probe}/file.txt"
git -C "${origin_probe}" add .
git -C "${origin_probe}" commit -q -m baseline

pr_probe="${tmp_root}/pr-probe"
mkdir -p "${pr_probe}"
git -C "${pr_probe}" init -q
git -C "${pr_probe}" config user.email "test@example.invalid"
git -C "${pr_probe}" config user.name "Eshu Test"
git -C "${pr_probe}" remote add origin "${origin_probe}"
# A plain `git remote add` configures the default wildcard fetch refspec
# (+refs/heads/*:refs/remotes/origin/*), under which even a bareword fetch
# DOES create origin/main -- that is NOT what actions/checkout@v5 does.
# It configures a NARROW refspec covering only the ref it actually needs
# (the PR's own branch), never a wildcard and never one that names the base
# branch. Reproduce that exactly, or this probe does not test the real bug.
git -C "${pr_probe}" config --unset-all remote.origin.fetch
git -C "${pr_probe}" config --add remote.origin.fetch \
  '+refs/heads/unrelated-pr-branch:refs/remotes/origin/unrelated-pr-branch'
git -C "${pr_probe}" commit -q -m empty --allow-empty

git -C "${pr_probe}" fetch --no-tags --depth=1 origin main >/dev/null 2>&1 || true
if git -C "${pr_probe}" rev-parse --verify origin/main >/dev/null 2>&1; then
  printf 'assumption broke: bareword fetch created origin/main (git behavior changed?)\n' >&2
  exit 1
fi

git -C "${pr_probe}" fetch --no-tags --depth=1 origin \
  "main:refs/remotes/origin/main" >/dev/null 2>&1
if ! git -C "${pr_probe}" rev-parse --verify origin/main >/dev/null 2>&1; then
  printf 'expected explicit-refspec fetch to create origin/main\n' >&2
  exit 1
fi

# Part 2: exercise the verifier itself through GITHUB_BASE_REF against a
# 2-commit PR (commit A adds evidence, commit B is the hot-path change) whose
# base is a DISCONNECTED history -- a deterministic stand-in for a shallow
# checkout where the base tip and the PR's local commits share no common
# ancestor object.
origin_dir="${tmp_root}/origin"
mkdir -p "${origin_dir}"
git -C "${origin_dir}" init -q
git -C "${origin_dir}" config user.email "test@example.invalid"
git -C "${origin_dir}" config user.name "Eshu Test"
git -C "${origin_dir}" checkout -q -b main
printf '# Unrelated Origin Baseline\n' >"${origin_dir}/UNRELATED.md"
git -C "${origin_dir}" add .
git -C "${origin_dir}" commit -q -m 'origin main baseline (disconnected from the PR side)'

pr_dir="${tmp_root}/pr"
mkdir -p "${pr_dir}/go/internal/storage/cypher"
git -C "${pr_dir}" init -q
git -C "${pr_dir}" config user.email "test@example.invalid"
git -C "${pr_dir}" config user.name "Eshu Test"
git -C "${pr_dir}" remote add origin "${origin_dir}"
# Narrow the fetch refspec exactly like Part 1's probe: real CI never
# configures a wildcard or a refspec naming the base branch, so a regression
# back to the bareword fetch must be caught here too, not papered over by
# the default wildcard matching "main" anyway.
git -C "${pr_dir}" config --unset-all remote.origin.fetch
git -C "${pr_dir}" config --add remote.origin.fetch \
  '+refs/heads/unrelated-pr-branch:refs/remotes/origin/unrelated-pr-branch'

# Commit A: the PR's first commit -- adds the evidence marker.
printf 'package cypher\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\n' \
  >"${pr_dir}/go/internal/storage/cypher/writer.go"
cat >"${pr_dir}/go/internal/storage/cypher/README.md" <<'MD'
# Cypher Storage

## Current Evidence (this PR)

Performance Evidence: writerQuery MERGE benchmarked flat vs baseline on the
20-repo local NornicDB corpus; terminal queue depth 0, p50/p95 unchanged.

No-Observability-Change: existing writer span/metric coverage already
instruments this MERGE path.
MD
git -C "${pr_dir}" add .
git -C "${pr_dir}" commit -q -m 'PR commit A: add evidence ahead of the hot-path change'

# Commit B: the PR's second (HEAD) commit -- the hot-path change itself.
printf 'package cypher\n\nconst readerQuery = "MATCH (r:Repository {id: $id}) RETURN r"\nconst writerQuery = "UNWIND $rows AS row MERGE (n:File {uid: row.uid})"\n' \
  >"${pr_dir}/go/internal/storage/cypher/writer.go"
git -C "${pr_dir}" add .
git -C "${pr_dir}" commit -q -m 'PR commit B: writerQuery MERGE (hot change)'

run_ci_shaped() {
  env -u ESHU_PERFORMANCE_EVIDENCE_BASE \
    ESHU_PERFORMANCE_EVIDENCE_REPO_ROOT="${pr_dir}" \
    GITHUB_BASE_REF=main \
    "${verifier}" >/tmp/eshu-perf-gate-ci-base.out 2>/tmp/eshu-perf-gate-ci-base.err
}

if ! run_ci_shaped; then
  printf 'expected the CI-shaped verifier run to PASS (evidence exists in commit A)\n' >&2
  printf -- '--- stdout ---\n' >&2
  sed -n '1,120p' /tmp/eshu-perf-gate-ci-base.out >&2
  printf -- '--- stderr ---\n' >&2
  sed -n '1,120p' /tmp/eshu-perf-gate-ci-base.err >&2
  exit 1
fi

if ! git -C "${pr_dir}" rev-parse --verify origin/main >/dev/null 2>&1; then
  printf 'expected origin/main to have been created by the verifier fetch\n' >&2
  exit 1
fi

if ! git -C "${pr_dir}" merge-base origin/main HEAD >/dev/null 2>&1; then
  : # Confirms the disconnected-history setup actually forces "no merge
    # base" -- if this ever starts succeeding, the fixture no longer
    # exercises the two-dot fallback and must be revisited.
else
  printf 'expected origin/main and the PR HEAD to have no merge base (fixture no longer disconnected)\n' >&2
  exit 1
fi

printf 'verify-performance-evidence CI base resolution test passed\n'
