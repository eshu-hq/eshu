#!/usr/bin/env bash
#
# test-verify-moved-file-refs.sh - Go-path mirror cases for
# verify-moved-file-refs.sh.
#
# Builds throwaway git repos that reproduce the #6061 shape the gate exists to
# catch (a branch moves a Go file and leaves a pointer to the path it vacated)
# and, just as importantly, the shapes it must NOT catch: a reference that was
# already dead at the base, which is how every deliberate negative fixture in
# this repo (`does_not_exist.go`, `no_such_handler_file.go`, the
# telemetry-coverage ghost rows, the cigates selector's synthetic `r.go`) stays
# green. A gate that failed on those would be unfixable. Markdown-path cases
# live in test-verify-moved-file-refs-md.sh; shared builders live in
# lib/test-verify-moved-file-refs-harness.sh.
#
# Runs without Postgres, NornicDB, or a Go build.
set -euo pipefail

# shellcheck source=scripts/lib/test-verify-moved-file-refs-harness.sh
. "$(cd "$(dirname "$0")" && pwd)/lib/test-verify-moved-file-refs-harness.sh"

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

# 2b. CITATION STYLE: the reference drops the leading "go/" and is written in
#     Go-import style. This is the dominant citation style in this repo's docs,
#     AGENTS.md files and Go comments, and greping only the repo-root-relative
#     form git diff reports made it invisible -- #6781 Part B shipped ~20
#     dangling bare-style citations past a clean gate run.
repo="$(new_repo)"
printf 'See internal/reducer/widget.go for the widget family.\n' >"$repo/docs/design.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "cite the widget family without the go/ prefix"
mkdir -p "$repo/go/internal/reducer/widgetfam"
git -C "$repo" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
git -C "$repo" commit -qm "move widget family"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "fails on a bare Go-import-style citation with no go/ prefix" 1 "$rc"
check_output "names the bare-style referencing site" "docs/design.md" "$out"
rm -rf "$repo"

# 3. ATTRIBUTION: a path deleted BEFORE the base is inherited debt. The branch
#    did not break that reference, so the gate must stay silent about it --
#    the header's "inherited debt did not change, so it cannot either", and
#    what keeps the disclosed residual debt from failing unrelated PRs.
#    The branch must ALSO vacate something real, or the case exits through the
#    same "vacates no tracked" early return as case 4 and proves nothing about
#    attribution. That is precisely how the first version of this case was
#    vacuous: its fixture only appended doc lines, so vacated_n was 0 and the
#    gate never reached the scan. The "1 vacated path" assertion below is
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
check_output "reached the scan instead of the early exit" "1 vacated path" "$out"

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
check "passes when the branch vacates no tracked path" 0 "$rc"
check_output "says it had nothing to check" "vacates no tracked" "$out"
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

# 7b. A fragment in allowlist.d/ exempts a historical reference, and the
#     exemption is load-bearing: the same tree without the fragment fails.
repo="$(new_repo)"
root="$(git -C "$repo" rev-parse HEAD)"
mkdir -p "$repo/go/internal/reducer/widgetfam" "$repo/scripts/moved-file-refs-allowlist.d"
git -C "$repo" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
printf '# frozen main file: new entries live in fragments\n' \
  >"$repo/scripts/moved-file-refs-allowlist.d/go-internal-reducer.txt"
printf 'docs/design.md:go/internal/reducer/widget.go\n' \
  >>"$repo/scripts/moved-file-refs-allowlist.d/go-internal-reducer.txt"
git -C "$repo" add -A
git -C "$repo" commit -qm "move widget family with fragment exemption"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "allowlist.d fragment exempts a historical reference" 0 "$rc"
git -C "$repo" rm -q scripts/moved-file-refs-allowlist.d/go-internal-reducer.txt
git -C "$repo" commit -qm "drop the fragment"
set +e
out="$(cd "$repo" && ./scripts/verify-moved-file-refs.sh --base "$root" 2>&1)"
rc=$?
set -e
check "the same tree without the fragment fails" 1 "$rc"
check_output "names the vacated path once the fragment is gone" "go/internal/reducer/widget.go" "$out"
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

# 10. PULL REQUEST MERGE REF (#10222). On a pull_request run the job checks out
#     GitHub's merge ref, whose first parent is the base branch as it stood
#     when the ref was built. Diffing against origin/$GITHUB_BASE_REF fetched at
#     job time also contains commits that landed on the base afterwards, and
#     reads each one as this branch's own change. The gate must diff against
#     the first parent, and only for a pull_request event on a two-parent HEAD.

repo="$(new_merge_ref_repo other)"
set +e
out="$(run_gate_env "$repo" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)"
rc=$?
set -e
check "merge ref: a base that moved after the ref was built adds no change" 0 "$rc"
check_output "merge ref: the moved base tip is not read as the PR's deletion" "vacates no tracked" "$out"

# 10b. CONTROL. An explicit override still wins over the merge ref parent, and
#      pointing it at the moved tip brings the false failure back. Without this,
#      10 passing could mean the fixture never exercised the moved base.
set +e
out="$(run_gate_env "$repo" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main \
  ESHU_MOVED_FILE_REFS_BASE=origin/main)"
rc=$?
set -e
check "merge ref: an explicit base override still wins" 1 "$rc"
check_output "merge ref: the control reads the later main commit as a deletion" \
  "go/internal/reducer/future.go" "$out"
rm -rf "$repo"

# 10c. The PR's own move is still caught on the merge ref, and only the PR's
#      own move: the later main commit must not be counted with it.
repo="$(new_merge_ref_repo move)"
set +e
out="$(run_gate_env "$repo" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)"
rc=$?
set -e
check "merge ref: a PR that moves a file and leaves a reference still fails" 1 "$rc"
check_output "merge ref: names the moved path" "go/internal/reducer/widget.go" "$out"
case "$out" in
  *future.go*)
    printf 'FAIL merge ref: the later main commit was counted with the PR: %s\n' "$out" >&2
    failures=$((failures + 1))
    ;;
  *) printf 'ok   merge ref: the later main commit is not counted with the PR\n' ;;
esac
rm -rf "$repo"

# 10d. GUARDS. Parent 1 is the base only for a pull_request event on a
#      two-parent HEAD. A developer who merged main into a feature branch has a
#      two-parent HEAD whose first parent is their own tip, already carrying the
#      move; trusting it would hide the move. Each case below must still fail.
repo="$(new_local_merge_repo)"
set +e
out="$(run_gate_env "$repo")"
rc=$?
set -e
check "guard: a local merge with no event still sees the move" 1 "$rc"
check_output "guard: no event names the moved path" "go/internal/reducer/widget.go" "$out"
set +e
out="$(run_gate_env "$repo" GITHUB_EVENT_NAME=push)"
rc=$?
set -e
check "guard: a push event on a merge commit still sees the move" 1 "$rc"
check_output "guard: push event names the moved path" "go/internal/reducer/widget.go" "$out"
rm -rf "$repo"

# A pull_request run whose HEAD has one parent is not the merge ref. Its first
# parent is the branch's own previous commit, which already carries the move.
repo="$(new_repo)"
git -C "$repo" branch -M main
git -C "$repo" update-ref refs/remotes/origin/main HEAD
git -C "$repo" checkout -q -b feature
mkdir -p "$repo/go/internal/reducer/widgetfam"
git -C "$repo" mv go/internal/reducer/widget.go go/internal/reducer/widgetfam/widget.go
git -C "$repo" commit -qm "feature moves widget"
printf 'unrelated\n' >"$repo/docs/notes.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "feature follow-up"
set +e
out="$(run_gate_env "$repo" GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main)"
rc=$?
set -e
check "guard: a single-parent HEAD on a pull request still sees the move" 1 "$rc"
check_output "guard: single-parent HEAD names the moved path" "go/internal/reducer/widget.go" "$out"
rm -rf "$repo"

# Markdown-path cases (11-11d) live in test-verify-moved-file-refs-md.sh.

mirror_finish "test-verify-moved-file-refs"
