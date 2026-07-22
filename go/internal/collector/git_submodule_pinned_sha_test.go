// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fakeGitlinkSHA is a syntactically valid (40 lowercase hex characters)
// commit SHA used to hand-craft a gitlink tree entry via `git update-index
// --cacheinfo`. `git ls-tree` never validates that a gitlink SHA resolves to
// a real committed object — it is opaque tree data — so a fake SHA proves
// the mode-160000 parsing exactly the same way a real submodule commit
// would, without any network clone.
const fakeGitlinkSHA = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

// TestGitSubmoduleGitlinkSHAResolvesGitlinkEntry proves the core case: a
// path recorded in HEAD's tree as a mode-160000 gitlink resolves to its
// commit SHA. It also proves the uninitialized-submodule edge case in the
// same assertion — update-index --cacheinfo never touches the working
// directory, so lib/foo has no on-disk directory at all, yet the gitlink
// still resolves because ls-tree reads the committed TREE, not the working
// tree.
func TestGitSubmoduleGitlinkSHAResolvesGitlinkEntry(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")
	writeGitFile(t, filepath.Join(repoPath, "README.md"), "# root\n")
	runGit(t, repoPath, "add", "README.md")
	runGit(t, repoPath, "update-index", "--add", "--cacheinfo", "160000,"+fakeGitlinkSHA+",lib/foo")
	runGit(t, repoPath, "commit", "-m", "add gitlink")

	if _, err := os.Stat(filepath.Join(repoPath, "lib", "foo")); !os.IsNotExist(err) {
		t.Fatalf("lib/foo must not exist on disk (uninitialized submodule); Stat err = %v", err)
	}

	got := gitSubmoduleGitlinkSHA(context.Background(), repoPath, "lib/foo")
	if got == nil {
		t.Fatal("gitSubmoduleGitlinkSHA() = nil, want the gitlink SHA")
	}
	if *got != fakeGitlinkSHA {
		t.Fatalf("gitSubmoduleGitlinkSHA() = %q, want %q", *got, fakeGitlinkSHA)
	}
}

// TestGitSubmoduleGitlinkSHANilForRegularDirectory proves a committed
// regular directory at the declared path is NOT reported as a pin: its tree
// mode is 040000 (a tree), not 160000 (a gitlink), so the resolver must not
// guess a commit SHA for it.
func TestGitSubmoduleGitlinkSHANilForRegularDirectory(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")
	writeGitFile(t, filepath.Join(repoPath, "lib", "foo", "file.txt"), "not a submodule\n")
	runGit(t, repoPath, "add", "lib/foo/file.txt")
	runGit(t, repoPath, "commit", "-m", "regular directory")

	if got := gitSubmoduleGitlinkSHA(context.Background(), repoPath, "lib/foo"); got != nil {
		t.Fatalf("gitSubmoduleGitlinkSHA() = %q, want nil for a regular directory", *got)
	}
}

// TestGitSubmoduleGitlinkSHANilForMissingPath proves a ".gitmodules" entry
// declared but never `git submodule add`ed (no tree entry at all for that
// path) resolves to nil rather than an error.
func TestGitSubmoduleGitlinkSHANilForMissingPath(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.email", "test@example.com")
	runGit(t, repoPath, "config", "user.name", "Test")
	writeGitFile(t, filepath.Join(repoPath, "README.md"), "# root\n")
	runGit(t, repoPath, "add", "README.md")
	runGit(t, repoPath, "commit", "-m", "initial")

	if got := gitSubmoduleGitlinkSHA(context.Background(), repoPath, "lib/never-added"); got != nil {
		t.Fatalf("gitSubmoduleGitlinkSHA() = %q, want nil for a path with no tree entry", *got)
	}
}

// TestGitSubmoduleGitlinkSHANilForUnbornHEAD proves an unborn HEAD (a freshly
// initialized repository with zero commits) resolves to nil without a
// propagated error: `git ls-tree HEAD` fails because HEAD does not resolve
// to any commit yet.
func TestGitSubmoduleGitlinkSHANilForUnbornHEAD(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init")

	if got := gitSubmoduleGitlinkSHA(context.Background(), repoPath, "lib/foo"); got != nil {
		t.Fatalf("gitSubmoduleGitlinkSHA() = %q, want nil for an unborn HEAD", *got)
	}
}

// TestGitSubmoduleGitlinkSHANilForNonGitDirectory proves a repoPath that is
// not a git repository at all (the shape every pre-Phase-2b collector test
// fixture uses — a bare t.TempDir()) resolves to nil rather than panicking
// or propagating an error, matching gitCommitSHA's established fallback
// behavior for the same situation.
func TestGitSubmoduleGitlinkSHANilForNonGitDirectory(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()

	if got := gitSubmoduleGitlinkSHA(context.Background(), repoPath, "lib/foo"); got != nil {
		t.Fatalf("gitSubmoduleGitlinkSHA() = %q, want nil for a non-git directory", *got)
	}
}

// TestEmitSubmoduleFactsForCandidatesResolvesPinnedSHA is the
// failing-then-green proof for issue #5420 Phase 2b at the fact-stream
// level: a real git repository declares two submodules in ".gitmodules" —
// one with a committed gitlink, one declared but never added — and only the
// gitlink entry's emitted submodule.pin fact carries pinned_sha. Before the
// PinnedSHAResolver wiring, both facts left pinned_sha absent (Phase 2a's
// documented behavior); this test fails on that old behavior and passes
// once emitSubmoduleFactsForCandidates threads repoPath through to
// gitSubmoduleGitlinkSHA.
func TestEmitSubmoduleFactsForCandidatesResolvesPinnedSHA(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")

	gitmodulesBody := "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n" +
		"[submodule \"libnogit\"]\n\tpath = lib/nogit\n\turl = https://github.com/example/libnogit.git\n"
	writeGitFile(t, filepath.Join(repoRoot, ".gitmodules"), gitmodulesBody)
	runGit(t, repoRoot, "add", ".gitmodules")
	runGit(t, repoRoot, "update-index", "--add", "--cacheinfo", "160000,"+fakeGitlinkSHA+",lib/foo")
	runGit(t, repoRoot, "commit", "-m", "declare submodules")

	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.July, 21, 13, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFiles: []ContentFileSnapshot{{
			RelativePath: ".gitmodules",
			Body:         gitmodulesBody,
			Digest:       "sha1:gitmodules-pinned",
		}},
	}

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	pinFacts := factsByKind(envelopes, facts.SubmodulePinFactKind)
	if got, want := len(pinFacts), 2; got != want {
		t.Fatalf("submodule.pin fact count = %d, want %d", got, want)
	}

	var libfoo, libnogit facts.Envelope
	for _, envelope := range pinFacts {
		switch envelope.Payload["submodule_path"] {
		case "lib/foo":
			libfoo = envelope
		case "lib/nogit":
			libnogit = envelope
		}
	}

	pinnedSHA, ok := libfoo.Payload["pinned_sha"].(string)
	if !ok || pinnedSHA != fakeGitlinkSHA {
		t.Fatalf("libfoo Payload[pinned_sha] = %#v, want %q", libfoo.Payload["pinned_sha"], fakeGitlinkSHA)
	}

	if _, hasPinnedSHA := libnogit.Payload["pinned_sha"]; hasPinnedSHA {
		t.Fatalf("libnogit Payload unexpectedly includes pinned_sha (no gitlink was ever added): %#v", libnogit.Payload)
	}
}
