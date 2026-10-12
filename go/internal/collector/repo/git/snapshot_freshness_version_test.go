// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestBuildStreamingGenerationReconcileKeepsContentHint proves #7978: a
// reconciliation snapshot of an unchanged repository carries the same,
// non-empty freshness hint as an ordinary snapshot of the same content. A blank
// hint defeated the store's unchanged-generation check and minted one duplicate
// generation per scope per sweep interval. The forced reason travels on the
// generation, not by blanking the hint.
func TestBuildStreamingGenerationReconcileKeepsContentHint(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	observedAt := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	plain := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	reconcile := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	reconcile.Reconcile = true
	reconcile.ReconcileReason = "interval_elapsed"

	plainGen := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "run-plain",
		observedAt, plain, false, "")
	drainFactChannel(plainGen.Facts)
	reconcileGen := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "run-reconcile",
		observedAt, reconcile, false, "")
	drainFactChannel(reconcileGen.Facts)

	if reconcileGen.Generation.FreshnessHint == "" {
		t.Fatal("reconcile generation freshness hint is empty; want the content hint")
	}
	if got, want := reconcileGen.Generation.FreshnessHint, plainGen.Generation.FreshnessHint; got != want {
		t.Fatalf("reconcile hint = %q, want the unchanged content hint %q", got, want)
	}
	if got, want := reconcileGen.Generation.ReconcileReason, "interval_elapsed"; got != want {
		t.Fatalf("generation ReconcileReason = %q, want %q", got, want)
	}
	if plainGen.Generation.ReconcileReason != "" {
		t.Fatalf("plain generation ReconcileReason = %q, want empty", plainGen.Generation.ReconcileReason)
	}
}

// TestBuildStreamingGenerationReconcileWithoutReasonIsBounded keeps the stored
// reason non-empty for a reconcile snapshot that did not carry a decision
// reason, so the store still treats it as a reconciliation.
func TestBuildStreamingGenerationReconcileWithoutReasonIsBounded(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.Reconcile = true

	collected := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "run-r",
		time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC), snapshot, false, "")
	drainFactChannel(collected.Facts)
	if got, want := collected.Generation.ReconcileReason, GenerationReconcileReasonUnspecified; got != want {
		t.Fatalf("ReconcileReason = %q, want %q", got, want)
	}
}

// TestSnapshotFreshnessHintFoldsContentVersion proves a collector, parser, or
// fact-schema version change with unchanged content yields a different hint, so
// the store mints a generation for the new output instead of treating it as a
// duplicate (#7978).
func TestSnapshotFreshnessHintFoldsContentVersion(t *testing.T) {
	t.Parallel()
	snapshot := testCollectorSnapshot(t.TempDir(), "package main\n", "digest-1")

	v1 := snapshotFreshnessHintForVersion(snapshot, "git-collector.v1;schema=aaa")
	v2 := snapshotFreshnessHintForVersion(snapshot, "git-collector.v2;schema=aaa")
	schema := snapshotFreshnessHintForVersion(snapshot, "git-collector.v1;schema=bbb")
	if v1 == v2 {
		t.Fatalf("collector version bump kept hint %q", v1)
	}
	if v1 == schema {
		t.Fatalf("schema version change kept hint %q", v1)
	}
	if again := snapshotFreshnessHintForVersion(snapshot, "git-collector.v1;schema=aaa"); again != v1 {
		t.Fatalf("hint not deterministic: %q then %q", v1, again)
	}
	if got, want := snapshotFreshnessHint(snapshot), snapshotFreshnessHintForVersion(snapshot, GenerationContentVersion()); got != want {
		t.Fatalf("snapshotFreshnessHint = %q, want the hint for the current content version %q", got, want)
	}
}

// TestGenerationContentVersionNamesCollectorParsersAndSchema pins the shape of
// the version identity: the hand-bumped collector constant, the parser-set
// fingerprint (#7986), and a digest of the supported fact-schema registry.
func TestGenerationContentVersionNamesCollectorParsersAndSchema(t *testing.T) {
	t.Parallel()
	version := GenerationContentVersion()
	wantPrefix := gitGenerationContentVersion + ";parsers=" + parser.DefaultRegistry().Fingerprint() + ";schema="
	if !strings.HasPrefix(version, wantPrefix) {
		t.Fatalf("GenerationContentVersion() = %q, want prefix %q", version, wantPrefix)
	}
	if version == wantPrefix {
		t.Fatalf("GenerationContentVersion() = %q has an empty schema digest", version)
	}
	if again := GenerationContentVersion(); again != version {
		t.Fatalf("GenerationContentVersion not stable: %q then %q", version, again)
	}
}

// TestBuildStreamingGenerationDefaultBranchChangeKeepsHint proves a
// default-branch change no longer blanks the hint: the generation carries the
// DefaultBranchChanged flag, which the store honors by minting (#7678), and the
// hint folds the default branch so the content identity changes with it.
func TestBuildStreamingGenerationDefaultBranchChangeKeepsHint(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	snapshot := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	snapshot.DefaultBranchChanged = true

	collected := buildStreamingGeneration(repoPath, testCollectorRepositoryMetadata(repoPath), "run-branch",
		time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), snapshot, false, "")
	drainFactChannel(collected.Facts)
	if collected.Generation.FreshnessHint == "" {
		t.Fatal("default-branch generation hint is empty; want the content hint")
	}
	if !collected.Generation.DefaultBranchChanged {
		t.Fatal("generation DefaultBranchChanged = false, want true")
	}
	if collected.Generation.ReconcileReason != "" {
		t.Fatalf("generation ReconcileReason = %q, want empty for a default-branch change", collected.Generation.ReconcileReason)
	}

	main := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	main.GitRefs = []GitRef{{Name: "main", Kind: "branch", HeadSHA: "abc", Default: true}}
	trunk := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	trunk.GitRefs = []GitRef{{Name: "trunk", Kind: "branch", HeadSHA: "abc", Default: true}}
	if snapshotFreshnessHint(main) == snapshotFreshnessHint(trunk) {
		t.Fatal("default branch main -> trunk kept the same hint; want the default branch folded")
	}
}

// TestSnapshotFreshnessHintFoldsDeltaBaseline (ruling a9-120 §7.5): two deltas
// at the same head with the same paths but different baselines describe
// different diffs and must not share a hint; a re-diff from the same baseline
// keeps it, so the store can treat it as a duplicate. A full snapshot never
// carries a baseline, so its hint is unchanged.
func TestSnapshotFreshnessHintFoldsDeltaBaseline(t *testing.T) {
	t.Parallel()
	delta := func(baseline string) RepositorySnapshot {
		snapshot := testCollectorSnapshot("/repos/proof", "package main\n", "digest-1")
		snapshot.Delta = true
		snapshot.DeltaRelativePaths = []string{"main.go"}
		snapshot.DeltaBaselineCommitSHA = baseline
		return snapshot
	}
	if snapshotFreshnessHint(delta("sha-a")) == snapshotFreshnessHint(delta("sha-b")) {
		t.Fatal("deltas from different baselines share a hint")
	}
	first := snapshotFreshnessHint(delta("sha-a"))
	if again := snapshotFreshnessHint(delta("sha-a")); again != first {
		t.Fatalf("re-diff from the same baseline changed the hint: %s then %s", first, again)
	}
}
