// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestStreamFactsEmitsCodeownersOwnershipFromContentFileMetas proves the
// content-loop hook wiring: given a snapshot whose ContentFileMetas already
// contains the winning ".github/CODEOWNERS" entry (the two-phase re-read
// path), streamFacts emits one codeowners.ownership fact per rule line, with
// correct pattern/owners/order_index, and does not touch reducer/query
// surfaces.
func TestStreamFactsEmitsCodeownersOwnershipFromContentFileMetas(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	const codeownersPath = ".github/CODEOWNERS"
	codeownersBody := strings.Join([]string{
		"*.go @octocat",
		"/services/payments/ @org/payments-team",
		"",
	}, "\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, codeownersPath), codeownersBody)

	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.July, 21, 9, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: codeownersPath,
			Digest:       "sha1:codeowners",
			Language:     "codeowners",
			ArtifactType: "codeowners",
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

	ownershipFacts := factsByKind(envelopes, facts.CodeownersOwnershipFactKind)
	if got, want := len(ownershipFacts), 2; got != want {
		t.Fatalf("codeowners.ownership fact count = %d, want %d", got, want)
	}

	for _, envelope := range ownershipFacts {
		if got, want := envelope.SchemaVersion, facts.CodeownersSchemaVersionV1; got != want {
			t.Errorf("SchemaVersion = %q, want %q", got, want)
		}
		if got, want := envelope.Payload["source_path"], codeownersPath; got != want {
			t.Errorf("Payload[source_path] = %#v, want %#v", got, want)
		}
		if got, want := envelope.Payload["repo_id"], repo.ID; got != want {
			t.Errorf("Payload[repo_id] = %#v, want %#v", got, want)
		}
	}

	first, second := ownershipFacts[0], ownershipFacts[1]
	if got, want := first.Payload["pattern"], "*.go"; got != want {
		t.Fatalf("first Payload[pattern] = %#v, want %#v", got, want)
	}
	if got, want := first.Payload["order_index"], float64(0); got != want {
		t.Fatalf("first Payload[order_index] = %#v, want %#v", got, want)
	}
	firstOwners, _ := first.Payload["owners"].([]any)
	if len(firstOwners) != 1 || firstOwners[0] != "@octocat" {
		t.Fatalf("first Payload[owners] = %#v, want [@octocat]", first.Payload["owners"])
	}
	if got, want := second.Payload["pattern"], "/services/payments/"; got != want {
		t.Fatalf("second Payload[pattern] = %#v, want %#v", got, want)
	}
	if got, want := second.Payload["order_index"], float64(1); got != want {
		t.Fatalf("second Payload[order_index] = %#v, want %#v", got, want)
	}
}

// TestStreamFactsEmitsCodeownersOwnershipFromLegacyContentFiles proves the
// same hook fires on the legacy (ContentFiles, bodies-already-in-memory) path.
func TestStreamFactsEmitsCodeownersOwnershipFromLegacyContentFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.July, 21, 9, 30, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFiles: []ContentFileSnapshot{{
			RelativePath: "CODEOWNERS",
			Body:         "*.md @writer\n",
			Digest:       "sha1:codeowners-legacy",
			Language:     "codeowners",
		}},
	}

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	ownershipFacts := factsByKind(envelopes, facts.CodeownersOwnershipFactKind)
	if got, want := len(ownershipFacts), 1; got != want {
		t.Fatalf("codeowners.ownership fact count = %d, want %d", got, want)
	}
	if got, want := ownershipFacts[0].Payload["source_path"], "CODEOWNERS"; got != want {
		t.Fatalf("Payload[source_path] = %#v, want %#v", got, want)
	}
}

// TestStreamFactsCodeownersPrecedenceGithubWinsOverRoot proves precedence:
// when both ".github/CODEOWNERS" and root "CODEOWNERS" are present in the
// same snapshot, only the ".github" file's rules are emitted as ownership
// facts (root's rules are not), even though both still produce their own
// generic content facts.
func TestStreamFactsCodeownersPrecedenceGithubWinsOverRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoRoot)
	observedAt := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 2,
		ContentFileMetas: []ContentFileMeta{
			{RelativePath: ".github/CODEOWNERS", Digest: "sha1:github", Language: "codeowners"},
			{RelativePath: "CODEOWNERS", Digest: "sha1:root", Language: "codeowners"},
		},
	}
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".github", "CODEOWNERS"), "*.go @github-owner\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "CODEOWNERS"), "*.go @root-owner\n")

	collected := buildStreamingGeneration(repoRoot, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("FactCount = %d, want emitted fact count %d", got, want)
	}

	// Both files still produce their own generic content fact.
	if contentFacts := factsByKind(envelopes, "content"); len(contentFacts) != 2 {
		t.Fatalf("content fact count = %d, want 2", len(contentFacts))
	}

	ownershipFacts := factsByKind(envelopes, facts.CodeownersOwnershipFactKind)
	if got, want := len(ownershipFacts), 1; got != want {
		t.Fatalf("codeowners.ownership fact count = %d, want %d (only .github wins)", got, want)
	}
	if got, want := ownershipFacts[0].Payload["source_path"], ".github/CODEOWNERS"; got != want {
		t.Fatalf("Payload[source_path] = %#v, want %#v", got, want)
	}
	owners, _ := ownershipFacts[0].Payload["owners"].([]any)
	if len(owners) != 1 || owners[0] != "@github-owner" {
		t.Fatalf("Payload[owners] = %#v, want [@github-owner]", ownershipFacts[0].Payload["owners"])
	}
}

// TestStreamFactsEmitsNoCodeownersFactsWhenAbsent proves the hook is a no-op
// when no CODEOWNERS candidate is present.
func TestStreamFactsEmitsNoCodeownersFactsWhenAbsent(t *testing.T) {
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
	if ownershipFacts := factsByKind(envelopes, facts.CodeownersOwnershipFactKind); len(ownershipFacts) != 0 {
		t.Fatalf("codeowners.ownership fact count = %d, want 0", len(ownershipFacts))
	}
}

// TestNativeRepositorySnapshotterDiscoversCodeownersCandidates proves the
// real discovery gap: a repository whose only files are the three recognized
// CODEOWNERS locations plus one ordinary source file reaches
// snapshot.ContentFileMetas for every CODEOWNERS candidate (discovery admits
// them despite having no file extension, and they are diverted around the
// language-parser pipeline rather than silently dropped for lacking a
// registered parser.Definition).
func TestNativeRepositorySnapshotterDiscoversCodeownersCandidates(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "main.go"), "package main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".github", "CODEOWNERS"), "*.go @github-owner\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "CODEOWNERS"), "*.go @root-owner\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "docs", "CODEOWNERS"), "*.go @docs-owner\n")

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

	wantPaths := map[string]bool{
		".github/CODEOWNERS": false,
		"CODEOWNERS":         false,
		"docs/CODEOWNERS":    false,
	}
	for _, meta := range got.ContentFileMetas {
		if _, tracked := wantPaths[meta.RelativePath]; tracked {
			wantPaths[meta.RelativePath] = true
		}
	}
	for path, found := range wantPaths {
		if !found {
			t.Errorf("ContentFileMetas missing %q; got=%#v", path, got.ContentFileMetas)
		}
	}
}

// TestSnapshotRepositoryWithOnlyCodeownersSurvivesZeroParsedFiles proves the
// edge case where a repository's only file is a CODEOWNERS candidate: the
// zero-remaining-files early return in SnapshotRepository must still carry
// the extracted CODEOWNERS meta, mirroring how TerraformStateCandidates
// already survives that same early return.
func TestSnapshotRepositoryWithOnlyCodeownersSurvivesZeroParsedFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".github", "CODEOWNERS"), "*.go @octocat\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	snapshotter := NativeRepositorySnapshotter{}

	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: resolvedRepoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}
	if len(got.ContentFileMetas) != 1 || got.ContentFileMetas[0].RelativePath != ".github/CODEOWNERS" {
		t.Fatalf("ContentFileMetas = %#v, want one entry for .github/CODEOWNERS", got.ContentFileMetas)
	}
}

// TestNativeRepositorySnapshotterDeltaDeleteGithubCandidateReResolvesRootWinner
// proves the collector-side fix for issue #5419 Bug 2's empty-graph
// transition. CODEOWNERS winner-resolution is whole-repo (one winner among
// the three known candidate locations), but a delta snapshot narrows
// fileSet.Files to the delta's changed targets only (see
// resolveNativeSnapshotFileSetForTargets). Before the fix, deleting
// ".github/CODEOWNERS" while root "CODEOWNERS" stayed unchanged left NEITHER
// file in that narrowed set: the deleted file is gone from disk, and the
// unchanged root file was never a changed target. That produced zero
// CODEOWNERS candidates, so no fallback facts were emitted and the winner
// silently disappeared instead of falling back to root. The fix re-reads all
// three known candidate locations directly from repoPath whenever the delta
// touches any one of them, so root is discovered as the new winner.
func TestNativeRepositorySnapshotterDeltaDeleteGithubCandidateReResolvesRootWinner(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "main.go"), "package main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "CODEOWNERS"), "*.go @root-owner\n")
	// ".github/CODEOWNERS" is deliberately absent: this reproduces the
	// post-delta disk state right after the delete landed.

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	snapshotter := NativeRepositorySnapshotter{}

	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath: resolvedRepoRoot,
		Delta:    true,
		// "main.go" is an unrelated changed file: it forces the delta path
		// (resolveNativeSnapshotFileSetForTargets narrows fileSet.Files to
		// just this target), reproducing the real failure mode where the
		// narrowed file set hides an unrelated, unchanged CODEOWNERS
		// candidate.
		FileTargets:          []string{filepath.Join(resolvedRepoRoot, "main.go")},
		DeletedRelativePaths: []string{".github/CODEOWNERS"},
	})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	var rootMetaFound bool
	for _, meta := range snapshot.ContentFileMetas {
		if meta.RelativePath == ".github/CODEOWNERS" {
			t.Fatalf("ContentFileMetas unexpectedly contains deleted .github/CODEOWNERS: %#v", snapshot.ContentFileMetas)
		}
		if meta.RelativePath == "CODEOWNERS" {
			rootMetaFound = true
		}
	}
	if !rootMetaFound {
		t.Fatalf("ContentFileMetas missing root CODEOWNERS fallback; got=%#v", snapshot.ContentFileMetas)
	}

	repo := testCollectorRepositoryMetadata(resolvedRepoRoot)
	collected := buildStreamingGeneration(resolvedRepoRoot, repo, "run-delta", time.Now().UTC(), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	ownershipFacts := factsByKind(envelopes, facts.CodeownersOwnershipFactKind)
	if len(ownershipFacts) != 1 {
		t.Fatalf("codeowners.ownership fact count = %d, want 1 (root fallback); facts=%#v", len(ownershipFacts), ownershipFacts)
	}
	if got, want := ownershipFacts[0].Payload["source_path"], "CODEOWNERS"; got != want {
		t.Fatalf("Payload[source_path] = %#v, want %#v", got, want)
	}
	owners, _ := ownershipFacts[0].Payload["owners"].([]any)
	if len(owners) != 1 || owners[0] != "@root-owner" {
		t.Fatalf("Payload[owners] = %#v, want [@root-owner]", ownershipFacts[0].Payload["owners"])
	}
}

// TestNativeRepositorySnapshotterDeltaAddGithubCandidateWinsOverRoot is the
// companion collector-side proof for issue #5419 Bug 2's other transition: a
// repository already had a root "CODEOWNERS" file (previously the winner),
// and a delta adds ".github/CODEOWNERS". Since ".github/CODEOWNERS" is itself
// the delta's changed target, the collector already discovered it as a
// candidate before this fix; this test locks in that the whole-repo re-read
// this fix adds does not regress precedence — ".github" still wins and root's
// rules are NOT also emitted (the reducer-side whole-repo retract, proven
// separately, is what keeps root's stale edges from unioning with the new
// ".github" edges in the graph).
func TestNativeRepositorySnapshotterDeltaAddGithubCandidateWinsOverRoot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "CODEOWNERS"), "*.go @root-owner\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".github", "CODEOWNERS"), "*.go @github-owner\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	snapshotter := NativeRepositorySnapshotter{}

	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath: resolvedRepoRoot,
		Delta:    true,
		FileTargets: []string{
			filepath.Join(resolvedRepoRoot, ".github", "CODEOWNERS"),
		},
	})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	repo := testCollectorRepositoryMetadata(resolvedRepoRoot)
	collected := buildStreamingGeneration(resolvedRepoRoot, repo, "run-delta", time.Now().UTC(), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	ownershipFacts := factsByKind(envelopes, facts.CodeownersOwnershipFactKind)
	if len(ownershipFacts) != 1 {
		t.Fatalf("codeowners.ownership fact count = %d, want 1 (.github only, no union); facts=%#v", len(ownershipFacts), ownershipFacts)
	}
	if got, want := ownershipFacts[0].Payload["source_path"], ".github/CODEOWNERS"; got != want {
		t.Fatalf("Payload[source_path] = %#v, want %#v", got, want)
	}
	owners, _ := ownershipFacts[0].Payload["owners"].([]any)
	if len(owners) != 1 || owners[0] != "@github-owner" {
		t.Fatalf("Payload[owners] = %#v, want [@github-owner]", ownershipFacts[0].Payload["owners"])
	}
}
