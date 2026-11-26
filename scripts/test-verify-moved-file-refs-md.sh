#!/usr/bin/env bash
#
# test-verify-moved-file-refs-md.sh - Markdown-path mirror cases for
# verify-moved-file-refs.sh (#11532).
#
# A link to a vacated .md file is usually a same-directory basename
# ([text](sibling.md)), which a full-path grep never matches. These cases pin
# the basename sweep: deletion with a dead link fails, a dropped link passes
# through the scan (not the early exit), and a rename that keeps the basename
# plus a correct relative repoint passes because the line already resolves to
# the live target. Shared builders live in
# lib/test-verify-moved-file-refs-harness.sh.
#
# Runs without Postgres, NornicDB, or a Go build.
set -euo pipefail

# shellcheck source=scripts/lib/test-verify-moved-file-refs-harness.sh
. "$(cd "$(dirname "$0")" && pwd)/lib/test-verify-moved-file-refs-harness.sh"

# 11. MD VACATED (#11532): the branch deletes a .md file while a same-directory
#     .md link still names its basename. Full-path grep never matches a
#     same-dir `[text](sibling.md)` link, so this is the shape no gate caught.
repo="$(new_repo)"
printf '# probe target\n' >"$repo/docs/probe-target.md"
printf 'See [the probe](probe-target.md) for details.\n' >"$repo/docs/probe-ref.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "add md probe pair"
root="$(git -C "$repo" rev-parse HEAD)"
git -C "$repo" rm -q docs/probe-target.md
git -C "$repo" commit -qm "delete probe target"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "fails when a branch deletes a referenced md file" 1 "$rc"
check_output "names the md referencing site" "docs/probe-ref.md" "$out"
check_output "names the vacated md path" "docs/probe-target.md" "$out"

# 11b. CONTROL: dropping the dead link makes the same branch clean, so case 11
#      passing cannot mean the gate fires on every md deletion. The base is
#      pinned at the pre-delete commit so the run reaches the scan (proving a
#      one-path vacated set with no hits) instead of exiting through the
#      vacates-nothing early return, which would prove nothing about over-firing.
printf 'The probe target is gone; see the widget family instead.\n' >"$repo/docs/probe-ref.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "drop the dead md link"
set +e
out="$(cd "$repo" && ./scripts/verify-moved-file-refs.sh --base "$root" 2>&1)"
rc=$?
set -e
check "passes once the dead md link is dropped" 0 "$rc"
check_output "reached the scan with one vacated md path" "1 vacated path" "$out"
rm -rf "$repo"

# 11c. Rename that keeps the basename plus a correct relative repoint must NOT
#      fail: the line names the vacated basename but resolves to the live
#      target, so reporting it would falsely claim it resolves to nothing.
repo="$(new_repo)"
printf '# probe target\n' >"$repo/docs/probe-target.md"
printf 'See [the probe](probe-target.md) for details.\n' >"$repo/docs/probe-ref.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "add md probe pair"
root="$(git -C "$repo" rev-parse HEAD)"
mkdir -p "$repo/docs/probe-sub"
git -C "$repo" mv docs/probe-target.md docs/probe-sub/probe-target.md
printf 'See [the probe](probe-sub/probe-target.md) for details.\n' >"$repo/docs/probe-ref.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "move probe target and repoint"
set +e
out="$(cd "$repo" && ./scripts/verify-moved-file-refs.sh --base "$root" 2>&1)"
rc=$?
set -e
check "passes when a rename keeps the basename with a correct repoint" 0 "$rc"
check_output "reached the scan with one vacated md path" "1 vacated path" "$out"
rm -rf "$repo"

# 11d. CONTROL for 11c: the same rename with the dead link left behind still
#      fails, so 11c passing cannot mean renames are invisible to the gate.
repo="$(new_repo)"
printf '# probe target\n' >"$repo/docs/probe-target.md"
printf 'See [the probe](probe-target.md) for details.\n' >"$repo/docs/probe-ref.md"
git -C "$repo" add -A
git -C "$repo" commit -qm "add md probe pair"
mkdir -p "$repo/docs/probe-sub"
git -C "$repo" mv docs/probe-target.md docs/probe-sub/probe-target.md
git -C "$repo" add -A
git -C "$repo" commit -qm "move probe target without repoint"
set +e
out="$(run_gate "$repo")"
rc=$?
set -e
check "fails when a rename leaves the dead md link" 1 "$rc"
check_output "names the md referencing site after a rename" "docs/probe-ref.md" "$out"
check_output "names the rename repoint target" "docs/probe-sub/probe-target.md" "$out"
rm -rf "$repo"

mirror_finish "test-verify-moved-file-refs-md"
