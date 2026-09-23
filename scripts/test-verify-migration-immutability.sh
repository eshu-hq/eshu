#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-migration-immutability.sh"
migrations_dir="go/internal/storage/postgres/migrations"

# The real, restored 093 content -- read from this repo rather than
# hand-copied, so the allowed-exception fixture below hashes to the exact
# c95cae27... checksum verify-migration-immutability.sh allowlists, without
# this test ever needing to know or repeat that hash itself.
shipped_093="${repo_root}/${migrations_dir}/093_cross_scope_completion_queue.sql"
if [ ! -f "${shipped_093}" ]; then
  printf 'test-verify-migration-immutability: fixture source %s is missing\n' "${shipped_093}" >&2
  exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT
# Per-run output files live under this test's own mktemp -d, not a fixed
# /tmp/... name, so two invocations of this self-test (e.g. two gates running
# in parallel) never collide on the same path.
out_file="${tmp_root}/verifier.out"
err_file="${tmp_root}/verifier.err"

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/${migrations_dir}"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  printf -- '-- 001 shipped migration\nCREATE TABLE widgets (id INT);\n' \
    >"${dir}/${migrations_dir}/001_widgets.sql"
  cp "${shipped_093}" "${dir}/${migrations_dir}/093_cross_scope_completion_queue.sql"
  mkdir -p "${dir}/go/internal/unrelated"
  printf 'package unrelated\n' >"${dir}/go/internal/unrelated/source.go"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m initial
  printf '%s\n' "${dir}"
}

# run_verifier invokes $verifier against $dir with the given base override
# (default HEAD~1, the common case for a single-extra-commit fixture).
# GITHUB_BASE_REF is always explicitly unset: these fixtures simulate the
# non-CI resolution path (and the CI path's *symptoms* via an explicit base
# override), never real network fetching.
run_verifier() {
  local dir="$1"
  local base="${2:-HEAD~1}"
  env -u GITHUB_BASE_REF \
    ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT="${dir}" \
    ESHU_MIGRATION_IMMUTABILITY_BASE="${base}" \
    "${verifier}" >"${out_file}" 2>"${err_file}"
}

expect_pass() {
  local dir="$1"
  local base="${2:-HEAD~1}"
  if ! run_verifier "${dir}" "${base}"; then
    printf 'expected verifier to pass in %s (base=%s)\n' "${dir}" "${base}" >&2
    sed -n '1,120p' "${err_file}" >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  local want_substring="$2"
  local base="${3:-HEAD~1}"
  if run_verifier "${dir}" "${base}"; then
    printf 'expected verifier to fail in %s (base=%s)\n' "${dir}" "${base}" >&2
    sed -n '1,120p' "${out_file}" >&2
    exit 1
  fi
  if ! grep -qF -- "${want_substring}" "${err_file}"; then
    printf 'expected verifier stderr in %s to mention %q, got:\n' "${dir}" "${want_substring}" >&2
    sed -n '1,120p' "${err_file}" >&2
    exit 1
  fi
}

# New migration file: allowed.
added_repo="$(init_repo added)"
printf -- '-- 002 new migration\nCREATE TABLE gadgets (id INT);\n' \
  >"${added_repo}/${migrations_dir}/002_gadgets.sql"
git -C "${added_repo}" add .
git -C "${added_repo}" commit -q -m 'add 002'
expect_pass "${added_repo}"

# Change entirely outside the migrations directory: allowed regardless of
# content, since the gate only ever looks at migrations_dir.
unrelated_repo="$(init_repo unrelated)"
printf 'package unrelated\n\n// changed\n' >"${unrelated_repo}/go/internal/unrelated/source.go"
git -C "${unrelated_repo}" add .
git -C "${unrelated_repo}" commit -q -m 'unrelated change'
expect_pass "${unrelated_repo}"

# Editing a shipped migration in place: refused. This is the #6785/#6923
# class the gate exists to catch -- a byte appended to an already-shipped
# file, in a commit that changes nothing else about it.
edited_repo="$(init_repo edited)"
printf -- '-- edited after shipping\n' >>"${edited_repo}/${migrations_dir}/001_widgets.sql"
git -C "${edited_repo}" add .
git -C "${edited_repo}" commit -q -m 'edit 001 in place'
expect_fail "${edited_repo}" "001_widgets.sql was modified"

# Deleting a shipped migration: refused.
deleted_repo="$(init_repo deleted)"
git -C "${deleted_repo}" rm -q "${migrations_dir}/001_widgets.sql"
git -C "${deleted_repo}" commit -q -m 'delete 001'
expect_fail "${deleted_repo}" "001_widgets.sql was deleted"

# Renaming a shipped migration: refused.
renamed_repo="$(init_repo renamed)"
git -C "${renamed_repo}" mv "${migrations_dir}/001_widgets.sql" "${migrations_dir}/001_widgets_renamed.sql"
git -C "${renamed_repo}" commit -q -m 'rename 001'
expect_fail "${renamed_repo}" "001_widgets.sql was renamed"

# The #7002 exception: editing 093 is allowed ONLY when the result is exactly
# the shipped bytes. First corrupt it (simulating #6785/#6923), then restore
# it in a later commit -- that restoring commit must pass.
fix_repo="$(init_repo fix)"
printf -- '-- widened in place, exactly like #6785/#6923\n' \
  >>"${fix_repo}/${migrations_dir}/093_cross_scope_completion_queue.sql"
git -C "${fix_repo}" add .
git -C "${fix_repo}" commit -q -m 'edit 093 in place (simulated #6785/#6923)'
cp "${shipped_093}" "${fix_repo}/${migrations_dir}/093_cross_scope_completion_queue.sql"
git -C "${fix_repo}" add .
git -C "${fix_repo}" commit -q -m 'restore 093 to its shipped bytes'
expect_pass "${fix_repo}"

# A DIFFERENT edit to 093 (not landing back on the exact shipped checksum)
# must still fail -- the exception is keyed on the target checksum, not on
# the path.
wrong_fix_repo="$(init_repo wrong-fix)"
printf -- '-- widened in place, exactly like #6785/#6923\n' \
  >>"${wrong_fix_repo}/${migrations_dir}/093_cross_scope_completion_queue.sql"
git -C "${wrong_fix_repo}" add .
git -C "${wrong_fix_repo}" commit -q -m 'edit 093 in place (simulated #6785/#6923)'
printf -- '-- a DIFFERENT edit, not the shipped bytes\n' \
  >>"${wrong_fix_repo}/${migrations_dir}/093_cross_scope_completion_queue.sql"
git -C "${wrong_fix_repo}" add .
git -C "${wrong_fix_repo}" commit -q -m 'edit 093 again, still not shipped bytes'
expect_fail "${wrong_fix_repo}" "093_cross_scope_completion_queue.sql was modified"

# Regression (repo-root under GIT_DIR): the verifier must derive repo_root
# from its own location, not `git rev-parse --show-toplevel`. Git hooks
# (pre-commit/pre-push) export GIT_DIR, under which `git -C scripts rev-parse
# --show-toplevel` returns <repo>/scripts instead of the repo root. Run a COPY
# of the verifier from the fixture's own scripts/, with GIT_DIR set and
# ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT unset; it must still resolve the
# fixture root and correctly flag the edited file.
gitdir_repo="$(init_repo gitdir)"
mkdir -p "${gitdir_repo}/scripts"
cp "${verifier}" "${gitdir_repo}/scripts/verify-migration-immutability.sh"
printf -- '-- edited after shipping\n' >>"${gitdir_repo}/${migrations_dir}/001_widgets.sql"
git -C "${gitdir_repo}" add .
git -C "${gitdir_repo}" commit -q -m 'edit 001 in place (gitdir fixture)'
if env -u ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT -u GITHUB_BASE_REF \
    GIT_DIR="${gitdir_repo}/.git" ESHU_MIGRATION_IMMUTABILITY_BASE=HEAD~1 \
    "${gitdir_repo}/scripts/verify-migration-immutability.sh" \
    >"${out_file}" 2>"${err_file}"; then
  printf 'expected verifier to resolve repo_root under GIT_DIR and fail\n' >&2
  exit 1
fi
if ! grep -qF -- "001_widgets.sql was modified" "${err_file}"; then
  printf 'expected GIT_DIR run to still name 001_widgets.sql, got:\n' >&2
  sed -n '1,120p' "${err_file}" >&2
  exit 1
fi

# Regression (merge-base base): with no explicit base and no GITHUB_BASE_REF,
# the verifier must fall back to merge-base(origin/main, HEAD), not HEAD~1. On
# a branch with >1 commit past origin/main, a HEAD~1 base misses the first
# commit's edit; merge-base catches the whole branch. The fixture edits 001 in
# commit B and makes an unrelated change in commit C (HEAD), with origin/main
# pinned at the initial commit: merge-base must still flag 001; a HEAD~1 base
# would diff only C and wrongly pass.
mergebase_repo="$(init_repo mergebase)"
git -C "${mergebase_repo}" update-ref refs/remotes/origin/main HEAD
printf -- '-- edited after shipping\n' >>"${mergebase_repo}/${migrations_dir}/001_widgets.sql"
git -C "${mergebase_repo}" add .
git -C "${mergebase_repo}" commit -q -m 'B: edit 001 in place'
printf 'package unrelated\n\n// unrelated commit C\n' >"${mergebase_repo}/go/internal/unrelated/source.go"
git -C "${mergebase_repo}" add .
git -C "${mergebase_repo}" commit -q -m 'C: unrelated change'
if env -u ESHU_MIGRATION_IMMUTABILITY_BASE -u GITHUB_BASE_REF \
    ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT="${mergebase_repo}" \
    "${verifier}" >"${out_file}" 2>"${err_file}"; then
  printf 'expected merge-base fallback to flag 001 (edited in commit B), but verifier passed\n' >&2
  sed -n '1,40p' "${out_file}" >&2
  exit 1
fi
if ! grep -qF -- "001_widgets.sql was modified" "${err_file}"; then
  printf 'expected merge-base run to name 001_widgets.sql, got:\n' >&2
  sed -n '1,120p' "${err_file}" >&2
  exit 1
fi

# Regression (#7002 P1-a, false RED for a branch legitimately behind main):
# origin/main advances with a NEW migration (121) after this branch diverged;
# the branch itself only makes an unrelated change and never touches
# migrations at all. The gate must PASS -- main's own later commits are not
# this branch's diff. A base resolved to an unresolved ref name (origin/main,
# not a pre-computed SHA) exercises exactly the CI shape (base="origin/$GITHUB_BASE_REF"),
# where the old two-dot fallback compared the unrelated commit's tree directly
# against origin/main's CURRENT tree and reported 121 as a spurious deletion.
behind_main_repo="$(init_repo behind-main)"
git -C "${behind_main_repo}" update-ref refs/remotes/origin/main HEAD
divergence_point="$(git -C "${behind_main_repo}" rev-parse HEAD)"
printf -- '-- 121 added on main after this branch diverged\nCREATE TABLE main_only (id INT);\n' \
  >"${behind_main_repo}/${migrations_dir}/121_main_only.sql"
git -C "${behind_main_repo}" add .
git -C "${behind_main_repo}" commit -q -m 'main: add 121 after divergence'
git -C "${behind_main_repo}" update-ref refs/remotes/origin/main HEAD
git -C "${behind_main_repo}" reset -q --hard "${divergence_point}"
printf 'package unrelated\n\n// unrelated commit while behind main\n' \
  >"${behind_main_repo}/go/internal/unrelated/source.go"
git -C "${behind_main_repo}" add .
git -C "${behind_main_repo}" commit -q -m 'branch: unrelated change while behind main'
expect_pass "${behind_main_repo}" "origin/main"

# Regression (#7002 P1-b, vacuous pass on an unresolvable base): a base that
# shares no history with HEAD at all (an orphan ref) must fail the gate
# non-zero, never silently report "no shipped migration was modified" just
# because the diff/merge-base computation itself failed.
orphan_repo="$(init_repo orphan)"
default_branch="$(git -C "${orphan_repo}" symbolic-ref --short HEAD)"
git -C "${orphan_repo}" checkout -q --orphan unrelated-history
git -C "${orphan_repo}" rm -q -rf . >/dev/null
printf 'unrelated orphan content, no shared history with the real branch\n' >"${orphan_repo}/unrelated.txt"
git -C "${orphan_repo}" add .
git -C "${orphan_repo}" commit -q -m 'orphan: unrelated history'
git -C "${orphan_repo}" update-ref refs/remotes/origin/orphan-base HEAD
git -C "${orphan_repo}" checkout -q "${default_branch}"
if run_verifier "${orphan_repo}" "origin/orphan-base"; then
  printf 'expected verifier to FAIL non-zero when the base shares no history with HEAD, but it passed\n' >&2
  sed -n '1,60p' "${out_file}" >&2
  exit 1
fi
if ! grep -qF -- "could not resolve a common ancestor" "${err_file}"; then
  printf 'expected the orphan-base run to explain the resolution failure, got:\n' >&2
  sed -n '1,60p' "${err_file}" >&2
  exit 1
fi

# Same failure mode, simpler trigger: a bogus override that does not resolve
# to any object at all.
bogus_repo="$(init_repo bogus-base)"
if run_verifier "${bogus_repo}" "0000000000000000000000000000000000000000"; then
  printf 'expected verifier to FAIL non-zero for a bogus base override, but it passed\n' >&2
  sed -n '1,60p' "${out_file}" >&2
  exit 1
fi
if ! grep -qF -- "could not resolve a common ancestor" "${err_file}"; then
  printf 'expected the bogus-base run to explain the resolution failure, got:\n' >&2
  sed -n '1,60p' "${err_file}" >&2
  exit 1
fi

printf 'test-verify-migration-immutability: all scenarios passed\n'
