package collector

import (
	"testing"
	"time"
)

func TestBuildStreamingGenerationRecordsSourceCommitSHA(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 13, 5, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.HeadCommitSHA = "0123456789abcdef0123456789abcdef01234567"

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)

	if got, want := collected.Generation.SourceCommitSHA, snapshot.HeadCommitSHA; got != want {
		t.Fatalf("generation SourceCommitSHA = %q, want %q", got, want)
	}
	drainFactChannel(collected.Facts)
}

func TestBuildStreamingGenerationOmitsSourceCommitSHAWhenUnknown(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 13, 5, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)

	if collected.Generation.SourceCommitSHA != "" {
		t.Fatalf("generation SourceCommitSHA = %q, want empty", collected.Generation.SourceCommitSHA)
	}
	drainFactChannel(collected.Facts)
}
