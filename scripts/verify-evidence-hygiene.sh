#!/usr/bin/env bash
# verify-evidence-hygiene: keep docs/internal/evidence/ human- and agent-readable.
#
# Enforces four rules:
#   1. Markdown only under docs/internal/evidence/ — no raw .txt/.sql/.tsv/.log
#      dumps and no *-raw/ directories anywhere in the tree.
#   2. No links or references to untracked local state (.claude/, .codex/,
#      /Users/, /tmp/) in added lines under docs/internal/evidence/**/*.md.
#   3. Newly added notes under docs/internal/evidence/ (other than README.md)
#      must stay at or below 150 lines.
#   4. Newly added notes under docs/internal/evidence/ (other than README.md)
#      must include a `## Summary` heading so readers get the point first.
#
# Runs under macOS stock bash 3.2 as well as bash 5.x (no associative arrays,
# no large heredocs/here-strings).
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="${ESHU_EVIDENCE_HYGIENE_REPO_ROOT:-$(cd "${script_dir}/.." && pwd)}"
max_new_lines="${ESHU_EVIDENCE_HYGIENE_MAX_LINES:-150}"

# shellcheck source=scripts/lib/gate-diff-base.sh
. "${script_dir}/lib/gate-diff-base.sh"

usage() {
  printf 'usage: %s [--files <path...>] [--help]\n' "${0##*/}"
}

mode="repo"
explicit_files=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --files)
      mode="files"
      shift
      while [ "$#" -gt 0 ]; do
        explicit_files+=("$1")
        shift
      done
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      printf 'verify-evidence-hygiene: unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/eshu-evidence-hygiene.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

evidence_prefix="docs/internal/evidence/"
local_state_re='(\.claude/|\.codex/|/Users/|/tmp/)'

failures=0
fail_msg() {
  printf 'verify-evidence-hygiene: FAIL: %s\n' "$1" >&2
  failures=$((failures + 1))
}

check_raw_path() {
  local rel="$1"
  case "$rel" in
    "${evidence_prefix}"*) ;;
    *) return 0 ;;
  esac
  case "$rel" in
    *-raw/*)
      fail_msg "${rel}: raw dump directories (*-raw/) are forbidden under docs/internal/evidence/; keep raw outputs in local scratch or CI workflow artifacts"
      return 0
      ;;
  esac
  case "$rel" in
    *.md) ;;
    *)
      fail_msg "${rel}: non-Markdown file under docs/internal/evidence/; commit only concise .md summaries and keep raw outputs out of git"
      ;;
  esac
}

check_whole_tree_raw_paths() {
  local list_file="${tmp_dir}/tree-files.txt"
  git -C "$repo_root" ls-files --cached --others --exclude-standard -- docs/internal/evidence >"$list_file"
  while IFS= read -r rel; do
    [ -n "$rel" ] || continue
    [ -e "${repo_root}/${rel}" ] || continue
    check_raw_path "$rel"
  done <"$list_file"
}

check_new_note_shape() {
  local rel="$1"
  local abs="${repo_root}/${rel}"
  [ -f "$abs" ] || return 0
  [ "$rel" = "${evidence_prefix}README.md" ] && return 0

  local lines
  lines="$(awk 'END { print NR + 0 }' "$abs")"
  if [ "$lines" -gt "$max_new_lines" ]; then
    fail_msg "${rel}: new evidence note is ${lines} lines (limit ${max_new_lines}); summarize the findings and move durable invariants to the owning package doc or ADR"
  fi
  local rg_rc=0
  rg --no-config -q '^## Summary([[:space:]]|$)' "$abs" || rg_rc=$?
  if [ "$rg_rc" -eq 1 ]; then
    fail_msg "${rel}: new evidence note is missing a '## Summary' section (see docs/internal/evidence/README.md)"
  elif [ "$rg_rc" -gt 1 ]; then
    printf 'verify-evidence-hygiene: rg failed checking %s (exit %s)\n' "$rel" "$rg_rc" >&2
    exit 2
  fi
}

check_file_local_state_links() {
  local rel="$1"
  local abs="${repo_root}/${rel}"
  [ -f "$abs" ] || return 0
  [ "$rel" = "${evidence_prefix}README.md" ] && return 0
  if [ -n "$base" ] && git -C "$repo_root" cat-file -e "${base}:${rel}" 2>/dev/null; then
    local diff_added="${tmp_dir}/file-added.txt"
    local rg_rc=0
    git -C "$repo_root" diff --unified=0 "$base" -- "$rel" \
      | awk 'substr($0, 1, 1) == "+" && substr($0, 1, 3) != "+++" { print substr($0, 2) }' >"$diff_added"
    if [ -s "$diff_added" ]; then
      rg --no-config -q -e "$local_state_re" "$diff_added" || rg_rc=$?
      if [ "$rg_rc" -eq 0 ]; then
        fail_msg "${rel}: added line references untracked local state (.claude/, .codex/, /Users/, or /tmp/)"
      elif [ "$rg_rc" -gt 1 ]; then
        printf 'verify-evidence-hygiene: rg failed scanning %s (exit %s)\n' "$rel" "$rg_rc" >&2
        exit 2
      fi
    fi
    return 0
  fi
  local rg_rc=0
  rg --no-config -q -e "$local_state_re" "$abs" || rg_rc=$?
  if [ "$rg_rc" -eq 0 ]; then
    fail_msg "${rel}: references untracked local state (.claude/, .codex/, /Users/, or /tmp/)"
  elif [ "$rg_rc" -gt 1 ]; then
    printf 'verify-evidence-hygiene: rg failed scanning %s (exit %s)\n' "$rel" "$rg_rc" >&2
    exit 2
  fi
}

# Resolve base for diff-scoped checks.
# In --files (pre-commit) mode with no explicit override, compare staged edits
# against origin/main's merge base, or HEAD itself when on a fresh branch at
# origin/main (never falling back to HEAD~1, which would misclassify files
# already committed on origin/main's tip as newly added).
base=""
if [ "$mode" = "files" ] && [ -z "${ESHU_EVIDENCE_HYGIENE_BASE:-}" ]; then
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || true)"
  fi
  if [ -z "$base" ] && git -C "$repo_root" rev-parse --verify HEAD >/dev/null 2>&1; then
    base="HEAD"
  fi
else
  base_override="${ESHU_EVIDENCE_HYGIENE_BASE:-$(eshu_gate_merge_ref_parent "$repo_root")}"
  eshu_gate_resolve_diff_base "verify-evidence-hygiene" "$repo_root" "$base_override"
  base="$eshu_gate_diff_base"
fi

if [ -n "$base" ] && ! git -C "$repo_root" cat-file -e "${base}^{commit}" 2>/dev/null; then
  printf 'verify-evidence-hygiene: base commit %s is not present in %s\n' "$base" "$repo_root" >&2
  exit 2
fi

if [ "$mode" = "files" ]; then
  run_whole_tree=0
  if [ "${#explicit_files[@]}" -gt 0 ]; then
    for rel in "${explicit_files[@]}"; do
      case "$rel" in
        scripts/verify-evidence-hygiene.sh|scripts/test-verify-evidence-hygiene.sh)
          run_whole_tree=1
          continue
          ;;
        "${evidence_prefix}"*) ;;
        *) continue ;;
      esac
      [ -e "${repo_root}/${rel}" ] || continue
      check_raw_path "$rel"
      case "$rel" in
        *.md)
          check_file_local_state_links "$rel"
          if [ -z "$base" ] || ! git -C "$repo_root" cat-file -e "${base}:${rel}" 2>/dev/null; then
            check_new_note_shape "$rel"
          fi
          ;;
      esac
    done
  fi
  if [ "$run_whole_tree" -eq 1 ]; then
    check_whole_tree_raw_paths
  fi
else
  # Whole-tree check: no non-Markdown files or *-raw/ directories anywhere under docs/internal/evidence/.
  check_whole_tree_raw_paths

  # Diff-scoped checks on added/modified Markdown files under docs/internal/evidence/.
  if [ -n "$base" ]; then
    if git -C "$repo_root" merge-base "$base" HEAD >/dev/null 2>&1; then
      diff_range="${base}...HEAD"
    else
      diff_range="${base}..HEAD"
    fi

    # Newly added .md files must meet the size cap and ## Summary contract.
    added_names="${tmp_dir}/added-names.txt"
    git -C "$repo_root" diff --name-only --diff-filter=A "$diff_range" -- docs/internal/evidence >"$added_names"
    while IFS= read -r rel; do
      [ -n "$rel" ] || continue
      case "$rel" in
        "${evidence_prefix}"*.md) check_new_note_shape "$rel" ;;
      esac
    done <"$added_names"

    # Added lines in any added/modified .md note (other than README.md) must not
    # reference untracked local agent state.
    added_file="${tmp_dir}/added-lines.txt"
    git -C "$repo_root" diff --unified=0 "$diff_range" -- docs/internal/evidence \
      | awk '
          substr($0, 1, 6) == "+++ b/" { cur = substr($0, 7); next }
          $0 == "+++ /dev/null" { cur = ""; next }
          substr($0, 1, 1) == "+" && substr($0, 1, 3) != "+++" {
            if (cur != "" && cur != "docs/internal/evidence/README.md" && substr(cur, length(cur)-2) == ".md") {
              print cur "\t" substr($0, 2)
            }
          }
        ' >"$added_file"
    if [ -s "$added_file" ]; then
      matches_file="${tmp_dir}/added-hits.txt"
      rg_rc=0
      rg --no-config --no-filename --no-line-number -e "$local_state_re" "$added_file" >"$matches_file" || rg_rc=$?
      if [ "$rg_rc" -gt 1 ]; then
        printf 'verify-evidence-hygiene: rg failed scanning added lines (exit %s)\n' "$rg_rc" >&2
        exit 2
      fi
      if [ -s "$matches_file" ]; then
        hit_names="${tmp_dir}/hit-names.txt"
        awk -F'\t' '{ print $1 }' "$matches_file" | sort -u >"$hit_names"
        while IFS= read -r hit_file; do
          [ -n "$hit_file" ] || continue
          fail_msg "${hit_file}: added line references untracked local state (.claude/, .codex/, /Users/, or /tmp/)"
        done <"$hit_names"
      fi
    fi
  fi
fi

if [ "$failures" -gt 0 ]; then
  printf 'verify-evidence-hygiene: %d violation(s); see docs/internal/evidence/README.md\n' "$failures" >&2
  exit 1
fi

printf 'verify-evidence-hygiene: docs/internal/evidence/ is clean\n'
