// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// coveredRepoDependencyIntentStore opts the fake into the
// emitted-full-successor drain (#7165), reporting the configured generations
// as covered.
type coveredRepoDependencyIntentStore struct {
	*fakeRepoDependencyIntentStore
	covered map[string]struct{}
	lookups int
}

func (f *coveredRepoDependencyIntentStore) CoveredByEmittedFullSuccessorIDs(
	_ context.Context, _ string, generationIDs []string,
) (map[string]struct{}, error) {
	f.lookups++
	covered := make(map[string]struct{})
	for _, id := range generationIDs {
		if _, ok := f.covered[id]; ok {
			covered[id] = struct{}{}
		}
	}
	return covered, nil
}

func coveredRepoDependencyRow(intentID, genID string, now time.Time) SharedProjectionIntentRow {
	repoID := "repository:r_repo_a"
	return repoDependencyIntentRow(
		intentID, "scope-a", repoID, repoID, "run-1", genID, now,
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_target",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   CrossRepoEvidenceSource,
		},
	)
}

// TestRepoDependencyProjectionRunnerDrainsCoveredGenerations proves the #7165
// drain: rows on a generation covered by a newer emitted full generation are
// marked completed without a retract or write cycle, and counted apart from
// acceptance mismatches.
func TestRepoDependencyProjectionRunnerDrainsCoveredGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	row := coveredRepoDependencyRow("covered-1", "gen-old", now)
	reader := &coveredRepoDependencyIntentStore{
		fakeRepoDependencyIntentStore: &fakeRepoDependencyIntentStore{
			pendingByDomain:         []SharedProjectionIntentRow{row},
			pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {row}},
			leaseGranted:            true,
		},
		covered: map[string]struct{}{"gen-old": {}},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-old", true),
	}

	result, _, _, err := runner.processAcceptanceUnit(
		context.Background(), now, repoID, reader, PartitionProcessResult{}, now,
	)
	if err != nil {
		t.Fatalf("processAcceptanceUnit() error = %v", err)
	}
	if reader.lookups != 1 {
		t.Fatalf("drain lookups = %d, want exactly 1", reader.lookups)
	}
	if len(writer.retractCalls) != 0 {
		t.Fatalf("len(retractCalls) = %d, want 0: covered rows must not retract", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("len(writeCalls) = %d, want 0: covered rows must not write", len(writer.writeCalls))
	}
	if len(reader.marked) != 1 || reader.marked[0] != "covered-1" {
		t.Fatalf("marked = %v, want [covered-1]", reader.marked)
	}
	if result.CoveredByFullSuccessorIntents != 1 {
		t.Fatalf("CoveredByFullSuccessorIntents = %d, want 1", result.CoveredByFullSuccessorIntents)
	}
	if result.ProcessedIntents != 1 {
		t.Fatalf("ProcessedIntents = %d, want 1", result.ProcessedIntents)
	}
}

// TestRepoDependencyProjectionRunnerKeepsUncoveredGenerations pins the
// non-drain path: rows whose generation the port does not cover still project.
func TestRepoDependencyProjectionRunnerKeepsUncoveredGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	row := coveredRepoDependencyRow("kept-1", "gen-1", now)
	reader := &coveredRepoDependencyIntentStore{
		fakeRepoDependencyIntentStore: &fakeRepoDependencyIntentStore{
			pendingByDomain:         []SharedProjectionIntentRow{row},
			pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {row}},
			leaseGranted:            true,
		},
		covered: map[string]struct{}{},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
	}

	result, _, _, err := runner.processAcceptanceUnit(
		context.Background(), now, repoID, reader, PartitionProcessResult{}, now,
	)
	if err != nil {
		t.Fatalf("processAcceptanceUnit() error = %v", err)
	}
	if len(writer.writeCalls) == 0 {
		t.Fatal("len(writeCalls) = 0, want the kept row written")
	}
	if result.CoveredByFullSuccessorIntents != 0 {
		t.Fatalf("CoveredByFullSuccessorIntents = %d, want 0", result.CoveredByFullSuccessorIntents)
	}
}
