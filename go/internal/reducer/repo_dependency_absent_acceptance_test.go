// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// The shard wrapper must forward the #7853 probe: multi-worker runs wrap the
// store, and a wrapper that drops the port would silently disable the drain.
var _ worker.AbsentGenerationReader = &repoDependencyShardReader{}

// absentGenerationRepoDependencyStore is a fakeRepoDependencyIntentStore that
// opts into the #7853 drain by reporting a fixed gone set from
// AbsentGenerationIDs.
type absentGenerationRepoDependencyStore struct {
	*fakeRepoDependencyIntentStore
	gone map[string]struct{}
}

func (s *absentGenerationRepoDependencyStore) AbsentGenerationIDs(
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

func repoDependencyAbsentAcceptanceLookup(key SharedProjectionAcceptanceKey) (string, bool) {
	if key.AcceptanceUnitID == "repository:r_healthy" {
		return "gen-healthy", true
	}
	return "", false
}

func repoDependencyAbsentAcceptanceRows(now time.Time) []sharedintent.Row {
	return []sharedintent.Row{
		{
			IntentID:         "orphan-1",
			ProjectionDomain: DomainRepoDependency,
			PartitionKey:     "partition-orphan",
			ScopeID:          "scope-orphan",
			AcceptanceUnitID: "repository:r_orphan",
			RepositoryID:     "repository:r_orphan",
			SourceRunID:      "run-orphan",
			GenerationID:     "gen-orphan-gone",
			CreatedAt:        now,
		},
		{
			IntentID:         "healthy-1",
			ProjectionDomain: DomainRepoDependency,
			PartitionKey:     "partition-healthy",
			ScopeID:          "scope-healthy",
			AcceptanceUnitID: "repository:r_healthy",
			RepositoryID:     "repository:r_healthy",
			SourceRunID:      "run-healthy",
			GenerationID:     "gen-healthy",
			CreatedAt:        now.Add(time.Second),
		},
	}
}

func repoDependencyMarkedContains(marked []string, intentID string) bool {
	for _, id := range marked {
		if id == intentID {
			return true
		}
	}
	return false
}

func TestRepoDependencySelectionDrainsGenerationGoneOrphan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	inner := &fakeRepoDependencyIntentStore{pendingByDomain: repoDependencyAbsentAcceptanceRows(now)}
	reader := &absentGenerationRepoDependencyStore{
		fakeRepoDependencyIntentStore: inner,
		gone:                          map[string]struct{}{"gen-orphan-gone": {}},
	}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  repoDependencyAbsentAcceptanceLookup,
		Config:       RepoDependencyProjectionRunnerConfig{BatchLimit: 100},
	}

	unitID, drained, tornIDs, err := runner.selectAcceptanceUnitWork(context.Background(), now)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if unitID != "repository:r_healthy" {
		t.Fatalf("selected unit = %q, want the healthy unit past the drained orphan", unitID)
	}
	if drained != 1 {
		t.Fatalf("drained = %d, want 1", drained)
	}
	if len(tornIDs) != 0 {
		t.Fatalf("tornIDs = %v, want none", tornIDs)
	}
	if !repoDependencyMarkedContains(inner.marked, "orphan-1") {
		t.Fatalf("marked = %v, want the generation-gone orphan completed", inner.marked)
	}
	if repoDependencyMarkedContains(inner.marked, "healthy-1") {
		t.Fatalf("marked = %v, the healthy intent must not be marked by selection", inner.marked)
	}
}

func TestRepoDependencyShardedReaderForwardsAbsentGenerationProbe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	inner := &fakeRepoDependencyIntentStore{pendingByDomain: repoDependencyAbsentAcceptanceRows(now)}
	ported := &absentGenerationRepoDependencyStore{
		fakeRepoDependencyIntentStore: inner,
		gone:                          map[string]struct{}{"gen-orphan-gone": {}},
	}
	sharded := &repoDependencyShardReader{inner: ported, shardID: 0, shardCount: 1}
	runner := RepoDependencyProjectionRunner{
		IntentReader: sharded,
		AcceptedGen:  repoDependencyAbsentAcceptanceLookup,
		Config:       RepoDependencyProjectionRunnerConfig{BatchLimit: 100},
	}

	unitID, drained, _, err := runner.selectAcceptanceUnitWork(context.Background(), now)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if unitID != "repository:r_healthy" {
		t.Fatalf("selected unit = %q, want the healthy unit", unitID)
	}
	if drained != 1 {
		t.Fatalf("drained = %d, want 1 through the shard wrapper", drained)
	}
	if !repoDependencyMarkedContains(inner.marked, "orphan-1") {
		t.Fatalf("marked = %v, want the orphan drained through the shard wrapper", inner.marked)
	}
}

func TestRepoDependencySelectionDrainsAtCapAndRescans(t *testing.T) {
	t.Parallel()

	// A full 10,000-row cap window of generation-gone orphans ahead of the
	// healthy unit: pre-#7853 the selection errored at the cap and the
	// healthy unit starved. Post-fix the cap drains and rescans past them.
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	rows := make([]sharedintent.Row, 0, maxRepoDependencyAcceptanceScanLimit+1)
	gone := make(map[string]struct{}, maxRepoDependencyAcceptanceScanLimit)
	for i := 0; i < maxRepoDependencyAcceptanceScanLimit; i++ {
		gen := "gen-cap-orphan-gone"
		gone[gen] = struct{}{}
		rows = append(rows, sharedintent.Row{
			IntentID:         "orphan-cap-" + string(rune('a'+(i%26))) + "-" + string(rune('a'+((i/26)%26))) + "-" + string(rune('a'+((i/676)%26))),
			ProjectionDomain: DomainRepoDependency,
			PartitionKey:     "partition-orphan",
			ScopeID:          "scope-orphan",
			AcceptanceUnitID: "repository:r_orphan",
			RepositoryID:     "repository:r_orphan",
			SourceRunID:      "run-orphan",
			GenerationID:     gen,
			CreatedAt:        now.Add(time.Duration(i) * time.Nanosecond),
		})
	}
	rows = append(rows, sharedintent.Row{
		IntentID:         "healthy-1",
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     "partition-healthy",
		ScopeID:          "scope-healthy",
		AcceptanceUnitID: "repository:r_healthy",
		RepositoryID:     "repository:r_healthy",
		SourceRunID:      "run-healthy",
		GenerationID:     "gen-healthy",
		CreatedAt:        now.Add(time.Duration(maxRepoDependencyAcceptanceScanLimit+1) * time.Nanosecond),
	})
	inner := &fakeRepoDependencyIntentStore{pendingByDomain: rows}
	reader := &absentGenerationRepoDependencyStore{
		fakeRepoDependencyIntentStore: inner,
		gone:                          gone,
	}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  repoDependencyAbsentAcceptanceLookup,
		Config:       RepoDependencyProjectionRunnerConfig{BatchLimit: 100},
	}

	unitID, drained, _, err := runner.selectAcceptanceUnitWork(context.Background(), now)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v, want drain-and-rescan past the cap", err)
	}
	if unitID != "repository:r_healthy" {
		t.Fatalf("selected unit = %q, want the healthy unit past the drained cap window", unitID)
	}
	if drained != maxRepoDependencyAcceptanceScanLimit {
		t.Fatalf("drained = %d, want %d", drained, maxRepoDependencyAcceptanceScanLimit)
	}
}

func TestRepoDependencySelectionKeepsTornWritePending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	inner := &fakeRepoDependencyIntentStore{pendingByDomain: repoDependencyAbsentAcceptanceRows(now)}
	reader := &absentGenerationRepoDependencyStore{
		fakeRepoDependencyIntentStore: inner,
		gone:                          map[string]struct{}{},
	}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  repoDependencyAbsentAcceptanceLookup,
		Config:       RepoDependencyProjectionRunnerConfig{BatchLimit: 100},
	}

	unitID, drained, tornIDs, err := runner.selectAcceptanceUnitWork(context.Background(), now)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if unitID != "repository:r_healthy" {
		t.Fatalf("selected unit = %q, want the healthy unit", unitID)
	}
	if drained != 0 {
		t.Fatalf("drained = %d, want 0: the generation still exists", drained)
	}
	if len(tornIDs) != 1 {
		t.Fatalf("tornIDs = %v, want 1 torn row", tornIDs)
	}
	if len(inner.marked) != 0 {
		t.Fatalf("marked = %v, want no mark for a torn write", inner.marked)
	}
}
