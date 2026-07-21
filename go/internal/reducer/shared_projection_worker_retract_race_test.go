// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// stateModelingEdgeWriter models the canonical graph edge STATE for a
// repo-wide-retract domain (handles_route / runs_in / invokes_cloud_action).
//
// RetractEdges mirrors the real repo-wide retract Cypher
// (retractHandlesRouteEdgesCypher et al., edge_writer_retract.go): it deletes
// EVERY edge for each repo present in the batch, regardless of which specific
// edges the batch carries. WriteEdges adds each row's edge to its repo's set.
//
// The #2910 cross-partition retract race is invisible to a call-recording stub
// (stubEdgeWriter) because that stub only counts calls. Modeling the edge set in
// partition-processing order is what reveals — and then proves the fix removes —
// the loss of sibling partitions' just-written edges within a single cycle.
type stateModelingEdgeWriter struct {
	edgesByRepo map[string]map[string]struct{}
}

func newStateModelingEdgeWriter() *stateModelingEdgeWriter {
	return &stateModelingEdgeWriter{edgesByRepo: make(map[string]map[string]struct{})}
}

func (w *stateModelingEdgeWriter) RetractEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	for _, repoID := range stateModelingRepoIDs(rows) {
		delete(w.edgesByRepo, repoID)
	}
	return nil
}

func (w *stateModelingEdgeWriter) WriteEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	for _, row := range rows {
		repoID := sharedProjectionRowRepoID(row)
		if repoID == "" {
			continue
		}
		edges := w.edgesByRepo[repoID]
		if edges == nil {
			edges = make(map[string]struct{})
			w.edgesByRepo[repoID] = edges
		}
		edges[row.IntentID] = struct{}{}
	}
	return nil
}

func (w *stateModelingEdgeWriter) edgeCount(repoID string) int {
	return len(w.edgesByRepo[repoID])
}

func stateModelingRepoIDs(rows []SharedProjectionIntentRow) []string {
	seen := make(map[string]struct{}, len(rows))
	var out []string
	for _, row := range rows {
		repoID := sharedProjectionRowRepoID(row)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		out = append(out, repoID)
	}
	return out
}

// fenceTrackingIntentStore models the durable shared_projection_intents table for
// the worker's refresh fence: it lists not-yet-completed intents, records
// completion, and answers both the code-call partition-history lookup and the
// generation-scoped refresh-fence lookup from that durable state. A completed
// intent is removed from the pending listing (so it is not re-projected); an
// exact retry preserves that completion because production intent IDs are
// deterministic and Postgres does not reopen completed rows during upsert.
type fenceTrackingIntentStore struct {
	pending   []SharedProjectionIntentRow
	byID      map[string]SharedProjectionIntentRow
	completed map[string]struct{} // intent IDs
	fenceKeys map[string]struct{} // scope|au|run|partition_key|domain
}

func newFenceTrackingIntentStore(intents []SharedProjectionIntentRow) *fenceTrackingIntentStore {
	byID := make(map[string]SharedProjectionIntentRow, len(intents))
	for _, intent := range intents {
		byID[intent.IntentID] = intent
	}
	return &fenceTrackingIntentStore{
		pending:   intents,
		byID:      byID,
		completed: make(map[string]struct{}),
		fenceKeys: make(map[string]struct{}),
	}
}

func (s *fenceTrackingIntentStore) ListPendingDomainIntents(_ context.Context, domain string, limit int) ([]SharedProjectionIntentRow, error) {
	out := make([]SharedProjectionIntentRow, 0, len(s.pending))
	for _, intent := range s.pending {
		if intent.ProjectionDomain != domain {
			continue
		}
		if _, done := s.completed[intent.IntentID]; done {
			continue
		}
		out = append(out, intent)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fenceTrackingIntentStore) MarkIntentsCompleted(_ context.Context, intentIDs []string, _ time.Time) error {
	for _, id := range intentIDs {
		s.completed[id] = struct{}{}
		intent, ok := s.byID[id]
		if !ok {
			continue
		}
		s.fenceKeys[fenceTupleKey(intent.ScopeID, intent.AcceptanceUnitID, intent.SourceRunID, intent.PartitionKey, intent.ProjectionDomain)] = struct{}{}
	}
	return nil
}

func (s *fenceTrackingIntentStore) HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(
	_ context.Context,
	key SharedProjectionAcceptanceKey,
	partitionKey string,
	domain string,
) (bool, error) {
	_, ok := s.fenceKeys[fenceTupleKey(key.ScopeID, key.AcceptanceUnitID, key.SourceRunID, partitionKey, domain)]
	return ok, nil
}

func (s *fenceTrackingIntentStore) HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
	_ context.Context,
	key SharedProjectionAcceptanceKey,
	generationID string,
	partitionKey string,
	domain string,
) (bool, error) {
	for intentID, intent := range s.byID {
		if intent.ScopeID != key.ScopeID ||
			intent.AcceptanceUnitID != key.AcceptanceUnitID ||
			intent.SourceRunID != key.SourceRunID ||
			intent.GenerationID != generationID ||
			intent.PartitionKey != partitionKey ||
			intent.ProjectionDomain != domain {
			continue
		}
		if _, done := s.completed[intentID]; done {
			return true, nil
		}
	}
	return false, nil
}

func fenceTupleKey(scope, au, run, partitionKey, domain string) string {
	return scope + "\x00" + au + "\x00" + run + "\x00" + partitionKey + "\x00" + domain
}

// handlesRouteRepoIntents builds one whole-scope refresh intent plus perEdgeCount
// per-edge HANDLES_ROUTE intents for one repo, with the per-edge partition keys
// placed in distinct partitions so the repo's edges genuinely span the ring.
func handlesRouteRepoIntents(t *testing.T, repoID string, perEdgeCount, partitionCount int, created time.Time) []SharedProjectionIntentRow {
	t.Helper()

	intents := []SharedProjectionIntentRow{
		BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainHandlesRoute,
			PartitionKey:     repoWideRetractRefreshPartitionKey(DomainHandlesRoute, repoID),
			ScopeID:          "scope-a",
			AcceptanceUnitID: repoID,
			RepositoryID:     repoID,
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload: map[string]any{
				"repo_id":     repoID,
				"intent_type": repoRefreshIntentType,
				"action":      repoRefreshAction,
			},
			CreatedAt: created,
		}),
	}
	for p := 0; p < perEdgeCount; p++ {
		partitionKey := partitionKeyForTestPartition(t, p%partitionCount, partitionCount, repoID+"-route-"+strconv.Itoa(p))
		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainHandlesRoute,
			PartitionKey:     partitionKey,
			ScopeID:          "scope-a",
			AcceptanceUnitID: repoID,
			RepositoryID:     repoID,
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"repo_id": repoID, "action": "upsert", retractViaRefreshKey: true},
			CreatedAt:        created,
		}))
	}
	return intents
}

func handlesRouteFenceConfig(partitionID, partitionCount int) PartitionProcessorConfig {
	return PartitionProcessorConfig{
		Domain:         DomainHandlesRoute,
		PartitionID:    partitionID,
		PartitionCount: partitionCount,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: handlesRouteEvidenceSource,
	}
}

// TestProcessPartitionOnceHandlesRouteFenceConverges is the #2898/#2910
// convergence proof: a repo with three HANDLES_ROUTE edges spanning three
// partitions, plus its paired refresh intent, projected through the fenced
// worker, retains ALL THREE edges. With the fence the single repo-wide retract
// runs once (via the refresh intent) and per-edge writes are held until it
// completes, so no partition wipes another's edges. Cycling the partitions in
// order until the backlog drains models the runner.
func TestProcessPartitionOnceHandlesRouteFenceConverges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 15, 0, 0, 0, time.UTC)
	const partitionCount = 3
	const repoID = "repo-a"

	intents := handlesRouteRepoIntents(t, repoID, partitionCount, partitionCount, now.Add(-time.Minute))
	store := newFenceTrackingIntentStore(intents)
	edges := newStateModelingEdgeWriter()
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	// Drain the backlog: repeat full passes over every partition until a pass
	// makes no progress. The refresh fence may defer per-edge rows for a cycle
	// until the refresh partition completes, so convergence can take >1 pass.
	for pass := 0; pass < partitionCount+2; pass++ {
		progressed := false
		for p := 0; p < partitionCount; p++ {
			result, err := ProcessPartitionOnce(
				context.Background(), now, handlesRouteFenceConfig(p, partitionCount),
				lease, store, edges, acceptedGen, nil, readiness, nil, nil, store, nil,
			)
			if err != nil {
				t.Fatalf("pass %d partition %d: %v", pass, p, err)
			}
			if result.ProcessedIntents > 0 {
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}

	if got := edges.edgeCount(repoID); got != partitionCount {
		t.Fatalf("repo %q retained %d HANDLES_ROUTE edges, want %d; the refresh fence must keep every partition's edge (#2898/#2910)",
			repoID, got, partitionCount)
	}
}

// TestProcessPartitionOnceHandlesRouteFenceHoldsWritesBeforeRetract proves the
// ordering invariant directly: when per-edge partitions are processed BEFORE the
// refresh partition, the fence DEFERS every per-edge write (nothing is written
// before the repo-wide retract); once the refresh partition commits the retract,
// the deferred rows write and all edges survive. This is the concurrency case a
// run-history "skip duplicate retract" guard cannot satisfy.
func TestProcessPartitionOnceHandlesRouteFenceHoldsWritesBeforeRetract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 16, 0, 0, 0, time.UTC)
	const partitionCount = 4
	const repoID = "repo-b"

	intents := handlesRouteRepoIntents(t, repoID, 3, partitionCount, now.Add(-time.Minute))
	refreshPartition, err := PartitionForKey(repoWideRetractRefreshPartitionKey(DomainHandlesRoute, repoID), partitionCount)
	if err != nil {
		t.Fatalf("PartitionForKey: %v", err)
	}

	store := newFenceTrackingIntentStore(intents)
	edges := newStateModelingEdgeWriter()
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	process := func(p int) PartitionProcessResult {
		result, procErr := ProcessPartitionOnce(
			context.Background(), now, handlesRouteFenceConfig(p, partitionCount),
			lease, store, edges, acceptedGen, nil, readiness, nil, nil, store, nil,
		)
		if procErr != nil {
			t.Fatalf("partition %d: %v", p, procErr)
		}
		return result
	}

	// Process every NON-refresh partition first: with no committed retract, every
	// per-edge row must be deferred and nothing may be written yet.
	deferredSeen := 0
	for p := 0; p < partitionCount; p++ {
		if p == refreshPartition {
			continue
		}
		deferredSeen += process(p).RefreshFenceDeferred
	}
	if edges.edgeCount(repoID) != 0 {
		t.Fatalf("repo %q has %d edges before the retract committed, want 0; the fence must hold writes", repoID, edges.edgeCount(repoID))
	}
	if deferredSeen == 0 {
		t.Fatalf("expected per-edge rows to be deferred before the refresh committed, got 0")
	}

	// Commit the refresh (repo-wide retract), then drain remaining partitions.
	process(refreshPartition)
	for pass := 0; pass < partitionCount; pass++ {
		for p := 0; p < partitionCount; p++ {
			if p == refreshPartition {
				continue
			}
			process(p)
		}
	}

	if got := edges.edgeCount(repoID); got != 3 {
		t.Fatalf("repo %q retained %d edges after refresh + drain, want 3", repoID, got)
	}
}

// TestProcessPartitionOnceHandlesRouteNilFencePreservesLegacyBehavior locks the
// nil-fence contract: with no fence wired, a repo-wide-retract domain keeps its
// pre-#2898 per-partition repo-wide retract byte-identical — which is exactly the
// #2910 race (only the last-processed partition's edge survives). This is why the
// runner MUST wire the fence; it also guards that the new code path is inert when
// the fence is absent.
func TestProcessPartitionOnceHandlesRouteNilFencePreservesLegacyBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 17, 0, 0, 0, time.UTC)
	const partitionCount = 3
	const repoID = "repo-c"

	// Per-edge rows only (no refresh intent), matching pre-#2898 emission.
	all := handlesRouteRepoIntents(t, repoID, partitionCount, partitionCount, now.Add(-time.Minute))
	perEdge := all[1:] // drop the refresh intent
	store := newFenceTrackingIntentStore(perEdge)
	edges := newStateModelingEdgeWriter()
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	for p := 0; p < partitionCount; p++ {
		if _, err := ProcessPartitionOnce(
			context.Background(), now, handlesRouteFenceConfig(p, partitionCount),
			lease, store, edges, acceptedGen, nil, readiness, nil, nil, nil, nil, // nil fence → legacy path
		); err != nil {
			t.Fatalf("partition %d: %v", p, err)
		}
	}

	if got := edges.edgeCount(repoID); got != 1 {
		t.Fatalf("nil-fence legacy path retained %d edges, want 1 (the unfixed per-partition repo-wide retract); the fence is load-bearing", got)
	}
}

// TestProcessPartitionOnceHandlesRouteUnmarkedRowsDrainWhenFenceWired proves the
// deploy-transition contract: per-edge rows emitted BEFORE #2898 (no
// retract_via_refresh marker, no paired refresh intent) must NOT be deferred
// forever once the fence is wired — they keep the legacy per-partition retract
// path and drain (complete) so the backlog never wedges. Such rows are superseded
// by the next re-ingest's marked, fenced rows.
func TestProcessPartitionOnceHandlesRouteUnmarkedRowsDrainWhenFenceWired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 18, 0, 0, 0, time.UTC)
	const partitionCount = 3
	const repoID = "repo-d"

	// Pre-#2898 emission: per-edge rows with no marker and no refresh intent.
	var intents []SharedProjectionIntentRow
	for p := 0; p < partitionCount; p++ {
		intents = append(intents, BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: DomainHandlesRoute,
			PartitionKey:     partitionKeyForTestPartition(t, p, partitionCount, repoID+"-legacy-"+strconv.Itoa(p)),
			ScopeID:          "scope-a",
			AcceptanceUnitID: repoID,
			RepositoryID:     repoID,
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"repo_id": repoID, "action": "upsert"},
			CreatedAt:        now.Add(-time.Minute),
		}))
	}
	store := newFenceTrackingIntentStore(intents)
	edges := newStateModelingEdgeWriter()
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	totalDeferred := 0
	for p := 0; p < partitionCount; p++ {
		result, err := ProcessPartitionOnce(
			context.Background(), now, handlesRouteFenceConfig(p, partitionCount),
			lease, store, edges, acceptedGen, nil, readiness, nil, nil, store, nil,
		)
		if err != nil {
			t.Fatalf("partition %d: %v", p, err)
		}
		totalDeferred += result.RefreshFenceDeferred
	}

	if totalDeferred != 0 {
		t.Fatalf("unmarked legacy rows deferred %d times with the fence wired; they must drain, not stall", totalDeferred)
	}
	for _, intent := range intents {
		if _, done := store.completed[intent.IntentID]; !done {
			t.Fatalf("legacy row %q did not complete with the fence wired; the backlog would wedge", intent.IntentID)
		}
	}
}
