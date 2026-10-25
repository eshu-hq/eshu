// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/epoch"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestGenerationContentVersionFoldsCommittedEpoch pins the production read
// (#9097): the version names the epoch of the committed ledger, not a
// hand-typed constant.
func TestGenerationContentVersionFoldsCommittedEpoch(t *testing.T) {
	t.Parallel()
	want := "git-collector." + epoch.Current() + ";parsers="
	if version := GenerationContentVersion(); !strings.HasPrefix(version, want) {
		t.Fatalf("GenerationContentVersion() = %q, want prefix %q", version, want)
	}
	if got := contentVersionForEpoch(epoch.Current()); got != GenerationContentVersion() {
		t.Fatalf("contentVersionForEpoch(current) = %q, want the cached %q", got, GenerationContentVersion())
	}
}

// TestContentEpochAdvanceDefeatsUnchangedGenerationSkip is the store seam of
// RED-A (#9097, ruling a9-117 d6): when the ledger epoch advances, the
// freshness hint of an unchanged snapshot moves, so the ingestion store's
// duplicate check no longer matches the generation stored under the old epoch
// and the new output is stored. An unchanged epoch keeps the match, so a
// fixture-only re-pin stores nothing.
func TestContentEpochAdvanceDefeatsUnchangedGenerationSkip(t *testing.T) {
	t.Parallel()
	pinned := epoch.Ledger{Epoch: epoch.Current(), Seal: epoch.SealOf(nil)}
	advanced, err := epoch.Repin(pinned, nil, true)
	if err != nil {
		t.Fatalf("Repin: %v", err)
	}
	oldVersion := contentVersionForEpoch(pinned.Epoch)
	newVersion := contentVersionForEpoch(advanced.Epoch)
	if oldVersion == newVersion {
		t.Fatalf("content version did not move with the epoch: %q", oldVersion)
	}
	want := "parsers=" + parser.DefaultRegistry().Fingerprint()
	if !strings.Contains(newVersion, want) {
		t.Fatalf("advanced version %q dropped the parser fingerprint", newVersion)
	}

	snapshot := testCollectorSnapshot(t.TempDir(), "package main\n", "digest-1")
	const commit = "0123456789abcdef0123456789abcdef01234567"
	stored := backfill.UnchangedGenerationCandidate{
		FreshnessHint: snapshotFreshnessHintForVersion(snapshot, oldVersion),
		SourceCommit:  commit,
	}
	sameEpoch := scope.ScopeGeneration{FreshnessHint: snapshotFreshnessHintForVersion(snapshot, oldVersion), SourceCommitSHA: commit}
	nextEpoch := scope.ScopeGeneration{FreshnessHint: snapshotFreshnessHintForVersion(snapshot, newVersion), SourceCommitSHA: commit}
	if !stored.Matches(sameEpoch) {
		t.Fatalf("an unchanged epoch must keep the duplicate match, or a fixture-only re-pin re-stores every repository")
	}
	if stored.Matches(nextEpoch) {
		t.Fatalf("an advanced epoch still matches the stored generation: new output would be dropped as a duplicate")
	}
}
