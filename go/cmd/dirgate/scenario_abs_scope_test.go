// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "testing"

// TestAbsR2fARenameOrSwapKeepsTheOvershootDirectoryInScope: a PR that renames a
// file, or swaps one file for another, inside an overshoot directory leaves the
// file count unchanged (41 -> 41) but changes the file set, so the directory is
// touched and the absolute term applies. It must not be read as "untouched"
// just because the count or the length of the set matches.
func TestAbsR2fARenameOrSwapKeepsTheOvershootDirectoryInScope(t *testing.T) {
	pr := map[string]string{"GITHUB_EVENT_NAME": "pull_request"}
	for _, tc := range []struct {
		name string
		edit func(r *scratchRepo)
	}{
		{"rename one file", func(r *scratchRepo) {
			r.git("mv", "go/internal/foo/b000.go", "go/internal/foo/z000.go")
		}},
		{"swap one file for another", func(r *scratchRepo) {
			r.rm("go/internal/foo/b000.go")
			r.addGoFiles(dirFoo, "y", 0, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := raceRepo(t)
			mergeBranch(r, "b")
			r.git("checkout", "-q", "-b", "reshuffle", "main")
			tc.edit(r)
			r.commit("reshuffle one file")
			r.git("checkout", "-q", "main")
			r.git("merge", "-q", "--no-ff", "-m", "Merge reshuffle", "reshuffle")
			r.runEnv(pr, "ci").mustExit(t, 1, "RED", "internal/foo", "41 -> 41", "needs +1")
		})
	}
}

// TestAbsR2gPrePushIsScopedLikeThePullRequestItSimulates: pre-push mirrors the
// PR check, so an overshoot that already sits on the base (internal/foo at 41,
// W=0) does not hold an unrelated branch's floor RED. A branch that changes the
// directory owes it, and a valid trailer names its directory into scope. The
// full sweep that catches the debt where it is made is the push run on main
// (R2b).
func TestAbsR2gPrePushIsScopedLikeThePullRequestItSimulates(t *testing.T) {
	newBranch := func(t *testing.T, name string) *scratchRepo {
		r := raceRepo(t)
		mergeBranch(r, "b")
		r.git("checkout", "-q", "-b", name, "main")
		return r
	}
	t.Run("a branch that does not touch the directory is green", func(t *testing.T) {
		r := newBranch(t, "unrelated")
		r.write("go/internal/elsewhere/x.go", "package elsewhere\n")
		r.commit("a following unrelated change")
		r.run("-base-ref", "main", "pre-push").mustExit(t, 0, "0 RED")
	})
	t.Run("a branch that adds a file to it owes the overshoot", func(t *testing.T) {
		r := newBranch(t, "grow")
		r.addGoFiles(dirFoo, "m", 0, 1)
		r.commit("one more file")
		r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "internal/foo", "needs +2")
	})
	t.Run("a branch that renames a file in it owes the overshoot", func(t *testing.T) {
		r := newBranch(t, "rename")
		r.git("mv", "go/internal/foo/b000.go", "go/internal/foo/z000.go")
		r.commit("rename one file")
		r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "internal/foo", "41 -> 41", "needs +1")
	})
	t.Run("a valid trailer counts the directory it names", func(t *testing.T) {
		r := newBranch(t, "repair")
		r.commit(growthTrailer(1))
		r.run("-base-ref", "main", "pre-push").mustExit(t, 0, "0 RED")
		r2 := newBranch(t, "overwaive")
		r2.commit(growthTrailer(2))
		r2.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "over-waived")
	})
	t.Run("the merge-base fallback after a conflicting merge is scoped too", func(t *testing.T) {
		r := raceRepo(t)
		mergeBranch(r, "b")
		r.git("checkout", "-q", "-b", "clash", "main")
		r.write("go/internal/elsewhere/x.go", "package elsewhere\n\nvar fromBranch = 1\n")
		r.commit("branch edits x.go")
		r.git("checkout", "-q", "main")
		r.write("go/internal/elsewhere/x.go", "package elsewhere\n\nvar fromMain = 2\n")
		r.commit("main edits x.go")
		r.git("checkout", "-q", "clash")
		r.run("-base-ref", "main", "pre-push").mustExit(t, 0, "0 RED", "merge-base fallback")
	})
	t.Run("the same base is still red on the main-side push run", func(t *testing.T) {
		r := newBranch(t, "unrelated")
		r.write("go/internal/elsewhere/x.go", "package elsewhere\n")
		r.commit("a following unrelated change")
		r.git("checkout", "-q", "main")
		r.git("merge", "-q", "--no-ff", "-m", "Merge unrelated", "unrelated")
		r.runEnv(pushEvent, "ci").mustExit(t, 1, "RED", "internal/foo", "41 -> 41", "never waived")
	})
}
