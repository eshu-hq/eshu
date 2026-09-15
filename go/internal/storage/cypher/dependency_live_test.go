// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestBoltWorkloadDependencyConcurrentWritersConverge holds one writer's
// transaction open while a second writer publishes the same logical edge.
// Source- and target-scope workload materialization can overlap this way, so
// both successful writes must still leave exactly one DEPENDS_ON relationship.
func TestBoltWorkloadDependencyConcurrentWritersConverge(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { runner.close(context.Background()) })

	nonce := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	sourceID := "pr6634-workload-dependency-source-" + nonce
	targetID := "pr6634-workload-dependency-target-" + nonce
	seedWorkloadDependencyNodes(t, ctx, runner, sourceID, targetID)
	t.Cleanup(func() {
		cleanupWorkloadDependencyNodes(t, runner, sourceID, targetID)
	})

	firstReached := make(chan struct{}, 1)
	firstRelease := make(chan struct{})
	t.Cleanup(func() { closeRunsOnGate(firstRelease) })
	secondAttempted := make(chan struct{}, 1)
	secondReached := make(chan struct{}, 1)
	firstErrors := startWorkloadDependencyWrite(ctx, nil, runsOnBoltExecutor{
		runner: runner,
		groupGate: &runsOnGroupGate{
			cypherContains: "MERGE (source)-[rel:DEPENDS_ON",
			reached:        firstReached,
			release:        firstRelease,
		},
	}, sourceID, targetID, "first")
	awaitRunsOnGate(t, firstReached, "first workload dependency MERGE")
	secondErrors := startWorkloadDependencyWrite(ctx, secondAttempted, runsOnBoltExecutor{
		runner: runner,
		groupGate: &runsOnGroupGate{
			cypherContains: "MERGE (source)-[rel:DEPENDS_ON",
			reached:        secondReached,
		},
	}, sourceID, targetID, "second")
	awaitRunsOnGate(t, secondAttempted, "second workload dependency write attempt")
	observeRunsOnOverlap(t, secondReached, "second workload dependency MERGE")
	var secondErr error
	secondFinished := false
	select {
	case secondErr = <-secondErrors:
		secondFinished = true
		t.Log("second workload dependency write committed before the first transaction was released")
	case <-time.After(2 * time.Second):
		t.Log("second workload dependency write remained blocked until the first transaction was released")
	}
	closeRunsOnGate(firstRelease)

	if err := <-firstErrors; err != nil {
		t.Fatalf("first workload dependency write: %v", err)
	}
	if !secondFinished {
		secondErr = <-secondErrors
	}
	if secondErr != nil {
		t.Fatalf("second workload dependency write: %v", secondErr)
	}
	rows := readWorkloadDependencyRows(t, ctx, runner, sourceID, targetID)
	if len(rows) != 1 {
		t.Fatalf("workload dependency rows = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0]["identity_key"] != "canonical" {
		t.Fatalf("workload dependency identity_key = %#v, want canonical", rows[0]["identity_key"])
	}
}

func TestBoltWorkloadDependencyWriterAdoptsLegacyIdentity(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { runner.close(context.Background()) })

	sourceID := fmt.Sprintf("pr6634-workload-dependency-legacy-source-%d", time.Now().UTC().UnixNano())
	targetID := fmt.Sprintf("pr6634-workload-dependency-legacy-target-%d", time.Now().UTC().UnixNano())
	seedWorkloadDependencyNodes(t, ctx, runner, sourceID, targetID)
	t.Cleanup(func() { cleanupWorkloadDependencyNodes(t, runner, sourceID, targetID) })
	if err := runner.runCypherSingle(ctx, Statement{Cypher: `
MATCH (source:Workload {id: $source_id})
MATCH (target:Workload {id: $target_id})
CREATE (source)-[:DEPENDS_ON {evidence_source: 'legacy-a'}]->(target)
CREATE (source)-[:DEPENDS_ON {evidence_source: 'legacy-b'}]->(target)
CREATE (source)-[:DEPENDS_ON {identity_key: 'canonical', evidence_source: 'stale'}]->(target)`, Parameters: map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
	}}); err != nil {
		t.Fatalf("seed legacy workload dependency edges: %v", err)
	}

	if err := writeWorkloadDependency(ctx, runsOnBoltExecutor{runner: runner}, sourceID, targetID, "legacy"); err != nil {
		t.Fatalf("adopt legacy workload dependency identity: %v", err)
	}
	rows := readWorkloadDependencyRows(t, ctx, runner, sourceID, targetID)
	if len(rows) != 1 || rows[0]["identity_key"] != "canonical" ||
		rows[0]["evidence_source"] != reducer.EvidenceSourceWorkloads {
		t.Fatalf("adopted workload dependency rows = %#v, want one canonical current edge", rows)
	}
}

func TestBoltWorkloadDependencyLegacyCleanupRollsBackWithWrite(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { runner.close(context.Background()) })

	sourceID := fmt.Sprintf("pr6634-workload-dependency-rollback-source-%d", time.Now().UTC().UnixNano())
	targetID := fmt.Sprintf("pr6634-workload-dependency-rollback-target-%d", time.Now().UTC().UnixNano())
	seedWorkloadDependencyNodes(t, ctx, runner, sourceID, targetID)
	t.Cleanup(func() { cleanupWorkloadDependencyNodes(t, runner, sourceID, targetID) })
	if err := runner.runCypherSingle(ctx, Statement{Cypher: `
MATCH (source:Workload {id: $source_id})
MATCH (target:Workload {id: $target_id})
CREATE (source)-[:DEPENDS_ON {evidence_source: 'legacy'}]->(target)`, Parameters: map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
	}}); err != nil {
		t.Fatalf("seed rollback workload dependency edge: %v", err)
	}

	err := writeWorkloadDependency(ctx, runsOnBoltExecutor{
		runner:            runner,
		forceInvalidFinal: true,
	}, sourceID, targetID, "rollback")
	if err == nil {
		t.Fatal("workload dependency write error = nil, want forced keyed-write failure")
	}
	rows := readWorkloadDependencyRows(t, ctx, runner, sourceID, targetID)
	if len(rows) != 1 || rows[0]["identity_key"] != nil || rows[0]["evidence_source"] != "legacy" {
		t.Fatalf("workload dependency rows after rollback = %#v, want original propertyless edge", rows)
	}
}

func startWorkloadDependencyWrite(
	ctx context.Context,
	attempted chan<- struct{},
	executor runsOnBoltExecutor,
	sourceID, targetID, intentSuffix string,
) <-chan error {
	errors := make(chan error, 1)
	go func() {
		notifyRunsOnGate(attempted)
		errors <- writeWorkloadDependency(ctx, executor, sourceID, targetID, intentSuffix)
	}()
	return errors
}

func writeWorkloadDependency(
	ctx context.Context,
	executor runsOnBoltExecutor,
	sourceID, targetID, intentSuffix string,
) error {
	_, err := NewEdgeWriter(executor, 1).WriteEdges(
		ctx,
		reducer.DomainWorkloadDependency,
		[]reducer.SharedProjectionIntentRow{{
			IntentID:     "pr6634-workload-dependency-" + intentSuffix,
			RepositoryID: sourceID,
			Payload: map[string]any{
				"workload_id":        sourceID,
				"target_workload_id": targetID,
			},
		}},
		reducer.EvidenceSourceWorkloads,
	)
	return err
}

func readWorkloadDependencyRows(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	sourceID, targetID string,
) []map[string]any {
	t.Helper()
	rows, err := runner.runCypher(ctx, `
MATCH (:Workload {id: $source_id})-[rel:DEPENDS_ON]->(:Workload {id: $target_id})
RETURN rel.evidence_source AS evidence_source,
       rel.identity_key AS identity_key`, map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
	})
	if err != nil {
		t.Fatalf("read workload dependency edges: %v", err)
	}
	return rows
}

func seedWorkloadDependencyNodes(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	sourceID, targetID string,
) {
	t.Helper()
	err := runner.runCypherSingle(ctx, Statement{Cypher: `
	CREATE (source:Workload {id: $source_id})
	CREATE (target:Workload {id: $target_id})`, Parameters: map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
	}})
	if err != nil {
		t.Fatalf("seed workload dependency nodes: %v", err)
	}
}

func cleanupWorkloadDependencyNodes(
	t *testing.T,
	runner *boltRetractTestRunner,
	sourceID, targetID string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runner.runCypherSingle(ctx, Statement{Cypher: `
MATCH (n:Workload)
WHERE n.id IN $ids
DETACH DELETE n`, Parameters: map[string]any{
		"ids": []string{sourceID, targetID},
	}}); err != nil {
		t.Errorf("clean workload dependency nodes: %v", err)
	}
}
