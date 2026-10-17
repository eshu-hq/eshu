// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
	"testing"
)

const (
	baselineCapPath    = "go/cmd/dirgate/baseline-cap.tsv"
	baselineNamingPath = "go/cmd/dirgate/baseline-naming.tsv"
)

var pushEvent = map[string]string{"GITHUB_EVENT_NAME": "push"}

// mergeBranch merges branch into main with a merge commit and leaves main
// checked out, which is the commit a push run sees.
func mergeBranch(r *scratchRepo, branch string) {
	r.git("checkout", "-q", "main")
	r.git("merge", "-q", "--no-ff", "-m", "Merge "+branch, branch)
}

// raceRepo builds the concurrent-merge race: a 39-file directory, two branches
// that each add one file with no trailer, and both merged. main ends at 41
// files, 1 over the cap with no waiver anywhere.
func raceRepo(t *testing.T) *scratchRepo {
	t.Helper()
	r := seedBase(t, 39)
	for _, name := range []string{"a", "b"} {
		r.git("checkout", "-q", "-b", name, "main")
		r.addGoFiles(dirFoo, name, 0, 1)
		r.commit("add " + name)
	}
	mergeBranch(r, "a")
	return r
}

// ---- RED ----

func TestAbsR1ConcurrentGrowthIsRedOnThePushRunOfTheSecondMerge(t *testing.T) {
	r := raceRepo(t)
	r.run("-event", "push", "-head-ref", "HEAD", "ci").mustExit(t, 0) // A alone: 39 -> 40 is fine.
	mergeBranch(r, "b")
	r.runEnv(pushEvent, "ci").mustExit(t, 1, "RED", "internal/foo", "40 -> 41", "needs +1")
}

func TestAbsR2ALaterUnrelatedPRIsRedWithTheOvershoot(t *testing.T) {
	r := raceRepo(t)
	mergeBranch(r, "b")
	r.git("checkout", "-q", "-b", "unrelated", "main")
	r.write("go/internal/elsewhere/x.go", "package elsewhere\n")
	r.commit("touch another directory")
	r.git("checkout", "-q", "main")
	r.git("merge", "-q", "--no-ff", "-m", "Merge unrelated", "unrelated")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").
		mustExit(t, 1, "RED", "internal/foo", "41 -> 41", "never waived")
}

func TestAbsR3ARepairWithPlusTwoIsOverWaived(t *testing.T) {
	r := raceRepo(t)
	mergeBranch(r, "b")
	r.git("checkout", "-q", "-b", "repair", "main")
	r.commit("repair\n\nDirgate-Growth: internal/foo +2 #6054 concurrent merges of a and b")
	r.git("checkout", "-q", "main")
	r.git("merge", "-q", "--no-ff", "-m", "Merge repair", "repair")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").
		mustExit(t, 1, "RED", "over-waived", "+2", "+1")
}

func TestAbsR4OneMoreFileWithPlusOneNeedsPlusTwo(t *testing.T) {
	r := raceRepo(t)
	mergeBranch(r, "b")
	r.git("checkout", "-q", "-b", "more", "main")
	r.addGoFiles(dirFoo, "m", 0, 1)
	r.commit(growthTrailer(1))
	r.git("checkout", "-q", "main")
	r.git("merge", "-q", "--no-ff", "-m", "Merge more", "more")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").
		mustExit(t, 1, "RED", "+2", "+1 from this change", "+1 already on base")
}

func TestAbsR5EditingAnExistingBaselineIsRedEvenWithATrailer(t *testing.T) {
	for _, edit := range []struct {
		name string
		do   func(r *scratchRepo)
	}{
		{"modify cap row", func(r *scratchRepo) { r.write(baselineCapPath, "internal/foo\t99\n") }},
		{"remove cap file", func(r *scratchRepo) { r.rm(baselineCapPath) }},
		{"modify naming file", func(r *scratchRepo) { r.write(baselineNamingPath, "internal/foo\tx.go\n") }},
		{"remove naming file", func(r *scratchRepo) { r.rm(baselineNamingPath) }},
	} {
		t.Run(edit.name, func(t *testing.T) {
			r := seedBase(t, 41)
			r.git("checkout", "-q", "-b", "feature")
			edit.do(r)
			r.commit("tamper with a baseline\n\nDirgate-Growth: internal/foo +1 #6054 trying to waive it")
			r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "baseline", "write-once")
		})
	}
}

func TestAbsR7UnbaselinedCollisionIsRedOnEveryEvent(t *testing.T) {
	r := newScratchRepo(t)
	r.write("go/internal/p/doc.go", "package p\n")
	r.write("go/internal/p/bar/a.go", "package bar\n")
	r.commit("base")
	r.baseline()
	r.write("go/internal/p/bar_x.go", "package p\n")
	r.commit("a collision lands after the baseline, with no row and no nolint")
	for _, event := range []string{"push", "schedule", "workflow_dispatch", "merge_group", ""} {
		r.runEnv(map[string]string{"GITHUB_EVENT_NAME": event}, "ci").
			mustExit(t, 1, "RED", "bar_x.go")
	}
}

func TestAbsR8SquashCommitBodyCarriesANonTipTrailer(t *testing.T) {
	r := seedBase(t, 41)
	r.addGoFiles(dirFoo, "s", 0, 1)
	r.commit("squash of #1\n\n* first commit\nDirgate-Growth: internal/foo +1 #6054 squash body line\n* second commit")
	r.runEnv(pushEvent, "ci").mustExit(t, 0)

	r2 := seedBase(t, 41)
	r2.addGoFiles(dirFoo, "s", 0, 1)
	r2.commit("squash of #1\n\n* first commit\n* second commit")
	r2.runEnv(pushEvent, "ci").mustExit(t, 1, "RED", "41 -> 42")
}

func TestAbsR9OldPRBranchUnshallowsWithoutACliff(t *testing.T) {
	src := seedBase(t, 41)
	src.git("config", "uploadpack.allowReachableSHA1InWant", "true")
	src.git("config", "uploadpack.allowFilter", "true")
	main := src.git("rev-parse", "HEAD")
	// A PR branch of 1200 commits; the waiver is on the oldest.
	src.fastImport(chainCommits("refs/heads/pr", main, 1200, "go/internal/foo/g000.go",
		"grow\n\nDirgate-Growth: internal/foo +1 #6054 oldest commit of a long branch"))
	src.git("checkout", "-q", "-b", "mergeref", "main")
	src.git("merge", "-q", "--no-ff", "-m", "Merge pr", "pr")
	dst := shallowClone(t, src)
	dst.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").mustExit(t, 0)

	dst2 := shallowClone(t, src)
	dst2.git("remote", "set-url", "origin", "file:///nonexistent/dirgate-remote")
	dst2.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").mustExit(t, 2, "unshallow")
}

func TestAbsR10BaselineBootstrapIsGreenButEditingItLaterIsRed(t *testing.T) {
	r := newScratchRepo(t)
	r.addGoFiles(dirFoo, "f", 0, 45)
	r.commit("base without baselines")
	r.git("checkout", "-q", "-b", "landing")
	r.write(baselineCapPath, "internal/foo\t45\n")
	r.write(baselineNamingPath, "")
	r.commit("land the baselines")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestAbsBaselineGeneratorIsDeterministicAndSorted(t *testing.T) {
	r := newScratchRepo(t)
	r.addGoFiles("internal/zeta", "z", 0, 41)
	r.addGoFiles("internal/alpha", "a", 0, 42)
	r.addGoFiles("internal/small", "s", 0, 40)
	r.write("go/internal/p/bar/a.go", "package bar\n")
	r.write("go/internal/p/bar_x.go", "package p\n")
	r.write("go/internal/p/bar_y.go", "package p\n")
	r.commit("base")
	dir1, dir2 := t.TempDir(), t.TempDir()
	r.run("-base-ref", "HEAD", "-out", dir1, "baseline").mustExit(t, 0)
	r.run("-base-ref", "HEAD", "-out", dir2, "baseline").mustExit(t, 0)
	for _, f := range []string{"baseline-cap.tsv", "baseline-naming.tsv"} {
		a, b := readFile(t, dir1+"/"+f), readFile(t, dir2+"/"+f)
		if a != b {
			t.Fatalf("%s differs between runs", f)
		}
	}
	capRows := dataRows(readFile(t, dir1+"/baseline-cap.tsv"))
	if got, want := strings.Join(capRows, ","), "internal/alpha\t42,internal/zeta\t41"; got != want {
		t.Fatalf("cap rows = %q, want %q", got, want)
	}
	namingRows := dataRows(readFile(t, dir1+"/baseline-naming.tsv"))
	if got, want := strings.Join(namingRows, ","), "internal/p\tbar_x.go,internal/p\tbar_y.go"; got != want {
		t.Fatalf("naming rows = %q, want %q", got, want)
	}
}

// ---- GREEN ----

func TestAbsG1RepairByOneCommitPRIsGreenEverywhere(t *testing.T) {
	r := raceRepo(t)
	mergeBranch(r, "b")
	r.git("checkout", "-q", "-b", "repair", "main")
	r.commit("repair\n\nDirgate-Growth: internal/foo +1 #6054 concurrent merges of a and b")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").
		mustExit(t, 2) // not a merge commit: the PR run needs the merge ref
	r.git("checkout", "-q", "main")
	r.git("merge", "-q", "--no-ff", "-m", "Merge repair", "repair")
	r.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").mustExit(t, 0)
	r.runEnv(pushEvent, "ci").mustExit(t, 0)
	r.git("checkout", "-q", "-b", "later", "main")
	r.write("go/internal/elsewhere/x.go", "package elsewhere\n")
	r.commit("a following unrelated PR")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestAbsG2RepairByMovingOneFileOutIsGreen(t *testing.T) {
	r := raceRepo(t)
	mergeBranch(r, "b")
	r.git("checkout", "-q", "-b", "shrink", "main")
	r.rm("go/internal/foo/b000.go")
	r.commit("move one out")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestAbsG3TwoTrailerGrowthsMergedInEitherOrder(t *testing.T) {
	for _, order := range [][2]string{{"a", "b"}, {"b", "a"}} {
		r := seedBase(t, 41)
		for _, name := range []string{"a", "b"} {
			r.git("checkout", "-q", "-b", name, "main")
			r.addGoFiles(dirFoo, name, 0, 1)
			r.commit(growthTrailer(1))
		}
		mergeBranch(r, order[0])
		r.runEnv(pushEvent, "ci").mustExit(t, 0)
		mergeBranch(r, order[1])
		r.runEnv(pushEvent, "ci").mustExit(t, 0)
	}
}

func TestAbsG5BaselineHistoryShrinkAndRegrow(t *testing.T) {
	r := seedBase(t, 45) // baseline row 45
	r.git("checkout", "-q", "-b", "grow3", "main")
	r.addGoFiles(dirFoo, "w", 0, 3)
	r.commit(growthTrailer(3))
	mergeBranch(r, "grow3")
	r.git("checkout", "-q", "-b", "shrink", "main")
	for i := 0; i < 18; i++ {
		r.rm(fmt.Sprintf("go/%s/f%03d.go", dirFoo, i))
	}
	r.commit("shrink to 30")
	mergeBranch(r, "shrink")
	r.git("checkout", "-q", "-b", "regrow", "main")
	r.addGoFiles(dirFoo, "x", 0, 11)
	r.commit(growthTrailer(1))
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

func TestAbsG6WholeDirectoryRenameWithTheGrowthTrailer(t *testing.T) {
	r := newScratchRepo(t)
	r.addGoFiles("internal/old", "o", 0, 300)
	r.commit("base")
	r.baseline()
	r.git("checkout", "-q", "-b", "rename")
	r.git("mv", "go/internal/old", "go/internal/fresh")
	r.commit("rename the directory\n\nDirgate-Growth: internal/fresh +260 #6054 whole-directory rename")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

// ---- argument and trailer-validity pins ----

func TestFlagsAfterTheModeAreRejected(t *testing.T) {
	r := seedBase(t, 3)
	out := t.TempDir()
	r.run("-base-ref", "HEAD", "baseline", "-out", out).mustExit(t, 2, "flags go before the mode")
	r.run("-event", "push", "ci", "-head-ref", "HEAD").mustExit(t, 2, "flags go before the mode")
	r.run("-base-ref", "main", "pre-push", "extra").mustExit(t, 2, "flags go before the mode")
	// The pre-commit mode keeps taking staged file names after it.
	r.git("checkout", "-q", "-b", "feature")
	r.addGoFiles(dirFoo, "n", 0, 1)
	r.git("add", "-A")
	r.run("-base-ref", "main", "pre-commit", "go/internal/foo/n000.go").mustExit(t, 0)
}

// An invalid trailer in history must not count toward W: here the base holds
// one unwaived file over the cap and a malformed trailer that claims to waive it.
func TestInvalidTrailerInHistoryDoesNotCountTowardW(t *testing.T) {
	r := seedBase(t, 40)
	r.addGoFiles(dirFoo, "x", 0, 1)
	r.commit("grow\n\nDirgate-Growth: internal/foo +1 no issue reference here")
	r.git("checkout", "-q", "-b", "later")
	r.write("go/internal/elsewhere/x.go", "package elsewhere\n")
	r.commit("an unrelated change")
	r.git("checkout", "-q", "main")
	r.git("merge", "-q", "--no-ff", "-m", "Merge later", "later")
	r.runEnv(pushEvent, "ci").mustExit(t, 1, "RED", "41 -> 41", "never waived")
}

// An indented trailer is not a trailer: it neither waives growth in the range
// nor counts toward W.
func TestIndentedTrailerIsNotAccepted(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, "grow\n\n  Dirgate-Growth: internal/foo +1 #6054 indented, so not a trailer")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "needs +1")

	h := seedBase(t, 40)
	h.addGoFiles(dirFoo, "x", 0, 1)
	h.commit("grow\n\n\tDirgate-Growth: internal/foo +1 #6054 tab-indented")
	h.git("checkout", "-q", "-b", "later")
	h.write("go/internal/elsewhere/x.go", "package elsewhere\n")
	h.commit("an unrelated change")
	h.git("checkout", "-q", "main")
	h.git("merge", "-q", "--no-ff", "-m", "Merge later", "later")
	h.runEnv(pushEvent, "ci").mustExit(t, 1, "RED", "41 -> 41", "never waived")
}

// TestRevertOnMainKeepsTheWaivedGrowthInW pins the accepted residual slack: when
// growth that carried a trailer is reverted on main, the revert's push run
// exits 0 and W still holds the waived N, because W sums every commit
// reachable from the base and nothing subtracts from it. The invariant is not
// airtight: that N lets the same amount of future unwaived growth pass the
// absolute check.
func TestRevertOnMainKeepsTheWaivedGrowthInW(t *testing.T) {
	r := seedBase(t, 41)
	r.git("checkout", "-q", "-b", "grow", "main")
	r.addGoFiles(dirFoo, "g", 0, 1)
	r.commit(growthTrailer(1))
	mergeBranch(r, "grow")
	r.runEnv(pushEvent, "ci").mustExit(t, 0, "W=0") // the merge itself: its trailer is in the range, not yet in W

	r.rm("go/internal/foo/g000.go")
	r.commit("revert the growth on main")
	res := r.runEnv(pushEvent, "ci")
	res.mustExit(t, 0, "W=1", "0 RED")
}
