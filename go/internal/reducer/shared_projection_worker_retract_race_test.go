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
// partition-processing order is what reveals that one partition's repo-wide
// retract deletes another partition's just-written edges within a single cycle.
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

// TestProcessPartitionOnceHandlesRouteMultiEdgeRepoSurvivesAllPartitions is the
// #2910 state-modeling reproduction. A single repo has three HANDLES_ROUTE edges
// whose per-edge partition keys (handles_route_intents.go:96) hash to three
// distinct partitions. The generic worker retracts repo-wide AND writes per
// partition, so processing the partitions in order must leave ALL three edges in
// place. On the unfixed worker only the last-processed partition's edge survives,
// because each partition's repo-wide retract wipes the edges the earlier
// partitions wrote in this same cycle.
func TestProcessPartitionOnceHandlesRouteMultiEdgeRepoSurvivesAllPartitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 15, 0, 0, 0, time.UTC)
	created := now.Add(-time.Minute)
	const partitionCount = 3
	const repoID = "repo-a"

	intents := make([]SharedProjectionIntentRow, 0, partitionCount)
	for p := 0; p < partitionCount; p++ {
		partitionKey := partitionKeyForTestPartition(t, p, partitionCount, "route-"+strconv.Itoa(p))
		intents = append(intents, SharedProjectionIntentRow{
			IntentID:         "route-" + strconv.Itoa(p),
			ProjectionDomain: DomainHandlesRoute,
			PartitionKey:     partitionKey,
			ScopeID:          "scope-a",
			AcceptanceUnitID: repoID,
			RepositoryID:     repoID,
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"repo_id": repoID, "action": "upsert"},
			CreatedAt:        created,
		})
	}

	reader := &stubSharedIntentReader{pending: intents}
	edges := newStateModelingEdgeWriter()
	lease := &stubLeaseManager{claimResult: true}
	acceptedGen := acceptedGenerationFixed("gen-1", true)
	readiness := readinessLookupFixed(true, true)

	for p := 0; p < partitionCount; p++ {
		cfg := PartitionProcessorConfig{
			Domain:         DomainHandlesRoute,
			PartitionID:    p,
			PartitionCount: partitionCount,
			LeaseOwner:     "worker-1",
			LeaseTTL:       30 * time.Second,
			BatchLimit:     100,
			EvidenceSource: handlesRouteEvidenceSource,
		}
		if _, err := ProcessPartitionOnce(
			context.Background(), now, cfg, lease, reader, edges,
			acceptedGen, nil, readiness, nil, nil,
		); err != nil {
			t.Fatalf("ProcessPartitionOnce(partition %d) error = %v", p, err)
		}
	}

	if got := edges.edgeCount(repoID); got != partitionCount {
		t.Fatalf("repo %q retained %d HANDLES_ROUTE edges after all %d partitions, want %d; "+
			"a per-partition repo-wide retract dropped edges (#2910)", repoID, got, partitionCount, partitionCount)
	}
}
