// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
)

// TestSnapshotRepositoryKeepsGitTrackedFileDespiteGitignoreMatch is the
// issue #5591 repro: a force-committed (`git add -f`) terraform.tfstate that
// matches the repo's own `*.tfstate` .gitignore rule stays tracked by git and
// must still surface as a TerraformStateCandidate. Before the fix, discovery
// applied .gitignore as a pure pattern filter with no knowledge of git's
// tracked set, silently dropping the file (0 terraform_state_candidate facts,
// drift-detection capability unreachable). scratch.tfstate is left genuinely
// untracked so the same test proves git semantics still apply both ways: an
// untracked file matching the same rule is still skipped.
func TestSnapshotRepositoryKeepsGitTrackedFileDespiteGitignoreMatch(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init", "-b", "main")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")

	stateBody := `{"version":4,"serial":7,"lineage":"00000000-0000-0000-0000-000000000000","resources":[]}`
	writeFile(t, repoRoot, ".gitignore", "*.tfstate\n")
	writeFile(t, repoRoot, "terraform.tfstate", stateBody)
	writeFile(t, repoRoot, "scratch.tfstate", stateBody)

	runGit(t, repoRoot, "add", ".gitignore")
	runGit(t, repoRoot, "add", "-f", "terraform.tfstate")
	runGit(t, repoRoot, "commit", "-m", "initial")
	// scratch.tfstate is intentionally never `git add`ed: it stays
	// untracked and must remain gitignored.

	snapshotter := NativeRepositorySnapshotter{}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	relPaths := make([]string, 0, len(snapshot.TerraformStateCandidates))
	for _, candidate := range snapshot.TerraformStateCandidates {
		relPaths = append(relPaths, candidate.RelativePath)
	}
	if got, want := relPaths, []string{"terraform.tfstate"}; !collectorStringSlicesEqual(got, want) {
		t.Fatalf("TerraformStateCandidates relative paths = %v, want %v (tracked-ignored file must survive discovery, untracked one must not)", got, want)
	}

	if snapshot.DiscoveryAdvisory == nil {
		t.Fatal("DiscoveryAdvisory = nil, want non-nil")
	}
	if got, want := snapshot.DiscoveryAdvisory.SkipBreakdown.FilesGitignore, 1; got != want {
		t.Fatalf("SkipBreakdown.FilesGitignore = %d, want %d (only untracked scratch.tfstate; terraform.tfstate is tracked)", got, want)
	}
}

// TestSnapshotRepositoryNonGitDirectoryUnaffectedByTrackedResolver proves
// #5591's tracked-file exception degrades safely: when RepoPath is not a git
// checkout at all (git ls-files fails, resolver reports ok=false), gitignore
// filtering behaves exactly as it did before #5591 for every matching file.
func TestSnapshotRepositoryNonGitDirectoryUnaffectedByTrackedResolver(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	stateBody := `{"version":4,"serial":7,"lineage":"00000000-0000-0000-0000-000000000000","resources":[]}`
	writeFile(t, repoRoot, ".gitignore", "*.tfstate\n")
	writeFile(t, repoRoot, "terraform.tfstate", stateBody)

	snapshotter := NativeRepositorySnapshotter{}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got := len(snapshot.TerraformStateCandidates); got != 0 {
		t.Fatalf("TerraformStateCandidates count = %d, want 0 in a non-git directory (gitignore applies with no tracked-file exception)", got)
	}
	if snapshot.DiscoveryAdvisory == nil {
		t.Fatal("DiscoveryAdvisory = nil, want non-nil")
	}
	if got, want := snapshot.DiscoveryAdvisory.SkipBreakdown.FilesGitignore, 1; got != want {
		t.Fatalf("SkipBreakdown.FilesGitignore = %d, want %d", got, want)
	}
}

// TestSnapshotRepositoryStillHonorsEshuIgnoreForTrackedFileAndRecordsSkip
// proves the #5591 rule is scoped to .gitignore only: .eshuignore remains
// the operator's own opt-out and still skips a file git tracks, but that
// skip must stay individually visible via the new
// SkipBreakdown.TrackedFilesEshuIgnore counter rather than disappearing into
// the existing aggregate FilesEshuIgnore count.
func TestSnapshotRepositoryStillHonorsEshuIgnoreForTrackedFileAndRecordsSkip(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init", "-b", "main")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "Test")

	stateBody := `{"version":4,"serial":7,"lineage":"00000000-0000-0000-0000-000000000000","resources":[]}`
	writeFile(t, repoRoot, ".eshuignore", "*.tfstate\n")
	writeFile(t, repoRoot, "terraform.tfstate", stateBody)
	runGit(t, repoRoot, "add", "-f", "terraform.tfstate")
	runGit(t, repoRoot, "commit", "-m", "initial")

	snapshotter := NativeRepositorySnapshotter{}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got := len(snapshot.TerraformStateCandidates); got != 0 {
		t.Fatalf("TerraformStateCandidates count = %d, want 0 (.eshuignore still skips a tracked file)", got)
	}
	if snapshot.DiscoveryAdvisory == nil {
		t.Fatal("DiscoveryAdvisory = nil, want non-nil")
	}
	if got, want := snapshot.DiscoveryAdvisory.SkipBreakdown.FilesEshuIgnore, 1; got != want {
		t.Fatalf("SkipBreakdown.FilesEshuIgnore = %d, want %d", got, want)
	}
	if got, want := snapshot.DiscoveryAdvisory.SkipBreakdown.TrackedFilesEshuIgnore, 1; got != want {
		t.Fatalf("SkipBreakdown.TrackedFilesEshuIgnore = %d, want %d (tracked skip must be individually observable)", got, want)
	}
}
