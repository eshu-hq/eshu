// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildStreamingGenerationRecordsSourceCommitSHA(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 13, 5, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.HeadCommitSHA = "0123456789abcdef0123456789abcdef01234567"

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")

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

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")

	if collected.Generation.SourceCommitSHA != "" {
		t.Fatalf("generation SourceCommitSHA = %q, want empty", collected.Generation.SourceCommitSHA)
	}
	drainFactChannel(collected.Facts)
}

// TestBuildStreamingGenerationReconcileMarksRepositoryFact keeps the
// reconciliation marker on the repository fact while the generation keeps its
// content hint (#7978): the store, not a blank hint, decides whether a
// reconciliation stores a generation or re-projects the active one.
func TestBuildStreamingGenerationReconcileMarksRepositoryFact(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 13, 5, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.Reconcile = true

	collected := buildStreamingGeneration(repoPath, repo, "run-reconcile", observedAt, snapshot, false, "")

	if collected.Generation.FreshnessHint == "" {
		t.Fatal("reconcile generation FreshnessHint is empty, want the content hint")
	}
	if collected.Generation.IsDelta {
		t.Fatal("reconcile generation IsDelta = true, want false (full observation)")
	}
	envelopes := drainFactChannel(collected.Facts)
	repositoryFact := requireRepositoryFact(t, envelopes)
	if got, _ := repositoryFact.Payload["reconciliation_generation"].(bool); !got {
		t.Fatalf("repository reconciliation_generation = %#v, want true", repositoryFact.Payload["reconciliation_generation"])
	}
}

func TestBuildStreamingGenerationRecordsDeltaFlag(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 13, 5, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	full := testCollectorSnapshot(repoPath, "package main\n", "digest-full")
	if got := buildStreamingGeneration(repoPath, repo, "run-full", observedAt, full, false, ""); got.Generation.IsDelta {
		drainFactChannel(got.Facts)
		t.Fatal("full snapshot generation IsDelta = true, want false")
	} else {
		drainFactChannel(got.Facts)
	}

	delta := testCollectorSnapshot(repoPath, "package main\n", "digest-delta")
	delta.Delta = true
	got := buildStreamingGeneration(repoPath, repo, "run-delta", observedAt, delta, false, "")
	if !got.Generation.IsDelta {
		t.Fatal("delta snapshot generation IsDelta = false, want true")
	}
	drainFactChannel(got.Facts)
}

func requireRepositoryFact(t *testing.T, envelopes []facts.Envelope) facts.Envelope {
	t.Helper()

	for _, envelope := range envelopes {
		if envelope.FactKind == "repository" {
			return envelope
		}
	}
	t.Fatalf("repository fact missing from %#v", envelopes)
	return facts.Envelope{}
}
