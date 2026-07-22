// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestStreamFactsEmitsSubmodulePinFromContentFileMetas proves the
// content-loop hook wiring: given a snapshot whose ContentFileMetas already
// contains the ".gitmodules" entry (the two-phase re-read path), streamFacts
// emits one submodule.pin fact per declared submodule, with correct
// path/url/resolved_repo_id, and pinned_sha always absent (Phase 2b).
func TestStreamFactsEmitsSubmodulePinFromContentFileMetas(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	const gitmodulesPath = ".gitmodules"
	gitmodulesBody := "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n" +
		"[submodule \"libbar\"]\n\tpath = lib/bar\n\turl = ../libbar.git\n"
	writeCollectorTestFile(t, filepath.Join(repoRoot, gitmodulesPath), gitmodulesBody)

	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: gitmodulesPath,
			Digest:       "sha1:gitmodules",
			Language:     "submodule",
			ArtifactType: "submodule",
		}},
	}

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	if contentFacts := factsByKind(envelopes, "content"); len(contentFacts) != 1 {
		t.Fatalf("content fact count = %d, want 1", len(contentFacts))
	}

	pinFacts := factsByKind(envelopes, facts.SubmodulePinFactKind)
	if got, want := len(pinFacts), 2; got != want {
		t.Fatalf("submodule.pin fact count = %d, want %d", got, want)
	}

	for _, envelope := range pinFacts {
		if got, want := envelope.SchemaVersion, facts.SubmoduleSchemaVersionV1; got != want {
			t.Errorf("SchemaVersion = %q, want %q", got, want)
		}
		if got, want := envelope.Payload["parent_repo_id"], repo.ID; got != want {
			t.Errorf("Payload[parent_repo_id] = %#v, want %#v", got, want)
		}
		if _, hasPinnedSHA := envelope.Payload["pinned_sha"]; hasPinnedSHA {
			t.Errorf("Payload unexpectedly includes pinned_sha (Phase 2b): %#v", envelope.Payload)
		}
	}

	var libfoo, libbar facts.Envelope
	for _, envelope := range pinFacts {
		switch envelope.Payload["submodule_path"] {
		case "lib/foo":
			libfoo = envelope
		case "lib/bar":
			libbar = envelope
		}
	}
	if got, want := libfoo.Payload["submodule_url"], "https://github.com/example/libfoo.git"; got != want {
		t.Fatalf("libfoo Payload[submodule_url] = %#v, want %#v", got, want)
	}
	resolvedRepoID, ok := libfoo.Payload["resolved_repo_id"].(string)
	if !ok || resolvedRepoID == "" {
		t.Fatalf("libfoo Payload[resolved_repo_id] = %#v, want a non-empty resolved id", libfoo.Payload["resolved_repo_id"])
	}

	if got, want := libbar.Payload["submodule_url"], "../libbar.git"; got != want {
		t.Fatalf("libbar Payload[submodule_url] = %#v, want %#v", got, want)
	}
	if _, hasResolved := libbar.Payload["resolved_repo_id"]; hasResolved {
		t.Fatalf("libbar Payload unexpectedly includes resolved_repo_id for a relative url: %#v", libbar.Payload)
	}
}

// TestStreamFactsEmitsSubmodulePinFromLegacyContentFiles proves the same
// hook fires on the legacy (ContentFiles, bodies-already-in-memory) path.
func TestStreamFactsEmitsSubmodulePinFromLegacyContentFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.July, 21, 9, 30, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFiles: []ContentFileSnapshot{{
			RelativePath: ".gitmodules",
			Body:         "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n",
			Digest:       "sha1:gitmodules-legacy",
			Language:     "submodule",
		}},
	}

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	pinFacts := factsByKind(envelopes, facts.SubmodulePinFactKind)
	if got, want := len(pinFacts), 1; got != want {
		t.Fatalf("submodule.pin fact count = %d, want %d", got, want)
	}
	if got, want := pinFacts[0].Payload["submodule_path"], "lib/foo"; got != want {
		t.Fatalf("Payload[submodule_path] = %#v, want %#v", got, want)
	}
}

// TestStreamFactsEmitsNoSubmoduleFactsWhenAbsent proves the hook is a no-op
// when no .gitmodules candidate is present.
func TestStreamFactsEmitsNoSubmoduleFactsWhenAbsent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.July, 21, 10, 30, 0, 0, time.UTC)
	const otherPath = "main.go"
	writeCollectorTestFile(t, filepath.Join(repoRoot, otherPath), "package main\n")
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: otherPath,
			Digest:       "sha1:main",
			Language:     "go",
		}},
	}

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}
	if pinFacts := factsByKind(envelopes, facts.SubmodulePinFactKind); len(pinFacts) != 0 {
		t.Fatalf("submodule.pin fact count = %d, want 0", len(pinFacts))
	}
}

// TestNativeRepositorySnapshotterDiscoversGitmodulesCandidate proves the real
// discovery gap: a repository whose files include a root ".gitmodules" plus
// one ordinary source file reaches snapshot.ContentFileMetas for the
// .gitmodules candidate (discovery admits it despite having no file
// extension, and it is diverted around the language-parser pipeline rather
// than silently dropped for lacking a registered parser.Definition).
func TestNativeRepositorySnapshotterDiscoversGitmodulesCandidate(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "main.go"), "package main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".gitmodules"), "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	now := time.Date(2026, time.July, 21, 11, 0, 0, 0, time.UTC)
	snapshotter := NativeRepositorySnapshotter{Now: func() time.Time { return now }}

	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	var found bool
	for _, meta := range got.ContentFileMetas {
		if meta.RelativePath == ".gitmodules" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ContentFileMetas missing .gitmodules; got=%#v", got.ContentFileMetas)
	}
}

// TestSnapshotRepositoryWithOnlyGitmodulesSurvivesZeroParsedFiles proves the
// edge case where a repository's only file is .gitmodules: the
// zero-remaining-files early return in SnapshotRepository must still carry
// the extracted .gitmodules meta, mirroring how TerraformStateCandidates
// already survives that same early return.
func TestSnapshotRepositoryWithOnlyGitmodulesSurvivesZeroParsedFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".gitmodules"), "[submodule \"libfoo\"]\n\tpath = lib/foo\n\turl = https://github.com/example/libfoo.git\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	snapshotter := NativeRepositorySnapshotter{}

	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	if len(got.ContentFileMetas) != 1 || got.ContentFileMetas[0].RelativePath != ".gitmodules" {
		t.Fatalf("ContentFileMetas = %#v, want one entry for .gitmodules", got.ContentFileMetas)
	}
}

// TestNoteSubmoduleCandidateIgnoresOtherPaths proves the accumulation helper
// only captures the exact ".gitmodules" path.
func TestNoteSubmoduleCandidateIgnoresOtherPaths(t *testing.T) {
	t.Parallel()

	candidates := map[string]string{}
	noteSubmoduleCandidate(candidates, "vendor/.gitmodules", "ignored body")
	noteSubmoduleCandidate(candidates, "README.md", "ignored body")
	if len(candidates) != 0 {
		t.Fatalf("candidates = %#v, want empty", candidates)
	}

	noteSubmoduleCandidate(candidates, ".gitmodules", "real body")
	if got, want := candidates[".gitmodules"], "real body"; got != want {
		t.Fatalf("candidates[.gitmodules] = %q, want %q", got, want)
	}
}
