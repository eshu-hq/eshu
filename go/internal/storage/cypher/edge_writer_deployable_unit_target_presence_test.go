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
