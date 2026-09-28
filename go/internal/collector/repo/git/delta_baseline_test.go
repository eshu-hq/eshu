// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestUpdateRepositoryRecordsDeltaBaseline proves the commit a delta was
// diffed from travels with the delta (#7319), and that the full-snapshot
// fallbacks carry none.
func TestUpdateRepositoryRecordsDeltaBaseline(t *testing.T) {
	writeFakeGitForBaseline(t, `	*"rev-parse refs/remotes/origin/main"*)
		printf "newsha\n"
		;;
	*"cat-file -e oldsha"*)
		exit 0
		;;
	*"cat-file -e prunedsha"*)
		exit 1
		;;
	*"diff --name-status -z --find-renames oldsha refs/remotes/origin/main"*)
		printf "M\0cmd/api/main.go\0"
		;;
	*"checkout -B main refs/remotes/origin/main"*)
		;;`)

	for _, tc := range []struct {
		name, baseline, want string
	}{
		{name: "delta", baseline: "  oldsha\n", want: "oldsha"},
		{name: "no projected baseline", baseline: "", want: ""},
		{name: "unreachable baseline", baseline: "prunedsha", want: ""},
	} {
		updated, delta, _, err := updateRepository(context.Background(), baselineTestConfig(), t.TempDir(), "",
			discardLogger(), baselineTestEvent(), tc.baseline, nil)
		if err != nil || !updated {
			t.Fatalf("%s: updateRepository() = %v, %v; want updated", tc.name, updated, err)
		}
		if delta.BaselineCommitSHA != tc.want {
			t.Fatalf("%s: BaselineCommitSHA = %q, want %q", tc.name, delta.BaselineCommitSHA, tc.want)
		}
	}
}

// TestBuildSelectedRepositoriesCarriesDeltaBaseline covers the selection
// builder both the native and the webhook selector use. An empty diff is a
// full observation and carries no baseline.
func TestBuildSelectedRepositoriesCarriesDeltaBaseline(t *testing.T) {
	t.Parallel()
	repoPath := filepath.Join(t.TempDir(), "org", "repo")
	emptyPath := filepath.Join(filepath.Dir(repoPath), "empty")
	config := RepoSyncConfig{SourceMode: "explicit", ReposDir: filepath.Dir(filepath.Dir(repoPath))}
	deltas := map[string]GitSyncDelta{
		repoPath:  {ChangedFileTargets: []string{filepath.Join(repoPath, "a.go")}, BaselineCommitSHA: "A"},
		emptyPath: {BaselineCommitSHA: "A"},
	}
	selected := buildSelectedRepositories(config, []string{repoPath, emptyPath}, deltas, nil, nil, nil, nil)
	if len(selected) != 2 {
		t.Fatalf("selected = %d, want 2", len(selected))
	}
	byPath := map[string]SelectedRepository{}
	for _, repo := range selected {
		byPath[repo.RepoPath] = repo
	}
	if got := byPath[repoPath]; !got.Delta || got.DeltaBaselineCommitSHA != "A" {
		t.Fatalf("delta repo = Delta %v baseline %q, want true and A", got.Delta, got.DeltaBaselineCommitSHA)
	}
	if got := byPath[emptyPath]; got.Delta || got.DeltaBaselineCommitSHA != "" {
		t.Fatalf("empty-diff repo = Delta %v baseline %q, want a full observation with no baseline",
			got.Delta, got.DeltaBaselineCommitSHA)
	}
}

// TestNativeRepositorySnapshotterCarriesDeltaBaseline proves the snapshot
// keeps the baseline of the selected delta repository.
func TestNativeRepositorySnapshotterCarriesDeltaBaseline(t *testing.T) {
	t.Parallel()
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() = %v", err)
	}
	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(context.Background(), SelectedRepository{
		RepoPath: t.TempDir(), Delta: true, DeletedRelativePaths: []string{"gone.go"}, DeltaBaselineCommitSHA: "A",
	})
	if err != nil {
		t.Fatalf("SnapshotRepository() = %v", err)
	}
	if got.DeltaBaselineCommitSHA != "A" {
		t.Fatalf("snapshot DeltaBaselineCommitSHA = %q, want A", got.DeltaBaselineCommitSHA)
	}
}

// TestBuildStreamingGenerationRecordsDeltaBaseline proves the generation row
// gets the baseline for a delta and never for a full observation, even if a
// full snapshot were handed one.
func TestBuildStreamingGenerationRecordsDeltaBaseline(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.September, 28, 1, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	delta := testCollectorSnapshot(repoPath, "package main\n", "digest-delta")
	delta.Delta = true
	delta.DeltaBaselineCommitSHA = "A"
	got := buildStreamingGeneration(repoPath, repo, "run-delta", observedAt, delta, false, "")
	drainFactChannel(got.Facts)
	if got.Generation.DeltaBaselineCommitSHA != "A" {
		t.Fatalf("delta generation baseline = %q, want A", got.Generation.DeltaBaselineCommitSHA)
	}

	full := testCollectorSnapshot(repoPath, "package main\n", "digest-full")
	full.DeltaBaselineCommitSHA = "A"
	got = buildStreamingGeneration(repoPath, repo, "run-full", observedAt, full, false, "")
	drainFactChannel(got.Facts)
	if got.Generation.DeltaBaselineCommitSHA != "" {
		t.Fatalf("full generation baseline = %q, want empty", got.Generation.DeltaBaselineCommitSHA)
	}
}
