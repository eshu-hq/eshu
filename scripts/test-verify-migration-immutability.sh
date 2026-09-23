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

run_verifier() {
  local dir="$1"
  ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT="${dir}" \
    ESHU_MIGRATION_IMMUTABILITY_BASE=HEAD~1 \
    "${verifier}" >/tmp/eshu-migration-immutability.out 2>/tmp/eshu-migration-immutability.err
}

expect_pass() {
  local dir="$1"
  if ! run_verifier "${dir}"; then
    printf 'expected verifier to pass in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-migration-immutability.err >&2
    exit 1
  fi
}

expect_fail() {
  local dir="$1"
  local want_substring="$2"
  if run_verifier "${dir}"; then
    printf 'expected verifier to fail in %s\n' "${dir}" >&2
    sed -n '1,120p' /tmp/eshu-migration-immutability.out >&2
    exit 1
  fi
  if ! grep -qF -- "${want_substring}" /tmp/eshu-migration-immutability.err; then
    printf 'expected verifier stderr in %s to mention %q, got:\n' "${dir}" "${want_substring}" >&2
    sed -n '1,120p' /tmp/eshu-migration-immutability.err >&2
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
    >/tmp/eshu-migration-immutability.out 2>/tmp/eshu-migration-immutability.err; then
  printf 'expected verifier to resolve repo_root under GIT_DIR and fail\n' >&2
  exit 1
fi
if ! grep -qF -- "001_widgets.sql was modified" /tmp/eshu-migration-immutability.err; then
  printf 'expected GIT_DIR run to still name 001_widgets.sql, got:\n' >&2
  sed -n '1,120p' /tmp/eshu-migration-immutability.err >&2
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
    "${verifier}" >/tmp/eshu-migration-immutability.out 2>/tmp/eshu-migration-immutability.err; then
  printf 'expected merge-base fallback to flag 001 (edited in commit B), but verifier passed\n' >&2
  sed -n '1,40p' /tmp/eshu-migration-immutability.out >&2
  exit 1
fi
if ! grep -qF -- "001_widgets.sql was modified" /tmp/eshu-migration-immutability.err; then
  printf 'expected merge-base run to name 001_widgets.sql, got:\n' >&2
  sed -n '1,120p' /tmp/eshu-migration-immutability.err >&2
  exit 1
fi

printf 'test-verify-migration-immutability: all scenarios passed\n'
