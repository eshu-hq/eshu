#!/usr/bin/env bash
#
# verify-no-private-identifiers.sh — fail if an environment or organization
# identifier appears in a commit message or in added diff content.
#
# Repo rule: no environment name, employer environment prefix, organization
# name, or real hashed repository id in public artifacts (issue #7803). The
# patterns live in scripts/lib/private-identifier-pattern.sh, shared with
# scripts/dev/pre-enqueue-check.sh and the eshu-publish check-shape.sh, so a
# commit message, a diff line, a PR body, and an issue draft are held to one
# definition.
#
# Only ADDED lines are scanned, so the files that already carry a token need no
# exception: removing a token passes, and so does leaving it untouched. There is
# no path allowlist. The gate excludes only its own implementation files.
#
# Modes (same shape as verify-no-ai-attribution.sh):
#   (default)         range mode — scan commit messages and added diff lines in
#                     <base>..HEAD. base = ESHU_PRIVATE_IDENTIFIER_BASE, else
#                     origin/$GITHUB_BASE_REF, else merge-base origin/main HEAD.
#   --staged          scan staged (git diff --cached) added lines.
#   --message <file>  scan a commit-message file.
#
# Private mode: when ESHU_PRIVATE_IDENTIFIER_FILE names a readable file OUTSIDE
# the repository (one regex per line; blank lines and '#' lines are ignored),
# the same scans also apply those patterns. Real hashed ids cannot be listed in
# a public repository, so the owner keeps them there. Matches are reported as
# file:line only, never echoed. When the variable is unset the gate prints
# "private denylist: not configured" and continues; CI never sets it. A set but
# unusable file (missing, empty, inside the repo, invalid regex) fails the gate.
#
# Output never echoes a match. A hit in added content is reported as path:line
# and a hit in a commit message as "commit-message line N", so the failure log
# of a public CI run, a PR check and a pasted terminal does not repeat the
# identifier it refuses.
#
# Exit 0 when clean; non-zero listing each offending location.
set -euo pipefail

repo_root="${ESHU_PRIVATE_IDENTIFIER_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
fi

# shellcheck source=scripts/lib/private-identifier-pattern.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/private-identifier-pattern.sh"

# The gate's own implementation necessarily names these classes. Exclude it from
# content scans so the gate never flags itself. Nothing else is excluded.
self_excludes=(
  ':(exclude)scripts/verify-no-private-identifiers.sh'
  ':(exclude)scripts/lib/private-identifier-pattern.sh'
  ':(exclude)scripts/lib/test-private-identifier-cases.sh'
)

fail=0
scan_out=""
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

report() {
  printf 'verify-no-private-identifiers: %s\n' "$1" >&2
}

# scan_text <rg args...>: run rg and leave its matches in $scan_out. rg exits 1
# for "no match" and 2 or more for an error; an error is a failure, never a
# clean scan.
scan_text() {
  local rc=0
  scan_out="$(rg "$@")" || rc=$?
  if [ "$rc" -gt 1 ]; then
    report "ripgrep failed (exit $rc); content was not verified"
    fail=1
    scan_out=""
  fi
}

# added_lines <git diff args...>: write each added line to two parallel files,
# its location (path:line) to $loc_file and its content, with nothing in front
# of it, to $content_file. Matching runs on the content alone, so a pattern
# that anchors on the start of the line (a name at column 0, a "^" denylist
# entry) sees the real start of the line. The awk state machine reads the diff
# headers, so a content line that itself begins with "++" is never mistaken for
# a file header.
loc_file="$work/added.loc"
content_file="$work/added.content"
added_lines() {
  local diff_text
  : >"$loc_file"
  : >"$content_file"
  if ! diff_text="$(git -C "$repo_root" diff -U0 -M --no-color --no-ext-diff "$@" -- . "${self_excludes[@]}")"; then
    report "git diff $* failed; content was not verified"
    return 1
  fi
  LC_ALL=C awk -v loc="$loc_file" -v con="$content_file" '
    /^diff --git / { hdr = 1; next }
    hdr && /^\+\+\+ / { f = $0; sub(/^\+\+\+ /, "", f); sub(/^b\//, "", f); sub(/\t.*$/, "", f); next }
    /^@@ / { hdr = 0; s = $0; sub(/^@@ -[0-9]+(,[0-9]+)? \+/, "", s); sub(/[^0-9].*$/, "", s); ln = s + 0; next }
    !hdr && /^\+/ { print f ":" ln > loc; print substr($0, 2) > con; ln++ }
  ' <<<"$diff_text"
}

# report_locations <label>: print the path:line of every content line that
# $scan_out (rg -n output over $content_file) matched. Only the line numbers of
# the match output are used, so the matched text is never printed.
report_locations() {
  report "$1 (location only):"
  cut -d: -f1 <<<"$scan_out" | awk 'NR == FNR { want[$1] = 1; next } FNR in want' - "$loc_file" >&2
  fail=1
}

# --- private denylist -------------------------------------------------------
private_count=0
private_plain="$work/private.plain"
private_file="${ESHU_PRIVATE_IDENTIFIER_FILE:-}"
if [ -z "$private_file" ]; then
  printf 'verify-no-private-identifiers: private denylist: not configured\n'
else
  private_ok=1
  if [ ! -f "$private_file" ] || [ ! -r "$private_file" ]; then
    report "private denylist: ESHU_PRIVATE_IDENTIFIER_FILE is not a readable file"
    private_ok=0
  else
    private_dir="$(cd "$(dirname "$private_file")" && pwd -P)"
    root_phys="$(cd "$repo_root" && pwd -P)"
    case "$private_dir/" in
      "$root_phys"/*)
        report "private denylist: the file must live outside the repository"
        private_ok=0
        ;;
    esac
  fi
  if [ "$private_ok" -eq 1 ]; then
    # Drop blanks and comments: an empty regex line would match every line.
    rg -v '^[[:space:]]*(#|$)' "$private_file" >"$private_plain" || true
    private_count="$(wc -l <"$private_plain" | tr -d '[:space:]')"
    if [ "$private_count" -eq 0 ]; then
      report "private denylist: the file holds no patterns"
      private_ok=0
    else
      rc=0
      printf '\n' | rg -q -f "$private_plain" >/dev/null 2>&1 || rc=$?
      if [ "$rc" -gt 1 ]; then
        report "private denylist: the file holds an invalid regex"
        private_ok=0
      fi
    fi
  fi
  if [ "$private_ok" -eq 0 ]; then
    fail=1
    private_count=0
  else
    printf 'verify-no-private-identifiers: private denylist: %s pattern(s) applied\n' "$private_count"
  fi
fi

# scan_private_diff <label> <git diff args...>
scan_private_diff() {
  local label="$1"
  shift
  [ "$private_count" -gt 0 ] || return 0
  if ! added_lines "$@"; then
    fail=1
    return 0
  fi
  scan_text -n -f "$private_plain" "$content_file"
  if [ -n "$scan_out" ]; then
    report_locations "private denylist match in $label"
  fi
}

# scan_public_diff <label> <git diff args...>
scan_public_diff() {
  local label="$1"
  shift
  if ! added_lines "$@"; then
    fail=1
    return 0
  fi
  scan_text -n -e "$PRIVATE_IDENTIFIER_PATTERN" "$content_file"
  if [ -n "$scan_out" ]; then
    report_locations "environment or organization identifier in $label"
  fi
}

# scan_message <label> <text>: scan one commit message body.
scan_message() {
  local label="$1" text="$2"
  scan_text -n -e "$PRIVATE_IDENTIFIER_PATTERN" <<<"$text"
  if [ -n "$scan_out" ]; then
    report "environment or organization identifier in $label (location only):"
    cut -d: -f1 <<<"$scan_out" | sed 's/^/commit-message line /' >&2
    fail=1
  fi
  if [ "$private_count" -gt 0 ]; then
    scan_text -n -f "$private_plain" <<<"$text"
    if [ -n "$scan_out" ]; then
      report "private denylist match in $label (location only):"
      cut -d: -f1 <<<"$scan_out" | sed 's/^/commit-message line /' >&2
      fail=1
    fi
  fi
}

mode="${1:-range}"
case "$mode" in
  --staged)
    scan_public_diff "staged content" --cached
    scan_private_diff "staged content" --cached
    ;;
  --message)
    msg_file="${2:-}"
    if [ -n "$msg_file" ] && [ -f "$msg_file" ]; then
      # Git's own status comments ("# renamed: <path>") land in the file an
      # editor commit hands the commit-msg hook and are stripped from the
      # stored message. Scanning them would block the rename that removes a
      # token from a file name, so lines that start with # are not content.
      # They are blanked, not deleted, so a reported line number still counts
      # the file's own lines.
      msg_text="$(sed 's/^#.*$//' "$msg_file")"
      scan_message "the commit message" "$msg_text"
    fi
    ;;
  *)
    base_ref="${ESHU_PRIVATE_IDENTIFIER_BASE:-}"
    if [ -z "$base_ref" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
      # Use the base ref the CI checkout already fetched (fetch-depth: 0). Only
      # fetch if missing, and NEVER with --depth=1: a shallow base severs
      # ancestry, so <base>..HEAD balloons to nearly all history.
      if ! git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
        git -C "$repo_root" fetch --no-tags origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
      fi
      if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
        base_ref="origin/$GITHUB_BASE_REF"
      fi
    fi
    if [ -z "$base_ref" ]; then
      if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
        base_ref="origin/main"
      else
        printf 'verify-no-private-identifiers: no base commit available, skipping\n'
        exit 0
      fi
    fi
    # Scope to the commits THIS branch adds since it diverged from the base:
    # the merge-base, never historical commits already on the base.
    # A base that does not resolve, or shares no ancestor with HEAD, cannot
    # be scanned: fail rather than report a clean range that was never read.
    if ! base="$(git -C "$repo_root" merge-base "$base_ref" HEAD)"; then
      report "git merge-base $base_ref HEAD failed; the range was not verified"
      exit 1
    fi

    if ! commits="$(git -C "$repo_root" rev-list "$base..HEAD")"; then
      report "git rev-list $base..HEAD failed; commit messages were not verified"
      exit 1
    fi
    while IFS= read -r commit; do
      [ -n "$commit" ] || continue
      if ! commit_msg="$(git -C "$repo_root" log -1 --format=%B "$commit")"; then
        report "git log -1 $commit failed; the commit message was not verified"
        fail=1
        continue
      fi
      scan_message "commit ${commit:0:12}" "$commit_msg"
    done <<<"$commits"

    scan_public_diff "added content in $base...HEAD" "$base...HEAD"
    scan_private_diff "added content in $base...HEAD" "$base...HEAD"
    ;;
esac

if [ "$fail" -ne 0 ]; then
  printf '\nFix: remove or rephrase the identifier ("the QA environment",\n' >&2
  printf '"the production environment", "repo-X" for a repository id). History\n' >&2
  printf 'is not rewritten; only added lines and new commit messages are checked.\n' >&2
  exit 1
fi

printf 'verify-no-private-identifiers: no environment or organization identifiers found.\n'
