// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"testing"
)

const dirFoo = "internal/foo"

// seedBase commits n files in dirFoo on main, then commits the dirgate
// baselines generated from that tree, and returns the repo.
func seedBase(t *testing.T, n int) *scratchRepo {
	t.Helper()
	r := newScratchRepo(t)
	r.addGoFiles(dirFoo, "f", 0, n)
	r.commit("base")
	r.baseline()
	return r
}

// branchWithGrowth cuts feature from main and adds count files in one commit
// whose message is msg.
func branchWithGrowth(r *scratchRepo, count int, msg string) {
	r.git("checkout", "-q", "-b", "feature")
	r.addGoFiles(dirFoo, "g", 0, count)
	r.commit(msg)
}

func growthTrailer(n int) string {
	return fmt.Sprintf("grow\n\nDirgate-Growth: %s +%d #6054 needed for the test", dirFoo, n)
}

// ---- RED ----

func TestRedOverCapGrowthWithoutTrailer(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, "grow")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1,
		"RED", "internal/foo", "41", "42", "Dirgate-Growth: internal/foo +1 #")
}

func TestRedNewDirectoryOverCapWithoutTrailer(t *testing.T) {
	r := seedBase(t, 3)
	r.git("checkout", "-q", "-b", "feature")
	r.addGoFiles("internal/bar", "b", 0, 41)
	r.commit("new dir")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1,
		"RED", "internal/bar", "0", "41", "Dirgate-Growth: internal/bar +1 #")
}

func TestRedTrailerUnderWaivesGrowth(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 2, growthTrailer(1))
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "+2")
}

func TestRedTrailerOverWaivesGrowth(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, growthTrailer(2))
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "+1")
}

func TestRedTrailerForDirectoryThatDidNotGrow(t *testing.T) {
	r := seedBase(t, 10)
	branchWithGrowth(r, 1, growthTrailer(1)) // 11 files: far below the cap.
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "unused")
}

func TestRedTrailerWithoutIssue(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, "grow\n\nDirgate-Growth: internal/foo +1 no issue here")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "issue")
}

func TestRedTrailerWithoutReason(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, "grow\n\nDirgate-Growth: internal/foo +1 #6054")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "reason")
}

func TestRedTrailerZeroOrMalformedCount(t *testing.T) {
	for _, line := range []string{
		"Dirgate-Growth: internal/foo +0 #6054 nothing",
		"Dirgate-Growth: internal/foo 1 #6054 missing plus",
		"Dirgate-Growth: internal/foo",
	} {
		r := seedBase(t, 41)
		branchWithGrowth(r, 1, "grow\n\n"+line)
		r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED")
	}
}

func TestRedTrailerForMissingDirectory(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, "grow\n\nDirgate-Growth: internal/nope +1 #6054 typo")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "internal/nope", "not a directory")
}

func TestRedNamesTheCommitCarryingAnInvalidTrailer(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, "grow\n\nDirgate-Growth: internal/foo +1 #6054")
	sha := r.git("rev-parse", "--short=12", "HEAD")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, sha)
}

func TestRedNewFileBesideExistingSubpackage(t *testing.T) {
	r := newScratchRepo(t)
	r.write("go/internal/p/doc.go", "package p\n")
	r.write("go/internal/p/bar/a.go", "package bar\n")
	r.commit("base")
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/p/bar_x.go", "package p\n")
	r.commit("add bar_x")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "bar_x.go", "bar")
}

func TestRedNewSubpackageBesideExistingFile(t *testing.T) {
	r := newScratchRepo(t)
	r.write("go/internal/p/doc.go", "package p\n")
	r.write("go/internal/p/bar_x.go", "package p\n")
	r.commit("base")
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/p/bar/a.go", "package bar\n")
	r.commit("add bar/")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "bar_x.go")
}

func TestNamingEscapeNeedsReason(t *testing.T) {
	r := newScratchRepo(t)
	r.write("go/internal/p/bar/a.go", "package bar\n")
	r.commit("base")
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/p/bar_x.go", "package p //nolint:dirgate // legacy name kept for wire compatibility\n")
	r.commit("add escaped bar_x")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
	r.write("go/internal/p/bar_y.go", "package p //nolint:dirgate\n")
	r.commit("add bar_y without a reason")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "bar_y.go")
}

func TestRedCapNolintDoesNotWaive(t *testing.T) {
	r := seedBase(t, 41)
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/foo/doc.go", "package foo //nolint:dirgate // keep growing\n")
	r.addGoFiles(dirFoo, "g", 0, 1)
	r.commit("grow under a cap nolint")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "41", "43")
}

func TestRedTwoBranchesEachGrowOneWithOneTrailerTotal(t *testing.T) {
	r := seedBase(t, 41)
	r.git("checkout", "-q", "-b", "a")
	r.addGoFiles(dirFoo, "a", 0, 1)
	r.commit(growthTrailer(1))
	r.git("checkout", "-q", "main")
	r.git("checkout", "-q", "-b", "b")
	r.addGoFiles(dirFoo, "b", 0, 1)
	r.commit("grow b")
	r.git("checkout", "-q", "-b", "combined")
	r.git("merge", "-q", "--no-edit", "a")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "41", "43")
}

func TestRedNoResolvableBaseExitsTwo(t *testing.T) {
	r := seedBase(t, 3)
	r.run("-base-ref", "does-not-exist", "pre-push").mustExit(t, 2)
	r.run("-base-ref", "does-not-exist", "pre-commit").mustExit(t, 2)
}

func TestRedStaleMergeBaseWouldHideGrowth(t *testing.T) {
	// main moved 5 files out of foo after the PR branched. Against the merge
	// base the PR reads as a shrink; against HEAD^1 of the merge it grew.
	r := seedBase(t, 50)
	r.git("checkout", "-q", "-b", "pr")
	r.addGoFiles(dirFoo, "p", 0, 1)
	r.commit("pr adds one")
	r.git("checkout", "-q", "main")
	for i := 0; i < 5; i++ {
		r.rm(fmt.Sprintf("go/%s/f%03d.go", dirFoo, i))
	}
	r.commit("main moves five out")
	r.git("checkout", "-q", "-b", "mergeref")
	r.git("merge", "-q", "--no-ff", "-m", "Merge pr", "pr")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").
		mustExit(t, 1, "RED", "45", "46")
}

func TestRedCIModeNeedsAMergeCommitOnPullRequest(t *testing.T) {
	r := seedBase(t, 3)
	r.addGoFiles(dirFoo, "x", 0, 1)
	r.commit("plain commit, not a merge ref")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").mustExit(t, 2)
}

// ---- GREEN ----

func TestGreenShrinkOfOverCapDirectory(t *testing.T) {
	r := seedBase(t, 45)
	r.git("checkout", "-q", "-b", "feature")
	r.rm("go/internal/foo/f000.go")
	r.commit("move one out")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestGreenSameCountSwap(t *testing.T) {
	r := seedBase(t, 45)
	r.git("checkout", "-q", "-b", "feature")
	r.rm("go/internal/foo/f000.go")
	r.addGoFiles(dirFoo, "swap", 0, 1)
	r.commit("swap one for another")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestGreenRenameWithinDirectory(t *testing.T) {
	r := seedBase(t, 45)
	r.git("checkout", "-q", "-b", "feature")
	r.git("mv", "go/internal/foo/f000.go", "go/internal/foo/renamed.go")
	r.commit("rename")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestGreenGrowthWithExactTrailer(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, growthTrailer(1))
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestGreenSameTrailerAfterBaseGrowsFromAnotherPR(t *testing.T) {
	r := seedBase(t, 41)
	r.git("checkout", "-q", "-b", "pr")
	r.addGoFiles(dirFoo, "p", 0, 1)
	r.commit(growthTrailer(1))
	r.git("checkout", "-q", "main")
	r.addGoFiles(dirFoo, "other", 0, 1)
	r.commit(growthTrailer(1)) // another PR landed +1 with its own trailer
	r.git("checkout", "-q", "-b", "mergeref")
	r.git("merge", "-q", "--no-ff", "-m", "Merge pr", "pr")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").mustExit(t, 0)
}

func TestCapBoundary(t *testing.T) {
	r := seedBase(t, 39)
	branchWithGrowth(r, 1, "reach forty") // 40 files
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
	r.addGoFiles(dirFoo, "h", 0, 1)
	r.commit("forty one")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "39", "41")
}

func TestGreenBaselinedNamingCollisionNeedsNoNolint(t *testing.T) {
	r := newScratchRepo(t)
	r.write("go/internal/parser/go_language.go", "package parser\n")
	r.write("go/internal/parser/go/a.go", "package golang\n")
	r.commit("base")
	r.baseline()
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/parser/unrelated.go", "package parser\n")
	r.commit("touch the dir")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestStaleCapNolintMarkerIsNoted(t *testing.T) {
	r := seedBase(t, 3)
	r.write("go/internal/foo/doc.go", "package foo //nolint:dirgate // old cap escape\n")
	r.commit("stale marker")
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/foo/other.go", "package foo\n")
	r.commit("feature")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0, "NOTE", "stale", "go/internal/foo/doc.go")
}

func TestPreCommitChecksOnlyChangedDirectories(t *testing.T) {
	r := seedBase(t, 3)
	r.addGoFiles("internal/other", "o", 0, 41)
	r.commit("other is over the cap on main already")
	r.git("checkout", "-q", "-b", "feature")
	r.addGoFiles(dirFoo, "n", 0, 1)
	r.git("add", "-A")
	r.run("-base-ref", "main", "pre-commit", "go/internal/foo/n000.go").
		mustExit(t, 0, "evaluated 1 director")
}

func TestPreCommitCannotSeeThePendingMessageSoGrowthIsANote(t *testing.T) {
	r := seedBase(t, 41)
	r.git("checkout", "-q", "-b", "feature")
	r.addGoFiles(dirFoo, "n", 0, 1)
	r.git("add", "-A")
	r.run("-base-ref", "main", "pre-commit", "go/internal/foo/n000.go").
		mustExit(t, 0, "NOTE", "Dirgate-Growth: internal/foo +1 #")
}

func TestPreCommitNamingViolationIsRed(t *testing.T) {
	r := newScratchRepo(t)
	r.write("go/internal/p/bar/a.go", "package bar\n")
	r.commit("base")
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/p/bar_x.go", "package p\n")
	r.git("add", "-A")
	r.run("-base-ref", "main", "pre-commit", "go/internal/p/bar_x.go").mustExit(t, 1, "bar_x.go")
}

func TestPreCommitIgnoresFilesOutsideGo(t *testing.T) {
	r := seedBase(t, 3)
	r.git("checkout", "-q", "-b", "feature")
	r.write("tools/x/y.go", "package y\n")
	r.git("add", "-A")
	r.run("-base-ref", "main", "pre-commit", "tools/x/y.go").mustExit(t, 0, "evaluated 0 director")
}

func TestPrePushFallsBackToMergeBaseOnConflict(t *testing.T) {
	r := seedBase(t, 3)
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/foo/f000.go", "package x\n// feature\n")
	r.commit("feature edits f000")
	r.git("checkout", "-q", "main")
	r.write("go/internal/foo/f000.go", "package x\n// main\n")
	r.commit("main edits f000")
	r.git("checkout", "-q", "feature")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0, "merge-base", "rebase")
}

// TestPreCommitWorksWhileGitCommitHoldsTheIndexLock pins the hook shape: git
// commit keeps index.lock held while it runs pre-commit hooks, so a plain
// `git write-tree` fails with "Unable to create index.lock" whenever the
// index's cache-tree is stale (right after `git add`). The gate must read the
// index tree without taking that lock.
func TestPreCommitWorksWhileGitCommitHoldsTheIndexLock(t *testing.T) {
	r := seedBase(t, 3)
	r.git("checkout", "-q", "-b", "feature")
	r.addGoFiles(dirFoo, "n", 0, 2)
	r.git("add", "-A") // leaves the cache-tree stale
	r.write(".git/index.lock", "")
	r.run("-base-ref", "main", "pre-commit", "go/internal/foo/n000.go").
		mustExit(t, 0, "evaluated 1 director")
}
