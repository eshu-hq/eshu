#!/usr/bin/env bash
#
# test-verify-moved-file-refs.sh - mirror test for verify-moved-file-refs.sh.
#
# Builds throwaway git repos that reproduce the #6061 shape the gate exists to
# catch (a branch moves a Go file and leaves a pointer to the path it vacated)
# and, just as importantly, the shapes it must NOT catch: a reference that was
# already dead at the base, which is how every deliberate negative fixture in
# this repo (`does_not_exist.go`, `no_such_handler_file.go`, the
# telemetry-coverage ghost rows, the cigates selector's synthetic `r.go`) stays
# green. A gate that failed on those would be unfixable.
#
# Runs without Postgres, NornicDB, or a Go build.
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
gate="${script_dir}/verify-moved-file-refs.sh"
helper="${script_dir}/lib/gate-diff-base.sh"
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

# 1. THE CORE CASE: branch moves the file, doc still points at the old path.
repo="$(new_repo)"
mkdir -p "$repo/go/internal/reducer/widgetfam"
git -C "$repo" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
git -C "$repo" commit -qm "move widget family"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "fails when a branch moves a file and leaves a reference" 1 "$rc"
check_output "names the referencing site" "docs/design.md" "$out"
check_output "names the repoint target" "go/internal/reducer/widgetfam/widget.go" "$out"
rm -rf "$repo"

# 2. Same move, but the reference was updated: clean.
repo="$(new_repo)"
mkdir -p "$repo/go/internal/reducer/widgetfam"
git -C "$repo" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
printf 'See go/internal/reducer/widgetfam/widget.go for the widget family.\n' >"$repo/docs/design.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "move widget family and repoint"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "passes when the branch repoints its own reference" 0 "$rc"
rm -rf "$repo"

# 3. ATTRIBUTION: a path deleted BEFORE the base is inherited debt. The branch
#    did not break that reference, so the gate must stay silent about it --
#    the header's "inherited debt did not change, so it cannot either", and
#    what keeps the disclosed residual debt from failing unrelated PRs.
#    The branch must ALSO vacate something real, or the case exits through the
#    same "vacates no go path" early return as case 4 and proves nothing about
#    attribution. That is precisely how the first version of this case was
#    vacuous: its fixture only appended doc lines, so vacated_n was 0 and the
#    gate never reached the scan. The "1 vacated go path" assertion below is
#    what makes the silence meaningful, and 3b is what proves it is
#    attribution rather than an inability to see the reference at all.
repo="$(new_repo)"
root="$(git -C "$repo" rev-parse HEAD)"
printf 'package reducer\n' >"$repo/go/internal/reducer/gadget.go"
{
  printf 'See go/internal/reducer/widget.go for the widget family.\n'
  printf 'See go/internal/reducer/gadget.go for the gadget family.\n'
} >"$repo/docs/design.md"
git -C "$repo" rm -q go/internal/reducer/widget.go
git -C "$repo" add -A
git -C "$repo" commit -qm base-debt
mkdir -p "$repo/go/internal/reducer/gadgetfam"
git -C "$repo" mv go/internal/reducer/gadget.go go/internal/reducer/gadgetfam/gadget.go
{
  printf 'See go/internal/reducer/widget.go for the widget family.\n'
  printf 'See go/internal/reducer/gadgetfam/gadget.go for the gadget family.\n'
} >"$repo/docs/design.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "move gadget family and repoint"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "ignores a reference that was already dead at the base" 0 "$rc"
check_output "reached the scan instead of the early exit" "1 vacated go path" "$out"

# 3b. CONTROL for case 3. Same tree, base widened past the deletion: the
#     inherited reference IS reported. Without this, case 3 passing could mean
#     the gate simply cannot see that reference.
set +e
out="$(cd "$repo" && ./scripts/verify-moved-file-refs.sh --base "$root" 2>&1)"
rc=$?
set -e
check "widening the base past the deletion reports the inherited reference" 1 "$rc"
check_output "names the inherited dead path" "go/internal/reducer/widget.go" "$out"
rm -rf "$repo"

# 4. A branch that vacates nothing exits early and cheaply.
repo="$(new_repo)"
printf 'unrelated change\n' >>"$repo/docs/design.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "unrelated"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "passes when the branch vacates no go path" 0 "$rc"
check_output "says it had nothing to check" "vacates no go" "$out"
rm -rf "$repo"

# 5. Outright DELETION (no rename target) still fails, with deletion wording.
repo="$(new_repo)"
git -C "$repo" rm -q go/internal/reducer/widget.go
git -C "$repo" commit -qm "delete widget"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "fails when a branch deletes a referenced file" 1 "$rc"
check_output "says the path was deleted" "was deleted by this branch" "$out"
check_output "points a delete at the branch's added files" "among the files this branch ADDED" "$out"
rm -rf "$repo"

# 5b. A move PLUS a rewrite too heavy for git to pair. This is the shape the
# deletion wording exists for: -M is left at its default similarity on purpose,
# so this arrives as D+A rather than R, `new` is empty, and the gate cannot name
# a target. Lowering the threshold would not fix it -- it would make git pair the
# delete with whatever add happened to be closest, so a CONFIDENTLY WRONG target
# would replace honest "drop or rewrite" guidance. The message points at the
# branch's added files instead, which is true without guessing which one.
repo="$(new_repo)"
git -C "$repo" rm -q go/internal/reducer/widget.go
mkdir -p "$repo/go/internal/reducer/widgetfam"
printf 'package widgetfam\n\n// Rewritten in the move: nothing here resembles the old file.\nfunc New() int { return 0 }\n' \
  >"$repo/go/internal/reducer/widgetfam/widget.go"
git -C "$repo" add -A
git -C "$repo" commit -qm "move widget and rewrite it"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "fails when a move+rewrite is unpaired by git" 1 "$rc"
check_output "unpaired move still names the vacated path" "was deleted by this branch" "$out"
check_output "unpaired move points at the added files" "among the files this branch ADDED" "$out"
rm -rf "$repo"

# 6. The allowlist exempts a deliberately historical reference.
repo="$(new_repo)"
mkdir -p "$repo/go/internal/reducer/widgetfam"
git -C "$repo" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
printf '# historical transcript, repointing would falsify the record\n' \
  >"$repo/scripts/moved-file-refs-allowlist.txt"
printf 'docs/design.md:go/internal/reducer/widget.go\n' \
  >>"$repo/scripts/moved-file-refs-allowlist.txt"
git -C "$repo" add -A
git -C "$repo" commit -qm "move widget family with allowlist"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "allowlist exempts a deliberately historical reference" 0 "$rc"
rm -rf "$repo"

# 7. An allowlist entry is scoped to one file: it must not exempt a DIFFERENT
#    referencing file naming the same vacated path.
repo="$(new_repo)"
mkdir -p "$repo/go/internal/reducer/widgetfam"
git -C "$repo" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
printf 'Also see go/internal/reducer/widget.go here.\n' >"$repo/docs/other.md"
printf 'docs/design.md:go/internal/reducer/widget.go\n' \
  >"$repo/scripts/moved-file-refs-allowlist.txt"
git -C "$repo" add -A
git -C "$repo" commit -qm "move widget family, allowlist only one file"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "allowlist does not leak to another referencing file" 1 "$rc"
check_output "still reports the unallowlisted file" "docs/other.md" "$out"
rm -rf "$repo"

# 8. Usage contract.
set +e
"$gate" --help >/dev/null 2>&1
rc=$?
set -e
check "--help exits 0" 0 "$rc"

set +e
"$gate" --bogus-flag >/dev/null 2>&1
rc=$?
set -e
check "an unknown flag exits 2" 2 "$rc"

# 9. FAILS CLOSED. An unresolvable --base must abort with exit 2, never exit 0.
#    The evidence note cites this as the reason the gate can never pass
#    silently, so the claim needs a case rather than a one-off manual run. The
#    all-zero ref is used deliberately: it resolves in no clone, and the gate
#    aborts at the `git diff` before any sweep, so this case costs one git call.
repo="$(new_repo)"
set +e
out="$(cd "$repo" && ./scripts/verify-moved-file-refs.sh \
  --base 0000000000000000000000000000000000000000 2>&1)"
rc=$?
set -e
check "an unresolvable base exits 2, not 0" 2 "$rc"
check_output "says the scan did not run" "the scan did not run" "$out"
rm -rf "$repo"

if [ "$failures" -gt 0 ]; then
  printf '\ntest-verify-moved-file-refs: %s failure(s)\n' "$failures" >&2
  exit 1
fi
printf '\ntest-verify-moved-file-refs: all cases passed\n'
