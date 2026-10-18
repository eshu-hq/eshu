// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// absentGenerationCodeCallStore is a fakeCodeCallIntentStore that opts into
// the #7853 drain by reporting a fixed gone set from AbsentGenerationIDs.
type absentGenerationCodeCallStore struct {
	*fakeCodeCallIntentStore
	gone map[string]struct{}
}

func (s *absentGenerationCodeCallStore) AbsentGenerationIDs(
	_ context.Context, generationIDs []string,
) (map[string]struct{}, error) {
	gone := make(map[string]struct{}, len(generationIDs))
	for _, id := range generationIDs {
		if _, ok := s.gone[id]; ok {
			gone[id] = struct{}{}
		}
	}
	return gone, nil
}

func codeCallAbsentAcceptanceLookup(key sharedintent.AcceptanceKey) (string, bool) {
	if key.ScopeID == "scope-accepted" && key.AcceptanceUnitID == "repo-accepted" && key.SourceRunID == "run-accepted" {
		return "gen-accepted", true
	}
	return "", false
}

func codeCallAbsentAcceptanceRows(now time.Time) []sharedintent.Row {
	return []sharedintent.Row{
		{
			IntentID:         "orphan-1",
			ProjectionDomain: reducercontract.DomainCodeCalls,
			PartitionKey:     "caller->callee-orphan",
			ScopeID:          "scope-orphan",
			AcceptanceUnitID: "repo-orphan",
			RepositoryID:     "repo-orphan",
			SourceRunID:      "run-orphan",
			GenerationID:     "gen-orphan-gone",
			CreatedAt:        now,
		},
		{
			IntentID:         "accepted-1",
			ProjectionDomain: reducercontract.DomainCodeCalls,
			PartitionKey:     "caller->callee-accepted",
			ScopeID:          "scope-accepted",
			AcceptanceUnitID: "repo-accepted",
			RepositoryID:     "repo-accepted",
			SourceRunID:      "run-accepted",
			GenerationID:     "gen-accepted",
			CreatedAt:        now.Add(time.Second),
		},
	}
}

func markedContains(marked []string, intentID string) bool {
	for _, id := range marked {
		if id == intentID {
			return true
		}
	}
	return false
}

func TestCodeCallSelectionDrainsGenerationGoneOrphan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	inner := &fakeCodeCallIntentStore{pendingByDomain: codeCallAbsentAcceptanceRows(now)}
	reader := &absentGenerationCodeCallStore{
		fakeCodeCallIntentStore: inner,
		gone:                    map[string]struct{}{"gen-orphan-gone": {}},
	}
	runner := Runner{
		IntentReader: reader,
		AcceptedGen:  codeCallAbsentAcceptanceLookup,
		Config:       RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.selectAcceptanceUnitWorkWithStats(context.Background(), now)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWorkWithStats() error = %v", err)
	}
	if result.Key.ScopeID != "scope-accepted" {
		t.Fatalf("selected scope = %q, want %q past the drained orphan", result.Key.ScopeID, "scope-accepted")
	}
	if result.AbsentDrained != 1 {
		t.Fatalf("AbsentDrained = %d, want 1", result.AbsentDrained)
	}
	if !markedContains(inner.marked, "orphan-1") {
		t.Fatalf("marked = %v, want the generation-gone orphan completed", inner.marked)
	}
	if markedContains(inner.marked, "accepted-1") {
		t.Fatalf("marked = %v, the healthy intent must not be marked by selection", inner.marked)
	}
}

func TestCodeCallSelectionDrainsAtCapAndRescans(t *testing.T) {
	t.Parallel()

	// 11 generation-gone orphans ahead of the healthy unit with a scan cap
	// of 10: pre-#7853 the selection errored at the cap and the healthy
	// unit starved. Post-fix the cap drains and rescans past the orphans.
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	rows := make([]sharedintent.Row, 0, 12)
	gone := make(map[string]struct{}, 11)
	for i := 0; i < 11; i++ {
		gen := "gen-orphan-gone-" + string(rune('a'+i))
		gone[gen] = struct{}{}
		rows = append(rows, sharedintent.Row{
			IntentID:         "orphan-cap-" + string(rune('a'+i)),
			ProjectionDomain: reducercontract.DomainCodeCalls,
			PartitionKey:     "caller->callee-orphan",
			ScopeID:          "scope-orphan",
			AcceptanceUnitID: "repo-orphan",
			RepositoryID:     "repo-orphan",
			SourceRunID:      "run-orphan",
			GenerationID:     gen,
			CreatedAt:        now.Add(time.Duration(i) * time.Millisecond),
		})
	}
	rows = append(rows, sharedintent.Row{
		IntentID:         "accepted-1",
		ProjectionDomain: reducercontract.DomainCodeCalls,
		PartitionKey:     "caller->callee-accepted",
		ScopeID:          "scope-accepted",
		AcceptanceUnitID: "repo-accepted",
		RepositoryID:     "repo-accepted",
		SourceRunID:      "run-accepted",
		GenerationID:     "gen-accepted",
		CreatedAt:        now.Add(time.Second),
	})
	inner := &fakeCodeCallIntentStore{pendingByDomain: rows}
	reader := &absentGenerationCodeCallStore{
		fakeCodeCallIntentStore: inner,
		gone:                    gone,
	}
	runner := Runner{
		IntentReader: reader,
		AcceptedGen:  codeCallAbsentAcceptanceLookup,
		Config:       RunnerConfig{BatchLimit: 2, AcceptanceScanLimit: 10},
	}

	result, err := runner.selectAcceptanceUnitWorkWithStats(context.Background(), now)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWorkWithStats() error = %v, want drain-and-rescan past the cap", err)
	}
	if result.Key.ScopeID != "scope-accepted" {
		t.Fatalf("selected scope = %q, want %q past the drained cap window", result.Key.ScopeID, "scope-accepted")
	}
	if result.AbsentDrained != 11 {
		t.Fatalf("AbsentDrained = %d, want 11", result.AbsentDrained)
	}
}

func TestCodeCallSelectionKeepsTornWritePending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	inner := &fakeCodeCallIntentStore{pendingByDomain: codeCallAbsentAcceptanceRows(now)}
	reader := &absentGenerationCodeCallStore{
		fakeCodeCallIntentStore: inner,
		gone:                    map[string]struct{}{},
	}
	runner := Runner{
		IntentReader: reader,
		AcceptedGen:  codeCallAbsentAcceptanceLookup,
		Config:       RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.selectAcceptanceUnitWorkWithStats(context.Background(), now)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWorkWithStats() error = %v", err)
	}
	if result.Key.ScopeID != "scope-accepted" {
		t.Fatalf("selected scope = %q, want %q", result.Key.ScopeID, "scope-accepted")
	}
	if result.AbsentDrained != 0 {
		t.Fatalf("AbsentDrained = %d, want 0: the generation still exists", result.AbsentDrained)
	}
	if result.AbsentTorn != 1 {
		t.Fatalf("AbsentTorn = %d, want 1", result.AbsentTorn)
	}
	if len(inner.marked) != 0 {
		t.Fatalf("marked = %v, want no mark for a torn write", inner.marked)
	}
}
