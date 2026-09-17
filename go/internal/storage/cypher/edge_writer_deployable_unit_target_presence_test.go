// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func validDeployableUnitRow() reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     "i-du-1",
		RepositoryID: "repo-app",
		GenerationID: "gen-1",
		Payload: map[string]any{
			"repo_id":             "repo-app",
			"deployment_repo_id":  "repo-deploy",
			"deployable_unit_key": "du-1",
			"correlation_key":     "corr-1",
			"confidence":          0.9,
			"reason":              "test",
		},
	}
}

// TestEdgeWriterWriteEdgesDeployableUnitAbsentTargetFailsClosed is the #6184
// shard-3 regression: a CORRELATES_DEPLOYABLE_UNIT batch whose deployment
// Repository node is absent from the graph must fail retryably instead of
// completing with a silent zero-edge write. The template MATCHes both
// endpoint Repositories; the deployment node is committed by another scope's
// materialization with no happens-before against this batch, so a drain that
// reaches the write first binds nothing, the worker completes the intent,
// and the edge is lost with no error and no dead letter — while the write
// path counts the row written.
func TestEdgeWriterWriteEdgesDeployableUnitAbsentTargetFailsClosed(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: false}
	writer := NewEdgeWriter(executor, 0)

	_, err := writer.WriteEdges(
		context.Background(), reducer.DomainDeployableUnitEdges,
		[]reducer.SharedProjectionIntentRow{validDeployableUnitRow()},
		"reducer/deployable-unit-correlation",
	)
	if err == nil {
		t.Fatal("WriteEdges() with an absent deployment repo target succeeded silently, want a retryable error so the batch re-runs after materialization commits the target")
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want a retryable error so the rows stay queued", err)
	}
	if executor.executeCalls != 0 {
		t.Fatalf("executor writes = %d, want 0: no edge statement may run when a batch target is absent", executor.executeCalls)
	}
	if executor.probeCalls == 0 {
		t.Fatal("target existence probe was never consulted")
	}
}

// TestEdgeWriterDeployableUnitProbeAnchorsBothRepos pins the probe shape the
// guard relies on: the miss-detection statement must anchor both MATCH
// endpoints of the deployable-unit template (source AND deployment repo),
// stay read-only, and carry exactly this batch's targets.
func TestEdgeWriterDeployableUnitProbeAnchorsBothRepos(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{
		"repo_id":            "repo-app",
		"deployment_repo_id": "repo-deploy",
	}}
	probe, ok := buildTargetPresenceProbeStatement(reducer.DomainDeployableUnitEdges, rows)
	if !ok {
		t.Fatal("buildTargetPresenceProbeStatement(deployable_unit_edges) = false, want a probe anchoring both Repository endpoints")
	}
	for _, want := range []string{"(:Repository {id: $", "RETURN 1 LIMIT 1"} {
		if !strings.Contains(probe.Cypher, want) {
			t.Fatalf("probe cypher missing %q:\n%s", want, probe.Cypher)
		}
	}
	for _, forbidden := range []string{"MERGE", "CREATE", "DELETE", "SET ", "OPTIONAL", "WITH", "WHERE", "COUNT", "UNWIND"} {
		if strings.Contains(probe.Cypher, forbidden) {
			t.Fatalf("probe cypher must be anchored MATCHes only, found %q:\n%s", forbidden, probe.Cypher)
		}
	}
	found := map[string]bool{}
	for _, v := range probe.Parameters {
		if s, ok := v.(string); ok {
			found[s] = true
		}
	}
	for _, want := range []string{"repo-app", "repo-deploy"} {
		if !found[want] {
			t.Fatalf("probe parameters missing target %q: %v", want, probe.Parameters)
		}
	}
}

// TestEdgeWriterWriteEdgesDeployableUnitTargetPresentProceeds guards the
// happy path: when both batch targets exist, the probe must not cost the
// write anything.
func TestEdgeWriterWriteEdgesDeployableUnitTargetPresentProceeds(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: true}
	writer := NewEdgeWriter(executor, 0)

	if _, err := writer.WriteEdges(
		context.Background(), reducer.DomainDeployableUnitEdges,
		[]reducer.SharedProjectionIntentRow{validDeployableUnitRow()},
		"reducer/deployable-unit-correlation",
	); err != nil {
		t.Fatalf("WriteEdges() with all targets present error = %v", err)
	}
	if executor.executeCalls == 0 {
		t.Fatal("expected the edge write to run when all targets are present")
	}
}

// distinctDeployableUnitRows builds n DU rows with pairwise-distinct
// source and deployment repos so no probe deduplication collapses them.
func distinctDeployableUnitRows(n int) []reducer.SharedProjectionIntentRow {
	rows := make([]reducer.SharedProjectionIntentRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, reducer.SharedProjectionIntentRow{
			IntentID:     "i-du-chunk",
			RepositoryID: "repo-src",
			GenerationID: "gen-1",
			Payload: map[string]any{
				"repo_id":             "repo-src",
				"deployment_repo_id":  "repo-deploy-chunk",
				"deployable_unit_key": "du-chunk",
				"correlation_key":     "corr-chunk",
				"confidence":          0.9,
				"reason":              "test",
			},
		})
		// Vary the deployment repo per row; the source repo stays shared
		// (one MATCH) while each deployment repo adds one MATCH clause.
		rows[i].Payload["deployment_repo_id"] = "repo-deploy-" + string(rune('a'+i))
	}
	return rows
}

// TestEdgeWriterDeployableUnitProbeChunksPerWriteBatch pins the #6730 owner
// finding: one existence probe must cover at most one write batch worth of
// rows, so a 10k-row drain cannot build an unbounded MATCH chain the
// backend never proved. With BatchSize 2 and 3 distinct-target rows, the
// guard must probe twice (2+1) instead of once.
func TestEdgeWriterDeployableUnitProbeChunksPerWriteBatch(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: true}
	writer := NewEdgeWriter(executor, 2)

	rows := distinctDeployableUnitRows(3)
	if _, err := writer.WriteEdges(
		context.Background(), reducer.DomainDeployableUnitEdges,
		rows,
		"reducer/deployable-unit-correlation",
	); err != nil {
		t.Fatalf("WriteEdges() with all targets present error = %v", err)
	}
	if executor.probeCalls != 2 {
		t.Fatalf("probe calls = %d, want 2 (chunks of 2+1 at BatchSize 2)", executor.probeCalls)
	}
	for i, stmt := range executor.probeStmts {
		if got := strings.Count(stmt.Cypher, "MATCH "); got > 3 {
			t.Fatalf("probe %d has %d MATCH clauses, want at most 3 (one write batch of 2 rows: shared source + 2 targets)", i, got)
		}
	}
}
