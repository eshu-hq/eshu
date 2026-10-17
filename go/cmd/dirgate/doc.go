// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command dirgate enforces the per-directory cap on non-test .go files under
// go/ (issue #6054) as a ratchet derived from git, and the sibling-subpackage
// naming rule. It keeps two small write-once baselines of what already existed
// and no digest ledger.
//
// # The rule
//
// For every package directory D under go/ (vendor, testdata, generated and
// hidden segments are skipped):
//
//	b  = non-test .go files directly in D at the base
//	h  = the same at the head tree
//	B0 = baseline-cap.tsv row for D (0 if none)
//	W  = growth waived by valid trailers in every commit reachable from the base
//	w  = growth waived by valid trailers in the commits of base..head
//
//	delta_needed = max(0, h - max(40, b))
//	abs_needed   = max(0, h - max(40, B0) - W)
//	needed       = max(delta_needed, abs_needed)
//
// D is RED when w != needed: too little, too much, and an unused trailer all
// fail. delta_needed guards each merge. abs_needed catches growth that landed
// concurrently and was never waived: two PRs each add one file to a 39-file
// directory, both pass their own run, both land, and the second push run is RED.
// Shrinking, swapping files at the same count, and renaming inside a directory
// are GREEN with no action. A move that creates a new directory over 40 files
// needs a trailer. A waiver is a trailer in a commit message:
//
//	Dirgate-Growth: <D> +<N> #<issue> <reason>
//
// A trailer is invalid when D is not a directory in the head tree, N is below 1,
// the #issue is missing, or the reason is empty. A trailer cannot authorize later
// growth.
//
// # Naming rule
//
// A file F in D collides with a sibling subpackage S when F's stem equals S or
// starts with S_. The collision is RED on every event unless F has
// `//nolint:dirgate // <reason>` on its package line or a row in
// baseline-naming.tsv. The marker is the only per-file escape and does not waive
// the cap; the tool prints a NOTE for each marker that is not a live naming escape.
//
// # Baselines
//
// go/cmd/dirgate/baseline-cap.tsv (dir<TAB>count for every directory over 40)
// and baseline-naming.tsv (dir<TAB>file for every collision) are write-once. If
// a file exists at the base, any diff to it at the head is RED and no trailer
// waives it. Creating a file absent at the base is the bootstrap and is GREEN.
// The `baseline` mode generates both deterministically from a tree. Run it at
// landing and after any rebase, never after the baselines are on main.
//
// # Modes and base resolution
//
// Every event is enforcing, with one exception: pre-commit treats growth and
// trailer findings as NOTE because the commit message does not exist yet.
//
//   - ci: head is HEAD (the merge ref), base is HEAD^1. A merge-base would read
//     a PR that branched before main moved files out of D as a shrink.
//     pull_request requires a merge commit. push, schedule, merge_group,
//     workflow_dispatch and an empty event name enforce the same way.
//   - pre-push: head is `git merge-tree --write-tree <base-ref> HEAD`, base is
//     the base ref (default origin/main). On a merge conflict the run falls
//     back to the merge base, prints that line, and the branch needs a rebase.
//   - pre-commit: head is the index, base is the merge base, and only the
//     directories of the staged files are judged. Naming and baseline findings
//     stay RED.
//   - baseline: prints or writes the two baseline TSVs for -base-ref.
//
// A shallow checkout is unshallowed with one `git fetch --no-tags
// --filter=blob:none --unshallow origin` before any waiver is read. There is no
// commit-count limit. A failed fetch, an unresolvable base, or an unreadable
// history exits 2, never 0. Exit status: 0 green, 1 RED, 2 the comparison could
// not be made.
package main
