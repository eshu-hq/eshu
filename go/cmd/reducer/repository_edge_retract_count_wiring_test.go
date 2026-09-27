// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/retract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// deleteReportingSession reports a fixed relationship delete count for every
// statement through the same ReportWriteCounts seam neo4jSessionRunner uses
// after a committed Bolt statement.
type deleteReportingSession struct {
	runCypherOnlySession
	deleted int64
}

func (s *deleteReportingSession) RunCypher(ctx context.Context, cypher string, params map[string]any) error {
	if err := s.runCypherOnlySession.RunCypher(ctx, cypher, params); err != nil {
		return err
	}
	sourcecypher.ReportWriteCounts(ctx, cypher, params, sourcecypher.WriteCounters{RelationshipsDeleted: s.deleted})
	return nil
}

// TestProductionWorkloadMaterializerCountsRepositoryEdgeRetracts pins the
// production chain (newReducerCypherExecutor -> boundCypherExecutor ->
// newProbedWorkloadMaterializer -> CypherExecutor) so the #7285 stale repository-edge retract
// reports the relationships it actually deleted, with the graph write gate
// disabled and enabled, and still forwards every statement's counters to an
// outer differential-capture collector.
func TestProductionWorkloadMaterializerCountsRepositoryEdgeRetracts(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"gate disabled": {},
		"gate enabled":  {graphbackpressure.MaxInFlightEnv: "4"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			session := &deleteReportingSession{deleted: 3}
			getenv := func(key string) string { return env[key] }
			cypherExec := newReducerGraphWriteGate(getenv, nil).boundCypherExecutor(
				newReducerCypherExecutor(session, nil, nil),
			)
			outer := sourcecypher.NewWriteCountsCollector()
			ctx := sourcecypher.WithWriteCountsCollector(context.Background(), outer)

			// No reader: the unguarded keep-list path, so both statements run
			// and the counts come from the session's write summaries.
			result, err := retract.RepositoryEdges(ctx, newProbedWorkloadMaterializer(cypherExec, nil, nil).CypherExecutor(), nil, 500,
				[]retract.KeepList{{RepoID: "repository:r_payments", WorkloadIDs: []string{"workload:api"}}},
				reducer.EvidenceSourceWorkloads)
			if err != nil {
				t.Fatalf("RepositoryEdges() error = %v", err)
			}
			if !result.Counted || result.DefinesDeleted != 3 || result.EndpointEdgesDeleted != 3 {
				t.Fatalf("retract result = %+v, want counted with 3 DEFINES and 3 endpoint edges deleted", result)
			}
			if got := len(session.calls); got != 2 {
				t.Fatalf("session statements = %d, want 2", got)
			}
			if got := len(outer.Entries()); got != 2 {
				t.Fatalf("outer capture collector entries = %d, want 2 (the counting seam must not hide them)", got)
			}
		})
	}
}
