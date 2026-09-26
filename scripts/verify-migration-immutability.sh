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
# This gate always diffs against a resolved common-ancestor COMMIT, never a
# branch tip: resolving to a concrete merge-base up front, then diffing
# two-dot against that SHA, means the diff can never accidentally include
# commits the base branch made AFTER this branch diverged (which would show up
# as spurious deletions/modifications of migrations this PR never touched --
# exactly wrong for a gate whose whole job is refusing D/M/R). A base that
# cannot be resolved to a merge-base (shallow history, an orphan ref, a bogus
# override) is a hard failure, never a silent pass -- see find_merge_base.
#
# Base resolution order (#7002 P1, both a silent-narrowing bug class):
#   1. ESHU_MIGRATION_IMMUTABILITY_BASE, if set -- an explicit override always
#      wins.
#   2. GITHUB_BASE_REF (real CI PR context): origin/$GITHUB_BASE_REF MUST
#      resolve, after an --update-shallow fetch. If it still does not, this
#      is a hard failure (exit 1) -- never fall back to a narrower base here,
#      since that would silently shrink the diffed range below the PR's
#      actual base.
#   3. origin/main, if present (local/non-CI runs with a normal remote).
#   4. Otherwise (no origin/main, no GITHUB_BASE_REF): walk to the root
#      commit(s) reachable from HEAD (`git rev-list --max-parents=0 HEAD`)
#      and diff the whole local history from there, with a loud stderr
#      WARNING. A HEAD~1-only fallback here used to silently miss an earlier
#      commit's violation on a >1-commit branch; a root walk always resolves
#      locally (no network) and degrades correctly for a genuine
#      single-commit repo (root == HEAD == an empty diff), so it needs no
#      separate "skip, no history" case.
#
# Usage: scripts/verify-migration-immutability.sh
# Exit codes:
#   0 - no shipped migration was modified, deleted, or renamed since base
#       (other than the allowed #7002 restore).
#   1 - a shipped migration was modified, deleted, or renamed, OR the base
#       could not be resolved to an actual common ancestor of HEAD; details on
#       stderr naming the exact file or the resolution failure.
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

# find_merge_base prints a concrete merge-base commit SHA between ref and HEAD
# on stdout and returns 0, or prints nothing and returns 1. It first tries the
# direct computation; if that fails and the resolved ref is an origin branch,
# it makes a bounded number of attempts to deepen both HEAD's own history and
# that fetched base ref before giving up. This covers both pull_request
# (GITHUB_BASE_REF) and merge_group: GitHub does not set GITHUB_BASE_REF on the
# latter, but actions/checkout still exposes origin/main. It never treats
# "could not compute" as "nothing changed" -- that is the caller's job, and
# the caller must fail loud on a 1 return, not skip.
find_merge_base() {
  local ref="$1" mb remote branch
  if mb="$(git -C "$repo_root" merge-base "$ref" HEAD 2>/dev/null)"; then
    printf '%s\n' "$mb"
    return 0
  fi
  case "$ref" in
    origin/*)
      remote="origin"
      branch="${ref#origin/}"
      ;;
    *)
      # A caller-selected SHA, tag, or non-origin ref has no trustworthy
      # remote branch to deepen toward. Failing here preserves the exact
      # caller range instead of guessing a narrower replacement.
      return 1
      ;;
  esac
  if [ -z "$branch" ]; then
    return 1
  fi
  local depth=100
  while [ "$depth" -le 3200 ]; do
    if ! git -C "$repo_root" fetch --no-tags --deepen="$depth" >/dev/null 2>&1; then
      printf 'verify-migration-immutability: failed to deepen HEAD history while resolving %s.\n' "$ref" >&2
      return 1
    fi
    # Keep the remote-tracking ref synchronized while deepening it. An
    # explicit destination refspec prevents a FETCH_HEAD-only update; the
    # shallow update flag permits the remote tip to move. If it moves to
    # unrelated history, merge-base remains empty and this function fails
    # closed after the bounded attempts below.
    if ! git -C "$repo_root" fetch --no-tags --update-shallow --deepen="$depth" "$remote" \
      "${branch}:refs/remotes/${remote}/${branch}" >/dev/null 2>&1; then
      printf 'verify-migration-immutability: failed to deepen %s while resolving its common ancestor.\n' "$ref" >&2
      return 1
    fi
    if mb="$(git -C "$repo_root" merge-base "$ref" HEAD 2>/dev/null)"; then
      printf '%s\n' "$mb"
      return 0
    fi
    depth=$((depth * 4))
  done
  return 1
}

base_ref="${ESHU_MIGRATION_IMMUTABILITY_BASE:-}"
if [ -z "$base_ref" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  # An explicit destination refspec is required: `git fetch origin <branch>`
  # with no `:<dst>` only ever updates FETCH_HEAD, never
  # refs/remotes/origin/<branch> (eshu-hq/eshu#5542). --update-shallow is
  # required too: a shallow checkout can already hold this ref from an
  # earlier fetch, and git refuses to move an existing ref in a shallow repo
  # ("shallow roots are not allowed to be updated") unless told to (#7002
  # P1-b) -- without it the fetch silently no-ops and origin/$GITHUB_BASE_REF
  # is left stale or absent. No --depth limit here: a shallow single-commit
  # fetch of the base tip cannot reach a merge-base with HEAD once the branch
  # has fallen behind, which is exactly the case this gate most needs to get
  # right. find_merge_base deepens further if this still is not enough.
  #
  # If origin/$GITHUB_BASE_REF still does not resolve after this, FAIL HARD
  # rather than fall through to a narrower base (origin/main, a root walk):
  # a CI PR run that cannot see its real base must never silently diff a
  # smaller range than the actual PR -- that is the #7002 P1 class this gate
  # exists to prevent, just on the resolution path instead of the diff path.
  git -C "$repo_root" fetch --no-tags --update-shallow origin \
    "${GITHUB_BASE_REF}:refs/remotes/origin/${GITHUB_BASE_REF}" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base_ref="origin/$GITHUB_BASE_REF"
  else
    {
      printf 'verify-migration-immutability: GITHUB_BASE_REF=%s is set but origin/%s ' \
        "$GITHUB_BASE_REF" "$GITHUB_BASE_REF"
      printf 'does not resolve after an --update-shallow fetch.\n'
      printf 'Refusing to fall back to a narrower base (origin/main, a root walk) -- that\n'
      printf 'would silently shrink the diffed range below the PR'"'"'s actual base. Give this\n'
      printf 'job a deeper checkout (fetch-depth: 0) or a reachable origin/%s.\n' "$GITHUB_BASE_REF"
    } >&2
    exit 1
  fi
fi
if [ -z "$base_ref" ]; then
  # Local (non-CI) runs: prefer origin/main when present.
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base_ref="origin/main"
  else
    # No origin/main and no GITHUB_BASE_REF: there is no reliable "PR base"
    # signal at all. A HEAD~1-only fallback here used to diff just the last
    # commit, silently missing an EARLIER commit's edit on a >1-commit local
    # branch (#7002 P1-a) -- the same class of bug as the CI path above, on a
    # different resolution branch. Walk to the root commit(s) reachable from
    # HEAD instead: this always resolves purely locally (no network needed),
    # and for a genuine single-commit repo/fixture the root IS HEAD, so the
    # diff is naturally empty -- no separate "skip, no history" case is
    # needed. `rev-list --max-parents=0` can print more than one root when
    # unrelated histories were merged; taking the oldest (last line) is a
    # deliberately conservative choice -- it only ever widens the diffed
    # range, never narrows it.
    #
    # In a shallow clone the "root" is only the graft boundary, so the walk
    # would diff a truncated range and could miss an earlier edit (#7002
    # review F-5). There is no base to deepen toward here, so fail instead.
    if [ "$(git -C "$repo_root" rev-parse --is-shallow-repository 2>/dev/null)" = "true" ]; then
      {
        printf 'verify-migration-immutability: shallow checkout with no origin/main, no GITHUB_BASE_REF\n'
        printf 'and no ESHU_MIGRATION_IMMUTABILITY_BASE -- the root commit is only the shallow\n'
        printf 'boundary, so the full history cannot be diffed. Refusing to pass. Fetch the base\n'
        printf 'branch, unshallow, or set ESHU_MIGRATION_IMMUTABILITY_BASE.\n'
      } >&2
      exit 1
    fi
    base_ref="$(git -C "$repo_root" rev-list --max-parents=0 HEAD | tail -1)"
    {
      printf 'verify-migration-immutability: WARNING no origin/main and no GITHUB_BASE_REF -- '
      printf 'diffing the full local history from its root commit %s.\n' "$base_ref"
      printf 'This can be slow on a long-lived branch; set ESHU_MIGRATION_IMMUTABILITY_BASE to\n'
      printf 'pin a real base explicitly.\n'
    } >&2
  fi
fi
printf 'verify-migration-immutability: resolved base ref: %s\n' "$base_ref"

base_commit="$(find_merge_base "$base_ref")" || {
  {
    printf 'verify-migration-immutability: could not resolve a common ancestor between %s and HEAD.\n' "$base_ref"
    printf 'This is a hard failure, not a pass: a gate that cannot see the real diff must never\n'
    printf 'silently accept an edit to a shipped migration. Likely causes: %s is an orphan ref\n' "$base_ref"
    printf 'unrelated to this history, an invalid override, or (in CI) history too shallow to\n'
    printf 'reach the divergence point even after deepening -- consider a deeper checkout for\n'
    printf 'this job (fetch-depth: 0).\n'
  } >&2
  exit 1
}
printf 'verify-migration-immutability: merge-base commit: %s\n' "$base_commit"

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

# Two-dot diff against the already-resolved common-ancestor SHA: no further
# merge-base computation happens here, so this cannot fail the way a "..."
# diff can on an unreachable ancestor -- that possibility was already ruled
# out above. A genuine failure here (corrupt object, race on the worktree) is
# still not swallowed: capture the real exit status rather than `|| true`-ing
# it away, and fail loud rather than treat an empty result as ambiguous with
# "the command errored".
diff_err="$(mktemp)"
trap 'rm -f "$diff_err"' EXIT
diff_status=0
rows="$(git -C "$repo_root" diff --name-status --find-renames "$base_commit" HEAD -- "$migrations_dir" 2>"$diff_err")" || diff_status=$?
if [ "$diff_status" -ne 0 ]; then
  printf 'verify-migration-immutability: git diff against resolved base %s (%s) failed:\n' "$base_ref" "$base_commit" >&2
  cat "$diff_err" >&2
  exit 1
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
    printf '\nA shipped migration under %s changed relative to %s (%s).\n' "$migrations_dir" "$base_ref" "$base_commit"
    printf 'Widen or fix its behavior through a NEW guarded migration file instead --\n'
    printf 'see %s/README.md and %s/checksum_alias.go for the pattern (#7002).\n' "$migrations_dir" "$migrations_dir"
  } >&2
  exit 1
fi

printf 'verify-migration-immutability: no shipped migration was modified, deleted, or renamed\n'
