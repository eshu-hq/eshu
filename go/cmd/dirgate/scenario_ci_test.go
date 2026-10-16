// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"testing"
)

// mergeRefSource builds the shape CI checks out: a merge commit on branch
// mergeref whose first parent is main (which moved on after the PR branched)
// and whose second parent is a three-commit PR. The waiver for dirFoo's +1
// sits on the FIRST PR commit, not the tip.
func mergeRefSource(t *testing.T) *scratchRepo {
	t.Helper()
	src := seedBase(t, 41)
	src.git("config", "uploadpack.allowReachableSHA1InWant", "true")
	src.git("config", "uploadpack.allowFilter", "true")
	src.git("checkout", "-q", "-b", "pr")
	src.addGoFiles(dirFoo, "g", 0, 1)
	src.commit(growthTrailer(1))
	src.write("go/internal/other/a.go", "package other\n")
	src.commit("second PR commit")
	src.write("go/internal/other/b.go", "package other\n")
	src.commit("third PR commit")
	src.git("checkout", "-q", "main")
	src.write("go/internal/third/a.go", "package third\n")
	src.commit("main moves on")
	src.git("checkout", "-q", "-b", "mergeref")
	src.git("merge", "-q", "--no-ff", "-m", "Merge pr", "pr")
	return src
}

// shallowClone clones src at depth 2 on branch mergeref, the way
// actions/checkout does with fetch-depth: 2.
func shallowClone(t *testing.T, src *scratchRepo) *scratchRepo {
	t.Helper()
	dst := &scratchRepo{t: t, root: t.TempDir() + "/clone"}
	src.git("clone", "-q", "--depth=2", "--no-local", "--branch", "mergeref", "file://"+src.root, dst.root)
	if got := dst.git("rev-parse", "--is-shallow-repository"); got != "true" {
		t.Fatalf("setup: clone is not shallow (%s)", got)
	}
	return dst
}

// TestShallowCIFetchSeesTrailersOnOlderPRCommits pins the CI shape: the merge
// ref is checked out shallow (depth 2), which hides every PR commit but the
// tip, so the gate has to unshallow history itself before it can honour a waiver
// on an older PR commit.
func TestShallowCIFetchSeesTrailersOnOlderPRCommits(t *testing.T) {
	dst := shallowClone(t, mergeRefSource(t))
	dst.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").mustExit(t, 0)
}

// TestShallowEveryEventUnshallowsOnce: push, schedule, workflow_dispatch and an
// empty event enforce too, and each reads the waiver on the older PR commit
// after one unshallow fetch.
func TestShallowEveryEventUnshallowsOnce(t *testing.T) {
	for _, event := range []string{"push", "schedule", "workflow_dispatch", "", "merge_group"} {
		dst := shallowClone(t, mergeRefSource(t))
		dst.runEnv(map[string]string{"GITHUB_EVENT_NAME": event}, "ci").mustExit(t, 0)
		if got := dst.git("rev-parse", "--is-shallow-repository"); got != "false" {
			t.Fatalf("event %q: checkout still shallow (%s)", event, got)
		}
	}
}

func TestUnshallowFailureExitsTwo(t *testing.T) {
	dst := shallowClone(t, mergeRefSource(t))
	dst.git("remote", "set-url", "origin", "file:///nonexistent/dirgate-remote")
	dst.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").
		mustExit(t, 2, "unshallow")
}

func TestUnshallowWithoutAnOriginRemoteExitsTwo(t *testing.T) {
	dst := shallowClone(t, mergeRefSource(t))
	dst.git("remote", "remove", "origin")
	dst.runEnv(map[string]string{"GITHUB_EVENT_NAME": "pull_request"}, "ci").
		mustExit(t, 2, "unshallow")
}

// TestPrePushUsesTheMergedTreeNotTheMergeBase kills a mutant that always takes
// the merge-base fallback. Main and the PR both delete f000..f002; main also
// deletes f003 and f004; the PR adds one file. Merged onto main (45 files) the
// result is 46, which grew past the base. Against the merge base alone the PR
// reads 50 -> 48, a shrink.
func TestPrePushUsesTheMergedTreeNotTheMergeBase(t *testing.T) {
	r := seedBase(t, 50)
	r.git("checkout", "-q", "-b", "feature")
	for i := 0; i < 3; i++ {
		r.rm(fmt.Sprintf("go/%s/f%03d.go", dirFoo, i))
	}
	// Distinct content: identical files would pair with the deletions as renames
	// and turn the merge into a rename/delete conflict.
	r.write("go/internal/foo/g000.go", "package g\n// content no deleted file shares\n")
	r.commit("feature drops three and adds one")
	r.git("checkout", "-q", "main")
	for i := 0; i < 5; i++ {
		r.rm(fmt.Sprintf("go/%s/f%03d.go", dirFoo, i))
	}
	r.commit("main drops five")
	r.git("checkout", "-q", "feature")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "45 -> 46")
}

// TestGeneratedAndVendoredDirectoriesAreNotCounted kills a mutant that stops
// skipping `generated`, and covers the other skipped segments.
func TestGeneratedAndVendoredDirectoriesAreNotCounted(t *testing.T) {
	r := seedBase(t, 3)
	r.git("checkout", "-q", "-b", "feature")
	for _, dir := range []string{"internal/foo/generated", "internal/foo/vendor", "internal/foo/testdata", "internal/foo/.hidden"} {
		r.addGoFiles(dir, "x", 0, 45)
	}
	r.commit("fixtures and generated code")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0, "0 RED")
}

// TestGrepUserConfigCannotBreakMarkerParsing: grep.column=true makes git grep
// print an extra column field, which used to hide every marker.
func TestGrepUserConfigCannotBreakMarkerParsing(t *testing.T) {
	r := seedBase(t, 3)
	r.git("config", "grep.column", "true")
	r.git("config", "color.grep", "always")
	r.write("go/internal/foo/doc.go", "package foo //nolint:dirgate // old cap escape\n")
	r.commit("stale marker")
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/foo/other.go", "package foo\n")
	r.commit("feature")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0, "NOTE", "stale", "go/internal/foo/doc.go")
}

// TestGrepUserConfigCannotHideANamingEscape: the same config must not stop a
// justified naming escape from being honoured.
func TestGrepUserConfigCannotHideANamingEscape(t *testing.T) {
	r := newScratchRepo(t)
	r.git("config", "grep.column", "true")
	r.write("go/internal/p/bar/a.go", "package bar\n")
	r.commit("base")
	r.git("checkout", "-q", "-b", "feature")
	r.write("go/internal/p/bar_x.go", "package p //nolint:dirgate // legacy wire name\n")
	r.commit("escaped")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 0)
}

// TestRevertedGrowthLeavesAnUnusedTrailer pins what the docs promise: the
// trailer stays in the reverted commit's message, so the range is RED until the
// history is amended or rebased.
func TestRevertedGrowthLeavesAnUnusedTrailer(t *testing.T) {
	r := seedBase(t, 41)
	branchWithGrowth(r, 1, growthTrailer(1))
	r.git("revert", "--no-edit", "HEAD")
	r.run("-base-ref", "main", "pre-push").mustExit(t, 1, "RED", "unused trailer")
}
