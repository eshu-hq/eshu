#!/usr/bin/env bash
#
# test-verify-moved-file-refs-harness.sh - shared fixture harness for the
# moved-file-refs gate mirrors.
#
# The mirror split by responsibility (scripts-file-cap): this file holds the
# throwaway-repo builders and the check helpers, while
# test-verify-moved-file-refs.sh carries the Go-path cases and
# test-verify-moved-file-refs-md.sh carries the Markdown-path cases (#11532).
# Entry files source this harness, run their cases, then call mirror_finish
# once with their suite name.
#
# Runs without Postgres, NornicDB, or a Go build.
#
# These fixture repositories are deleted immediately after each case. Do not
# hand them to ambient asynchronous Trace2 consumers that can still write while
# cleanup removes the repository.
export GIT_TRACE2_EVENT=0

harness_dir="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
repo_scripts="$(cd "${harness_dir}/.." && pwd)"
gate="${repo_scripts}/verify-moved-file-refs.sh"
helper="${repo_scripts}/lib/gate-diff-base.sh"
failures=0

check() {
  # $1 = case name, $2 = expected exit, $3 = actual exit
  if [ "$2" != "$3" ]; then
    printf 'FAIL %s: exit=%s want=%s\n' "$1" "$3" "$2" >&2
    failures=$((failures + 1))
  else
    printf 'ok   %s\n' "$1"
  fi
}

check_output() {
  # $1 = case name, $2 = expected substring, $3 = actual output
  case "$3" in
    *"$2"*) printf 'ok   %s\n' "$1" ;;
    *)
      printf 'FAIL %s: output did not contain %s\n' "$1" "$2" >&2
      printf '     got: %s\n' "$3" >&2
      failures=$((failures + 1))
      ;;
  esac
}

# new_repo builds a repo whose FIRST commit is the base: a Go file under go/
# plus a doc referencing it. The caller then makes the branch commit.
new_repo() {
  local dir
  dir="$(mktemp -d)"
  git -C "$dir" init -q
  git -C "$dir" config user.email test@example.test
  git -C "$dir" config user.name "Test"
  mkdir -p "$dir/scripts/lib" "$dir/go/internal/reducer" "$dir/docs"
  cp "$gate" "$dir/scripts/verify-moved-file-refs.sh"
  cp "$helper" "$dir/scripts/lib/gate-diff-base.sh"
  printf 'package reducer\n' >"$dir/go/internal/reducer/widget.go"
  printf 'See go/internal/reducer/widget.go for the widget family.\n' >"$dir/docs/design.md"
  git -C "$dir" add -A
  git -C "$dir" commit -qm base
  printf '%s\n' "$dir"
}

run_gate() {
  # Echoes output; returns the gate's exit code in $rc (set by the caller).
  (cd "$1" && ./scripts/verify-moved-file-refs.sh 2>&1)
}

run_gate_env() {
  # $1 = repo; the rest are NAME=value assignments for the gate's environment.
  # The ambient CI variables are cleared so no case inherits them.
  local dir="$1"
  shift
  (cd "$dir" && env -u ESHU_MOVED_FILE_REFS_BASE -u GITHUB_EVENT_NAME \
    -u GITHUB_BASE_REF "$@" ./scripts/verify-moved-file-refs.sh 2>&1)
}

# new_merge_ref_repo builds a repo whose HEAD is a pull-request merge commit.
# Parent 1 is main as it stood when the ref was built; it already cites
# go/internal/reducer/future.go, a path that does not exist yet (the planned
# reference the header calls inherited debt). Parent 2 is the PR head: a move of
# widget.go when $1 is "move", an unrelated doc otherwise. Main then moves on
# and ADDS future.go, so origin/main holds a Go file the merge ref lacks, which
# a tip-versus-HEAD diff reads as a deletion of a referenced file.
new_merge_ref_repo() {
  local kind="$1" dir base
  dir="$(new_repo)"
  git -C "$dir" branch -M main
  printf 'Planned: go/internal/reducer/future.go lands later.\n' >>"$dir/docs/design.md"
  git -C "$dir" commit -qam "main when the merge ref was built"
  base="$(git -C "$dir" rev-parse HEAD)"
  git -C "$dir" checkout -q -b pr
  if [ "$kind" = "move" ]; then
    mkdir -p "$dir/go/internal/reducer/widgetfam"
    git -C "$dir" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
  else
    printf 'unrelated\n' >"$dir/docs/notes.md"
  fi
  git -C "$dir" add -A
  git -C "$dir" commit -qm "pr head"
  git -C "$dir" checkout -q main
  printf 'package reducer\n' >"$dir/go/internal/reducer/future.go"
  git -C "$dir" add -A
  git -C "$dir" commit -qm "main moves and adds future.go"
  git -C "$dir" update-ref refs/remotes/origin/main HEAD
  git -C "$dir" checkout -q -b mergeref "$base"
  git -C "$dir" merge -q --no-ff -m "merge ref" pr
  printf '%s\n' "$dir"
}

# new_local_merge_repo builds a two-parent HEAD that is NOT a pull-request
# merge ref: a developer branch that merged main into itself. The gate must
# not treat the first parent as the base here.
new_local_merge_repo() {
  local dir
  dir="$(new_repo)"
  git -C "$dir" branch -M main
  git -C "$dir" update-ref refs/remotes/origin/main HEAD
  git -C "$dir" checkout -q -b feature
  mkdir -p "$dir/go/internal/reducer/widgetfam"
  git -C "$dir" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
  git -C "$dir" commit -qm "feature moves widget"
  git -C "$dir" checkout -q main
  printf 'unrelated\n' >"$dir/docs/notes.md"
  git -C "$dir" add -A
  git -C "$dir" commit -qm "main moves"
  git -C "$dir" update-ref refs/remotes/origin/main HEAD
  git -C "$dir" checkout -q feature
  git -C "$dir" merge -q --no-ff -m "merge main into feature" main
  printf '%s\n' "$dir"
}

# mirror_finish reports the suite result. Each entry file calls it once with
# its own suite name after its cases.
mirror_finish() {
  if [ "${failures}" -gt 0 ]; then
    printf '\n%s: %s failure(s)\n' "$1" "${failures}" >&2
    exit 1
  fi
  printf '\n%s: all cases passed\n' "$1"
}
