#!/usr/bin/env bash
#
# verify-moved-file-refs.sh - fail a branch that moves or deletes a Go file and
# leaves a reference pointing at the path it vacated (#6525).
#
# Why this exists: the #6061 reducer subpackage split moved family after family
# out of the flat `go/internal/reducer/` root. Each move left behind pointers to
# the paths it vacated -- in design docs, evidence notes, scoped AGENTS.md
# files, package READMEs, SQL migration comments and Go doc comments -- and no
# gate objected. `scripts/verify-doc-citations.sh` is the closest existing gate,
# but it only tracks a citation carrying a `:NNN` line suffix; a BARE path
# reference carries no line number, so it was never tracked at all. That is
# deliberate there (a path-only reference survives line drift, which is why the
# epic prefers writing them that way) and the cost is precisely this class: 53
# dead reducer paths accumulated across docs/, scripts/, specs/ and go/ before
# anyone counted them.
#
# What it checks, and why it is scoped to the branch:
#
# The check is NOT "no dead path references exist anywhere". That would fire on
# inherited debt and on a long tail of DELIBERATE negative fixtures -- paths
# that are supposed not to exist, embedded in tests asserting that a checker
# rejects a missing file (`does_not_exist.go`, `no_such_handler_file.go`, the
# telemetry-coverage ghost rows, the cigates selector's synthetic `r.go`).
# Failing on those would be wrong and would make the gate unfixable.
#
# It is "this branch removed a path that something still points at". The
# attribution is what makes it precise: a path is reported only when it existed
# at the diff base and does not exist in the working tree. A deliberate fixture
# never existed at the base, so it can never trip this gate; inherited debt did
# not change, so it cannot either. Only a pointer the branch itself broke is
# reported -- exactly the failure the #6061 moves kept shipping.
#
# Working from the branch's deleted/renamed set (rather than scanning every
# reference in the tree and testing each for existence) is also what keeps this
# cheap: a branch that moves nothing greps for nothing and exits immediately.
# For a rename, git's own rename detection supplies the new path, so the failure
# message names the repoint target instead of leaving the author to find it.
#
# Intentional exceptions go in scripts/moved-file-refs-allowlist.txt -- a
# historical command transcript or a dated evidence note may legitimately record
# a path as it stood at the time, where repointing would falsify the record.
#
# Exit 0 when clean, 1 listing each dangling reference, 2 on a usage or
# operational error (so a scan that could not run is never a silent pass).
#
# Runs under macOS's stock /bin/bash 3.2 as well as Homebrew bash >= 5.1: no
# `declare -A`, and no here-string carrying a body of consequence.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="${ESHU_MOVED_FILE_REFS_REPO_ROOT:-$(cd "${script_dir}/.." && pwd)}"
allowlist_path="${ESHU_MOVED_FILE_REFS_ALLOWLIST:-${repo_root}/scripts/moved-file-refs-allowlist.txt}"

usage() {
  printf 'usage: %s [--base <ref>] [--help]\n' "${0##*/}"
  printf '  Fails when this branch moved or deleted a Go file under go/ and a\n'
  printf '  reference to the vacated path is still present in the working tree.\n'
  printf '  --base <ref>  diff base to attribute against (default: merge-base\n'
  printf '                with origin/main; see scripts/lib/gate-diff-base.sh)\n'
}

base_override="${ESHU_MOVED_FILE_REFS_BASE:-}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --help | -h)
      usage
      exit 0
      ;;
    --base)
      shift
      [ "$#" -gt 0 ] || {
        printf 'verify-moved-file-refs: --base requires a ref\n' >&2
        exit 2
      }
      base_override="$1"
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
  shift
done

command -v git >/dev/null 2>&1 || {
  printf 'verify-moved-file-refs: missing required tool: git\n' >&2
  exit 2
}
cd "${repo_root}"

# shellcheck source=scripts/lib/gate-diff-base.sh
. "${script_dir}/lib/gate-diff-base.sh"
eshu_gate_resolve_diff_base "verify-moved-file-refs" "${repo_root}" "${base_override}"
base="${eshu_gate_diff_base}"
if [ -z "${base}" ]; then
  # The helper already printed the operator-facing skip line.
  exit 0
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
tab="$(printf '\t')"

# Old path -> new path for every Go file under go/ the branch renamed, and old
# path -> "" for every one it deleted outright. -M turns a move into a single R
# record carrying both sides, which is what lets a failure name its target.
if ! git diff --name-status -M --diff-filter=DR "${base}" HEAD \
  >"${tmp_dir}/changed.txt" 2>"${tmp_dir}/diff-err.txt"; then
  printf 'verify-moved-file-refs: git diff against %s failed; the scan did not run\n' "${base}" >&2
  cat "${tmp_dir}/diff-err.txt" >&2
  exit 2
fi

: >"${tmp_dir}/vacated.txt"
while IFS="${tab}" read -r status old new; do
  [ -n "${old:-}" ] || continue
  case "${old}" in
    go/*.go) ;;
    *) continue ;;
  esac
  case "${status}" in
    R*) ;;
    D) new="" ;;
    *) continue ;;
  esac
  # A path re-created by the same branch (split in place, or moved back) is not
  # vacated: the reference still resolves, so there is nothing to report.
  [ -e "${old}" ] && continue
  printf '%s\t%s\n' "${old}" "${new:-}" >>"${tmp_dir}/vacated.txt"
done <"${tmp_dir}/changed.txt"

vacated_n="$(awk 'NF' "${tmp_dir}/vacated.txt" | wc -l | tr -d ' ')"
if [ "${vacated_n}" -eq 0 ]; then
  printf 'verify-moved-file-refs: branch vacates no go/**.go path (base %s); nothing to check\n' "${base}"
  exit 0
fi

# The gate and its own self-test necessarily contain the paths they demonstrate,
# and the allowlist necessarily names the paths it exempts.
exclusions=(
  ':(exclude)scripts/verify-moved-file-refs.sh'
  ':(exclude)scripts/test-verify-moved-file-refs.sh'
  ':(exclude)scripts/moved-file-refs-allowlist.txt'
)

# allowed reports whether "<referencing-file>:<vacated-path>" is exempted.
allowed() {
  [ -f "${allowlist_path}" ] || return 1
  awk -v want="$1" '
    /^[[:space:]]*#/ { next }
    { gsub(/^[[:space:]]+|[[:space:]]+$/, "") }
    NF && $0 == want { found = 1; exit }
    END { exit(found ? 0 : 1) }
  ' "${allowlist_path}"
}

violations=0
while IFS="${tab}" read -r old new; do
  [ -n "${old}" ] || continue
  # git grep exits 0 with matches, 1 with none, and >1 when the search itself
  # could not run. Only 1 means clean -- treating every non-zero status as
  # "clean" would turn an operational failure into a silent pass.
  set +e
  hits="$(git grep -n -F -- "${old}" -- . "${exclusions[@]}" 2>"${tmp_dir}/grep-err.txt")"
  grep_status=$?
  set -e
  case "${grep_status}" in
    0) ;;
    1) continue ;;
    *)
      printf 'verify-moved-file-refs: git grep for %s failed (exit %s); the scan did not run\n' \
        "${old}" "${grep_status}" >&2
      cat "${tmp_dir}/grep-err.txt" >&2
      exit 2
      ;;
  esac
  printf '%s\n' "${hits}" >"${tmp_dir}/hits.txt"
  while IFS= read -r hit; do
    [ -n "${hit}" ] || continue
    hit_file="${hit%%:*}"
    if allowed "${hit_file}:${old}"; then
      continue
    fi
    if [ "${violations}" -eq 0 ]; then
      printf 'verify-moved-file-refs: this branch vacated these paths but left references to them:\n\n' >&2
    fi
    printf '  %s\n' "${hit}" >&2
    if [ -n "${new}" ]; then
      printf '      -> repoint to %s\n' "${new}" >&2
    else
      printf '      -> %s was deleted by this branch; drop or rewrite the reference\n' "${old}" >&2
    fi
    violations=$((violations + 1))
  done <"${tmp_dir}/hits.txt"
done <"${tmp_dir}/vacated.txt"

if [ "${violations}" -gt 0 ]; then
  # One printf per fragment, NOT a heredoc. This body is 713 bytes, and
  # references/shell-portability.md records that bash >= 5.1 writes an entire
  # heredoc body to its reader before that reader runs, so a body strictly
  # between the 512-byte pipe buffer and 64 KB DEADLOCKS. macOS bash 3.2 never
  # had that writer change, which is why this survived local runs. A hang here
  # would strike exactly when the gate found a violation and tried to explain
  # it. printf is a builtin: no pipe, no fork, no deadlock. cmd/heredoc-budget
  # is the blocking gate that enforces this. Wording below is split, not
  # trimmed.
  printf '\n' >&2
  printf 'Each reference above names a file this branch moved or deleted, so it now\n' >&2
  printf 'resolves to nothing. Repoint it to the path shown, or drop it.\n' >&2
  printf '\n' >&2
  printf 'Write the repoint WITHOUT a `:NNN` line suffix. scripts/verify-doc-citations.sh\n' >&2
  printf 'refuses branch-authored LINE occurrences ("LINE debt may only decrease"), so a\n' >&2
  printf 'repoint that keeps a line number is rejected outright; a path-only reference is\n' >&2
  printf 'accepted and survives later line drift.\n' >&2
  printf '\n' >&2
  printf 'If a reference is deliberately historical -- a command transcript or a dated\n' >&2
  printf 'evidence note recording the path as it stood at the time, where repointing would\n' >&2
  printf 'falsify the record -- add a "<referencing-file>:<vacated-path>" line to\n' >&2
  printf 'scripts/moved-file-refs-allowlist.txt with a comment saying why.\n' >&2
  exit 1
fi

printf 'verify-moved-file-refs: %s vacated go path(s) against base %s, no dangling references\n' \
  "${vacated_n}" "${base}"
