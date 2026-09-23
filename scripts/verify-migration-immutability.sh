#!/usr/bin/env bash
# #7002: refuses to let a PR modify, delete, or rename a shipped Postgres
# migration under go/internal/storage/postgres/migrations/*.sql relative to
# the branch's base. #6785 and #6923 both got past
# migrations/embed_invariant_test.go's golden digest and
# migrations/migration_checksum_manifest_test.go's per-file manifest the same
# way: edit the .sql file AND update its pinned checksum in the same commit.
# Neither test diffs against a base commit, so neither can refuse an edit
# that updates its own witness. This gate is the enforcement: it looks at
# what the PR actually changed relative to its base, not what the PR claims
# about itself.
#
# The migration tracker (go/internal/storage/postgres/schema_bootstrap_lock.go)
# keys applied migrations by path + variant + checksum_sha256, so editing a
# shipped file in place changes what every already-bootstrapped database has
# on record for it -- exactly the #7002 incident (093 edited twice, breaking
# bootstrap for every database, including ops-qa, that had already recorded
# its shipped checksum). Widen behavior through a NEW guarded migration file
# instead (see migrations/README.md and migrations/checksum_alias.go for the
# pattern 112/120 established for 093).
#
# The one standing exception is #7002's own restore of 093 back to its
# originally shipped bytes: see allowed_migration_fix below. That exception is
# keyed on the exact target checksum, not on the path alone, so it permits
# only "land 093 back on precisely its shipped content" and nothing else.
#
# Usage: scripts/verify-migration-immutability.sh
# Exit codes:
#   0 - no shipped migration was modified, deleted, or renamed since base
#       (other than the allowed #7002 restore).
#   1 - a shipped migration was modified, deleted, or renamed; details on
#       stderr naming the exact file.
set -euo pipefail

repo_root="${ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  # Derive the repo root from the script's own location, NOT
  # `git rev-parse --show-toplevel`. Git hooks (pre-commit/pre-push) export
  # GIT_DIR, and with GIT_DIR set `git -C scripts rev-parse --show-toplevel`
  # returns the -C directory (<repo>/scripts) instead of the repo root, so the
  # `$repo_root/<path>` checks fail. The script always lives at <repo>/scripts/,
  # so dirname/.. is the repo root and is both worktree- and hook-safe.
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

migrations_dir="go/internal/storage/postgres/migrations"

base="${ESHU_MIGRATION_IMMUTABILITY_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  # Local (non-CI) runs: diff against the branch's divergence point from
  # origin/main, not HEAD~1 -- on a branch based on a squash-merge commit,
  # HEAD~1 is the pre-merge commit and would sweep in files this branch never
  # touched. CI keeps using GITHUB_BASE_REF above (see
  # ci-diffs-main-tip-local-diffs-merge-base / stacked-pr-gates-diff-the-wrong-base).
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || echo origin/main)"
  elif git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-migration-immutability: no base commit available, skipping\n'
    exit 0
  fi
fi

# allowed_migration_fix is the sole standing exception: path + the exact
# target sha256 the file's HEAD content must hash to. #7002 restored 093 to
# its originally shipped bytes; this entry permits precisely that outcome and
# nothing else -- a different edit to 093, or any edit to any other shipped
# migration, still fails below.
allowed_migration_fix() {
  local path="$1"
  case "$path" in
    "${migrations_dir}/093_cross_scope_completion_queue.sql")
      local want="c95cae2762bd4d0d42da4720eb0ad5545d2d032914bded15a65ab01acb92ce42"
      local got
      got="$(git -C "$repo_root" show "HEAD:${path}" 2>/dev/null | shasum -a 256 | cut -d' ' -f1)"
      [ "$got" = "$want" ]
      ;;
    *)
      return 1
      ;;
  esac
}

rows="$(git -C "$repo_root" diff --name-status --find-renames "$base"...HEAD -- "$migrations_dir" 2>/dev/null || true)"
if [ -z "$rows" ]; then
  rows="$(git -C "$repo_root" diff --name-status --find-renames "$base" HEAD -- "$migrations_dir" 2>/dev/null || true)"
fi

violations=0
while IFS=$'\t' read -r status first second; do
  [ -n "$status" ] || continue
  case "$first" in
    "${migrations_dir}"/*.sql) ;;
    *) continue ;;
  esac
  # Only direct children of migrations_dir are shipped SQL files; the embed
  # pattern (*.sql, non-recursive) never descends, so a deeper path can't be
  # one of them.
  case "${first#"${migrations_dir}"/}" in
    */*) continue ;;
  esac

  case "$status" in
    A*)
      continue
      ;;
    M*)
      if allowed_migration_fix "$first"; then
        printf 'verify-migration-immutability: %s modified but restored to its shipped checksum -- allowed (#7002)\n' "$first"
        continue
      fi
      printf 'verify-migration-immutability: %s was modified -- #7002: a shipped migration must never be edited; add a new guarded migration instead\n' "$first" >&2
      violations=1
      ;;
    D*)
      printf 'verify-migration-immutability: %s was deleted -- #7002: a shipped migration must never be deleted; leave it in place\n' "$first" >&2
      violations=1
      ;;
    R*|C*)
      target="${second:-$first}"
      printf 'verify-migration-immutability: %s was renamed to %s -- #7002: a shipped migration must never be renamed; add a new guarded migration instead\n' "$first" "$target" >&2
      violations=1
      ;;
    *)
      printf 'verify-migration-immutability: %s has unexpected diff status %s -- #7002: treat any change to a shipped migration as forbidden\n' "$first" "$status" >&2
      violations=1
      ;;
  esac
done <<<"$rows"

if [ "$violations" -ne 0 ]; then
  {
    printf '\nA shipped migration under %s changed relative to %s.\n' "$migrations_dir" "$base"
    printf 'Widen or fix its behavior through a NEW guarded migration file instead --\n'
    printf 'see %s/README.md and %s/checksum_alias.go for the pattern (#7002).\n' "$migrations_dir" "$migrations_dir"
  } >&2
  exit 1
fi

printf 'verify-migration-immutability: no shipped migration was modified, deleted, or renamed\n'
