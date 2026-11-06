// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// refreshCompletionFence is a RefreshFenceLookup whose answers follow the
// reader's completed set: a repo's per-edge rows are ready once that repo's
// refresh intent is marked completed. It mirrors the production fence's
// durable happens-before signal at unit scale.
type refreshCompletionFence struct {
	reader                      *fakeSharedIntentReader
	refreshIntentByPartitionKey map[string]string
}

func (f *refreshCompletionFence) HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
	_ context.Context,
	_ sharedintent.AcceptanceKey,
	_ string,
	partitionKey string,
	_ string,
) (bool, error) {
	want, ok := f.refreshIntentByPartitionKey[partitionKey]
	if !ok {
		return false, nil
	}
	f.reader.mu.Lock()
	defer f.reader.mu.Unlock()
	for _, id := range f.reader.marked {
		if id == want {
			return true, nil
		}
	}
	return false, nil
}

// capturingUnroutableWriter records the dead-letter rows the worker persists.
type capturingUnroutableWriter struct {
	rows []sharedintent.UnroutableRow
}

func (w *capturingUnroutableWriter) WriteUnroutableIntents(
	_ context.Context,
	rows []sharedintent.UnroutableRow,
) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// poisonFailingEdgeWriter mirrors the production writer's fail-closed delta
// rejection: any retract batch holding a delta-flagged row with no usable
// delta_file_paths fails with the same typed error production returns,
// before any statement runs. The poison split uses the shared predicate the
// worker quarantines with, so the test proves the two agree.
type poisonFailingEdgeWriter struct {
	fakeEdgeWriter
	retractBatches [][]sharedintent.Row
}

func (w *poisonFailingEdgeWriter) RetractEdges(
	_ context.Context,
	_ string,
	rows []sharedintent.Row,
	_ string,
) error {
	w.retractBatches = append(w.retractBatches, append([]sharedintent.Row(nil), rows...))
	for _, row := range rows {
		if sharedintent.RowHasUnusableDeltaScope(row) {
			return sharedintent.NewUnusableDeltaScopeRowError(sharedintent.RowRepoID(row))
		}
	}
	return nil
}

const (
	deadLetterScopeID     = "scope-a"
	deadLetterSourceRunID = "run-1"
	deadLetterGenID       = "gen-1"
	deadLetterPoisonRepo  = "repo-poison"
	deadLetterHealthyRepo = "repo-healthy"
)

func deadLetterRefreshRow(repoID string, usableDelta bool, createdAt time.Time) sharedintent.Row {
	payload := map[string]any{
		"action":      "refresh",
		"intent_type": sharedintent.RepoRefreshIntentType,
		"repo_id":     repoID,
	}
	if usableDelta {
		payload["delta_projection"] = true
		payload["delta_file_paths"] = []string{"src/changed.py"}
	}
	return sharedintent.Row{
		IntentID:         "refresh-" + repoID,
		ProjectionDomain: reducercontract.DomainRationaleEdges,
		PartitionKey:     sharedintent.RepoWideRetractRefreshPartitionKey(reducercontract.DomainRationaleEdges, repoID),
		ScopeID:          deadLetterScopeID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      deadLetterSourceRunID,
		GenerationID:     deadLetterGenID,
		Payload:          payload,
		CreatedAt:        createdAt,
	}
}

func deadLetterEdgeRow(intentID, repoID string, createdAt time.Time) sharedintent.Row {
	return sharedintent.Row{
		IntentID:         intentID,
		ProjectionDomain: reducercontract.DomainRationaleEdges,
		PartitionKey:     "rationale:edge:" + intentID,
		ScopeID:          deadLetterScopeID,
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      deadLetterSourceRunID,
		GenerationID:     deadLetterGenID,
		Payload: map[string]any{
			"action":              "upsert",
			"repo_id":             repoID,
			"retract_via_refresh": true,
			"rationale_uid":       "rationale:uid:WHY:" + intentID,
			"target_entity_id":    "content-entity:target",
			"comment_kind":        "WHY",
			"excerpt_hash":        "abc",
		},
		CreatedAt: createdAt,
	}
}

// TestSharedPartitionPoisonedRefreshDoesNotBlockPartition is the #10069
// regression: one poisoned whole-scope refresh row (a delta-generation
// repository whose changed paths could not be qualified, so its
// delta_file_paths are empty) must reach a terminal state instead of failing
// its partition's retract every poll. The poisoned row is selected first in
// its partition (refresh-first ordering) and fences per-edge rows behind it,
// so before the fix this batch fails as a whole: the poisoned refresh, the
// healthy sibling repo's refresh, and every per-edge row stay pending
// forever. After the fix the first cycle dead-letters the poisoned refresh
// (durable unroutable row plus completion) while the healthy repo's refresh
// still retracts, and the second cycle projects the fenced per-edge rows once
// the fence sees their refreshes completed.
func TestSharedPartitionPoisonedRefreshDoesNotBlockPartition(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	poisonRefresh := deadLetterRefreshRow(deadLetterPoisonRepo, false, createdAt)
	// The poisoned row carries the fail-closed shape: delta-flagged with no
	// usable paths. A usable delta would bind a file-scoped retract instead.
	poisonRefresh.Payload["delta_projection"] = true
	poisonRefresh.Payload["delta_file_paths"] = []string{}
	healthyRefresh := deadLetterRefreshRow(deadLetterHealthyRepo, false, createdAt.Add(time.Minute))
	healthyEdge := deadLetterEdgeRow("edge-healthy-1", deadLetterHealthyRepo, createdAt.Add(3*time.Minute))

	reader := &fakeSharedIntentReader{intents: []sharedintent.Row{
		poisonRefresh, healthyRefresh, healthyEdge,
	}}
	fence := &refreshCompletionFence{
		reader: reader,
		refreshIntentByPartitionKey: map[string]string{
			poisonRefresh.PartitionKey:  poisonRefresh.IntentID,
			healthyRefresh.PartitionKey: healthyRefresh.IntentID,
		},
	}
	edges := &poisonFailingEdgeWriter{}
	unroutable := &capturingUnroutableWriter{}
	cfg := PartitionProcessorConfig{
		Domain:         reducercontract.DomainRationaleEdges,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "test-runner",
		LeaseTTL:       time.Minute,
		BatchLimit:     10,
		EvidenceSource: "reducer/rationale",
	}

	now := time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC)
	first, err := ProcessPartitionOnce(
		context.Background(), now, cfg,
		&fakeLeaseManager{granted: true}, reader, edges,
		acceptedGenerationFixed(deadLetterGenID, true), nil,
		readinessLookupFixed(true, true), nil, nil, fence, nil, unroutable,
	)
	if err != nil {
		t.Fatalf("first ProcessPartitionOnce() error = %v, want the poisoned refresh dead-lettered so the partition drains", err)
	}

	marked := markedSet(reader)
	for _, want := range []string{poisonRefresh.IntentID, healthyRefresh.IntentID} {
		if !marked[want] {
			t.Fatalf("first cycle marked = %v, want it to include refresh %q (a poisoned row must not wedge a healthy sibling repo's refresh)", markedKeys(marked), want)
		}
	}
	if len(unroutable.rows) != 1 {
		t.Fatalf("unroutable rows = %+v, want exactly one dead letter for the poisoned refresh", unroutable.rows)
	}
	deadLetter := unroutable.rows[0]
	if deadLetter.IntentID != poisonRefresh.IntentID {
		t.Fatalf("dead letter intent = %q, want the poisoned refresh %q", deadLetter.IntentID, poisonRefresh.IntentID)
	}
	if deadLetter.Reason != sharedintent.UnusableDeltaScopeReason {
		t.Fatalf("dead letter reason = %q, want %q", deadLetter.Reason, sharedintent.UnusableDeltaScopeReason)
	}
	if deadLetter.RepositoryID != deadLetterPoisonRepo {
		t.Fatalf("dead letter repository = %q, want %q", deadLetter.RepositoryID, deadLetterPoisonRepo)
	}
	if deadLetter.ProjectionDomain != reducercontract.DomainRationaleEdges {
		t.Fatalf("dead letter domain = %q, want %q", deadLetter.ProjectionDomain, reducercontract.DomainRationaleEdges)
	}
	if first.UnusableDeltaDeadLettered != 1 {
		t.Fatalf("first cycle UnusableDeltaDeadLettered = %d, want 1", first.UnusableDeltaDeadLettered)
	}

	marked = markedSet(reader)
	if !marked[healthyEdge.IntentID] {
		t.Fatalf("first cycle marked = %v, want it to include the healthy per-edge row %q (same-batch rows write once their refresh retracts)", markedKeys(marked), healthyEdge.IntentID)
	}
	if first.ProcessedIntents != 3 {
		t.Fatalf("first cycle ProcessedIntents = %d, want 3 (two refreshes plus one per-edge row, with no double-count of the dead letter)", first.ProcessedIntents)
	}

	// A per-edge row of the poisoned repo pending while its refresh was
	// poisoned: once the refresh is dead-lettered and completed, the fence
	// opens and the row projects on the next cycle instead of deferring
	// forever.
	poisonEdge := deadLetterEdgeRow("edge-poison-1", deadLetterPoisonRepo, createdAt.Add(2*time.Minute))
	reader.mu.Lock()
	reader.intents = append(reader.intents, poisonEdge)
	reader.mu.Unlock()
	second, err := ProcessPartitionOnce(
		context.Background(), now, cfg,
		&fakeLeaseManager{granted: true}, reader, edges,
		acceptedGenerationFixed(deadLetterGenID, true), nil,
		readinessLookupFixed(true, true), nil, nil, fence, nil, unroutable,
	)
	if err != nil {
		t.Fatalf("second ProcessPartitionOnce() error = %v, want the fenced per-edge row to project once its refresh completed", err)
	}
	marked = markedSet(reader)
	if !marked[poisonEdge.IntentID] {
		t.Fatalf("marked after two cycles = %v, want it to include per-edge row %q", markedKeys(marked), poisonEdge.IntentID)
	}
	if second.ProcessedIntents != 1 {
		t.Fatalf("second cycle ProcessedIntents = %d, want 1 (the fenced per-edge row)", second.ProcessedIntents)
	}
	if second.UnusableDeltaDeadLettered != 0 {
		t.Fatalf("second cycle UnusableDeltaDeadLettered = %d, want 0 (the dead letter fires once)", second.UnusableDeltaDeadLettered)
	}
}

// transientFailingEdgeWriter fails every retract with a plain (untyped)
// backend-style error. Only the typed fail-closed rejection may quarantine;
// anything else must keep failing the cycle and retrying.
type transientFailingEdgeWriter struct {
	fakeEdgeWriter
}

func (w *transientFailingEdgeWriter) RetractEdges(
	_ context.Context,
	_ string,
	_ []sharedintent.Row,
	_ string,
) error {
	return errors.New("connection refused")
}

// TestSharedPartitionRetriesUntypedRetractError pins the other half of the
// #10069 contract: a retract failure that is NOT the typed fail-closed
// rejection still fails the cycle with nothing completed, so transient
// backend faults keep retrying instead of being terminalized into a dead
// letter. Type identity -- not error text -- is what separates the two.
func TestSharedPartitionRetriesUntypedRetractError(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	refresh := deadLetterRefreshRow(deadLetterHealthyRepo, false, createdAt)
	edge := deadLetterEdgeRow("edge-1", deadLetterHealthyRepo, createdAt.Add(time.Minute))
	reader := &fakeSharedIntentReader{intents: []sharedintent.Row{refresh, edge}}
	fence := &refreshCompletionFence{
		reader: reader,
		refreshIntentByPartitionKey: map[string]string{
			refresh.PartitionKey: refresh.IntentID,
		},
	}
	edges := &transientFailingEdgeWriter{}
	unroutable := &capturingUnroutableWriter{}
	cfg := PartitionProcessorConfig{
		Domain:         reducercontract.DomainRationaleEdges,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "test-runner",
		LeaseTTL:       time.Minute,
		BatchLimit:     10,
		EvidenceSource: "reducer/rationale",
	}

	_, err := ProcessPartitionOnce(
		context.Background(), createdAt.Add(time.Hour), cfg,
		&fakeLeaseManager{granted: true}, reader, edges,
		acceptedGenerationFixed(deadLetterGenID, true), nil,
		readinessLookupFixed(true, true), nil, nil, fence, nil, unroutable,
	)
	if err == nil {
		t.Fatalf("ProcessPartitionOnce() = nil, want the transient retract error to fail the cycle and retry")
	}
	if sharedintent.IsUnusableDeltaScopeError(err) {
		t.Fatalf("ProcessPartitionOnce() error = %v, want it NOT to classify as an unusable delta scope", err)
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want nothing completed on a transient failure", reader.marked)
	}
	if len(unroutable.rows) != 0 {
		t.Fatalf("unroutable rows = %+v, want no dead letter on a transient failure", unroutable.rows)
	}
}

// TestSharedPartitionFailsCycleWithoutUnroutableSink pins the fail-closed
// fallback: when no durable dead-letter sink is wired, a poisoned refresh
// fails the cycle instead of completing silently. Completing without the
// record would reintroduce the exact silent loss #5984 fixed.
func TestSharedPartitionFailsCycleWithoutUnroutableSink(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	poisonRefresh := deadLetterRefreshRow(deadLetterPoisonRepo, false, createdAt)
	poisonRefresh.Payload["delta_projection"] = true
	poisonRefresh.Payload["delta_file_paths"] = []string{}
	reader := &fakeSharedIntentReader{intents: []sharedintent.Row{poisonRefresh}}
	edges := &poisonFailingEdgeWriter{}
	cfg := PartitionProcessorConfig{
		Domain:         reducercontract.DomainRationaleEdges,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "test-runner",
		LeaseTTL:       time.Minute,
		BatchLimit:     10,
		EvidenceSource: "reducer/rationale",
	}

	_, err := ProcessPartitionOnce(
		context.Background(), createdAt.Add(time.Hour), cfg,
		&fakeLeaseManager{granted: true}, reader, edges,
		acceptedGenerationFixed(deadLetterGenID, true), nil,
		readinessLookupFixed(true, true), nil, nil, nil, nil, nil,
	)
	if err == nil {
		t.Fatalf("ProcessPartitionOnce() = nil, want the cycle to fail when no unroutable sink can record the dead letter")
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want nothing completed without a durable dead-letter record", reader.marked)
	}
}

func markedSet(reader *fakeSharedIntentReader) map[string]bool {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	set := make(map[string]bool, len(reader.marked))
	for _, id := range reader.marked {
		set[id] = true
	}
	return set
}

func markedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	return keys
}
