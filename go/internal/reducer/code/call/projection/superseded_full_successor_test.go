// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"testing"
	"time"

	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// coveredCodeCallIntentStore opts the fake into the emitted-full-successor
// drain (#7165), reporting the configured generations as covered.
type coveredCodeCallIntentStore struct {
	*fakeCodeCallIntentStore
	covered map[string]struct{}
	lookups int
}

func (f *coveredCodeCallIntentStore) CoveredByEmittedFullSuccessorIDs(
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

func coveredCodeCallRow(intentID, genID string, now time.Time) sharedintent.Row {
	return sharedintent.Row{
		IntentID:         intentID,
		ProjectionDomain: reducercontract.DomainCodeCalls,
		PartitionKey:     "caller->callee",
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     genID,
		Payload: map[string]any{
			"repo_id":          "repo-a",
			"caller_entity_id": "caller",
			"callee_entity_id": "callee",
			"evidence_source":  codecall.EvidenceSource,
		},
		CreatedAt: now,
	}
}

// TestCodeCallProjectionRunnerDrainsCoveredGenerations proves the #7165 drain:
// rows on a generation covered by a newer emitted full generation are marked
// completed without a write cycle, and counted apart from acceptance
// mismatches. Since #7736 F5 the pure-drain cycle runs the forced retract on
// the drained scope (one call per evidence source) so a crash-window partial
// write ahead of the drain cannot linger.
func TestCodeCallProjectionRunnerDrainsCoveredGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	row := coveredCodeCallRow("covered-1", "gen-old", now)
	reader := &coveredCodeCallIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByDomain:     []sharedintent.Row{row},
			pendingByAcceptance: map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {row}},
			leaseGranted:        true,
		},
		covered: map[string]struct{}{"gen-old": {}},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader.fakeCodeCallIntentStore,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-old", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if reader.lookups != 1 {
		t.Fatalf("drain lookups = %d, want exactly 1", reader.lookups)
	}
	if len(writer.retractCalls) != 2 {
		t.Fatalf("len(retractCalls) = %d, want 2: the forced drain retract runs once per evidence source (#7736 F5)", len(writer.retractCalls))
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
	if result.StaleIntents != 1 {
		t.Fatalf("StaleIntents = %d, want 1: covered intents are a subset of stale intents", result.StaleIntents)
	}
	if result.ProcessedIntents != 1 {
		t.Fatalf("ProcessedIntents = %d, want 1", result.ProcessedIntents)
	}
}

// TestCodeCallProjectionRunnerKeepsUncoveredGenerations pins the non-drain
// path: rows whose generation the port does not cover still project.
func TestCodeCallProjectionRunnerKeepsUncoveredGenerations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	row := coveredCodeCallRow("kept-1", "gen-1", now)
	reader := &coveredCodeCallIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByDomain:     []sharedintent.Row{row},
			pendingByAcceptance: map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {row}},
			leaseGranted:        true,
		},
		covered: map[string]struct{}{},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader.fakeCodeCallIntentStore,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if len(writer.writeCalls) == 0 {
		t.Fatal("len(writeCalls) = 0, want the kept row written")
	}
	if result.CoveredByFullSuccessorIntents != 0 {
		t.Fatalf("CoveredByFullSuccessorIntents = %d, want 0", result.CoveredByFullSuccessorIntents)
	}
}
