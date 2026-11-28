#!/usr/bin/env bash
# verify-no-ephemeral-docs: block new ephemeral evidence/arbiter/plan files and
# prevent PR-log evidence markers from being appended to go/**/*.md.
#
# Enforces two rules on newly added files and newly added lines:
#   1. No new files under docs/internal/evidence/, docs/internal/arbiter/,
#      docs/superpowers/, docs/plans/, or matching go/**/evidence-*.md.
#   2. No newly added Performance/Benchmark/No-Regression/Observability
#      Evidence or No-Observability-Change marker lines inside go/**/*.md.
#
# Proof belongs in the GitHub PR description or CI workflow artifacts. Arbiter
# rulings belong in GitHub issue comments. Durable package invariants belong in
# go/<pkg>/README.md (without PR-log markers) or docs/public/decisions/.
#
# Runs under macOS stock bash 3.2 as well as bash 5.x.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="${ESHU_NO_EPHEMERAL_DOCS_REPO_ROOT:-$(cd "${script_dir}/.." && pwd)}"

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
      printf 'verify-no-ephemeral-docs: unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/eshu-no-ephemeral-docs.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

marker_re='(^|[[:space:]])(Performance Evidence|Benchmark Evidence|No-Regression Evidence|Observability Evidence|No-Observability-Change)([[:space:]]*(\([^()]*\)|\[[^\[\]]*\]))?[[:space:]]*:'

failures=0
fail_msg() {
  printf 'verify-no-ephemeral-docs: FAIL: %s\n' "$1" >&2
  failures=$((failures + 1))
}

is_banned_new_path() {
  local rel="$1"
  case "$rel" in
    docs/internal/evidence/*|docs/internal/arbiter/*|docs/superpowers/*|docs/plans/*)
      return 0
      ;;
    go/*evidence-*.md)
      case "$(basename "$rel")" in
        evidence-*.md) return 0 ;;
      esac
      ;;
  esac
  return 1
}

is_go_markdown() {
  local rel="$1"
  case "$rel" in
    testdata/*|*/testdata/*|vendor/*|*/vendor/*|generated/*|*/generated/*) return 1 ;;
    go/*.md) return 0 ;;
    *) return 1 ;;
  esac
}

check_banned_new_path() {
  local rel="$1"
  if is_banned_new_path "$rel"; then
    fail_msg "${rel}: new ephemeral doc path is banned; put PR proof in the GitHub PR description or CI artifacts, arbiter rulings in GitHub issue comments, and durable ADRs in docs/public/decisions/"
  fi
}

check_file_added_markers() {
  local rel="$1"
  local abs="${repo_root}/${rel}"
  [ -f "$abs" ] || return 0
  is_go_markdown "$rel" || return 0

  local scan_file="${tmp_dir}/added-markers.txt"
  if [ -n "$base" ] && git -C "$repo_root" cat-file -e "${base}:${rel}" 2>/dev/null; then
    git -C "$repo_root" diff --unified=0 "$base" -- "$rel" \
      | awk 'substr($0, 1, 1) == "+" && substr($0, 1, 3) != "+++" { print substr($0, 2) }' >"$scan_file"
  else
    cp "$abs" "$scan_file"
  fi
  [ -s "$scan_file" ] || return 0

  local rg_rc=0
  rg --no-config -q -e "$marker_re" "$scan_file" || rg_rc=$?
  if [ "$rg_rc" -eq 0 ]; then
    fail_msg "${rel}: added PR evidence marker in go/**/*.md; put Performance/Observability Evidence markers in the GitHub PR body (verified by scripts/verify-performance-evidence.sh --pr-body) instead of package docs"
  elif [ "$rg_rc" -gt 1 ]; then
    printf 'verify-no-ephemeral-docs: rg failed scanning %s (exit %s)\n' "$rel" "$rg_rc" >&2
    exit 2
  fi
}

# Resolve base for diff-scoped checks.
base=""
if [ "$mode" = "files" ] && [ -z "${ESHU_NO_EPHEMERAL_DOCS_BASE:-}" ]; then
  if git -C "$repo_root" rev-parse --verify origin/main >/dev/null 2>&1; then
    base="$(git -C "$repo_root" merge-base origin/main HEAD 2>/dev/null || true)"
  fi
  if [ -z "$base" ] && git -C "$repo_root" rev-parse --verify HEAD >/dev/null 2>&1; then
    base="HEAD"
  fi
else
  base_override="${ESHU_NO_EPHEMERAL_DOCS_BASE:-$(eshu_gate_merge_ref_parent "$repo_root")}"
  eshu_gate_resolve_diff_base "verify-no-ephemeral-docs" "$repo_root" "$base_override"
  base="$eshu_gate_diff_base"
fi

if [ -n "$base" ] && ! git -C "$repo_root" cat-file -e "${base}^{commit}" 2>/dev/null; then
  printf 'verify-no-ephemeral-docs: base commit %s is not present in %s\n' "$base" "$repo_root" >&2
  exit 2
fi

if [ "$mode" = "files" ]; then
  if [ "${#explicit_files[@]}" -gt 0 ]; then
    for rel in "${explicit_files[@]}"; do
      [ -e "${repo_root}/${rel}" ] || continue
      if [ -z "$base" ] || ! git -C "$repo_root" cat-file -e "${base}:${rel}" 2>/dev/null; then
        check_banned_new_path "$rel"
      fi
      check_file_added_markers "$rel"
    done
  fi
else
  if [ -n "$base" ]; then
    if git -C "$repo_root" merge-base "$base" HEAD >/dev/null 2>&1; then
      diff_range="${base}...HEAD"
    else
      diff_range="${base}..HEAD"
    fi

    added_names="${tmp_dir}/added-names.txt"
    git -C "$repo_root" diff --name-only --diff-filter=A "$diff_range" >"$added_names"
    while IFS= read -r rel; do
      [ -n "$rel" ] || continue
      check_banned_new_path "$rel"
    done <"$added_names"

    added_go_md="${tmp_dir}/added-go-md.txt"
    git -C "$repo_root" diff --unified=0 "$diff_range" -- 'go/*.md' 'go/**/*.md' \
      | awk '
          substr($0, 1, 6) == "+++ b/" { cur = substr($0, 7); next }
          $0 == "+++ /dev/null" { cur = ""; next }
          substr($0, 1, 1) == "+" && substr($0, 1, 3) != "+++" {
            if (cur != "") print cur "\t" substr($0, 2)
          }
        ' >"$added_go_md"
    if [ -s "$added_go_md" ]; then
      hits_file="${tmp_dir}/marker-hits.txt"
      rg_rc=0
      rg --no-config --no-filename --no-line-number -e "$marker_re" "$added_go_md" >"$hits_file" || rg_rc=$?
      if [ "$rg_rc" -gt 1 ]; then
        printf 'verify-no-ephemeral-docs: rg failed scanning added go/*.md lines (exit %s)\n' "$rg_rc" >&2
        exit 2
      fi
      if [ -s "$hits_file" ]; then
        hit_names="${tmp_dir}/hit-names.txt"
        awk -F'\t' '{ print $1 }' "$hits_file" | sort -u >"$hit_names"
        while IFS= read -r hit_file; do
          [ -n "$hit_file" ] || continue
          is_go_markdown "$hit_file" || continue
          fail_msg "${hit_file}: added PR evidence marker in go/**/*.md; put Performance/Observability Evidence markers in the GitHub PR body (verified by scripts/verify-performance-evidence.sh --pr-body) instead of package docs"
        done <"$hit_names"
      fi
    fi
  fi
fi

if [ "$failures" -gt 0 ]; then
  printf 'verify-no-ephemeral-docs: %d violation(s)\n' "$failures" >&2
  exit 1
fi

printf 'verify-no-ephemeral-docs: no new ephemeral docs or go/**/*.md PR evidence markers\n'
