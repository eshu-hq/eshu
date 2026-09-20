// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const batchCanonicalWorkloadDependencyUnkeyedCypher = `UNWIND $rows AS row
MATCH (source:Workload {id: row.workload_id})
MATCH (target:Workload {id: row.target_workload_id})
MERGE (source)-[rel:DEPENDS_ON]->(target)
SET rel.confidence = 0.9,
    rel.reason = 'Runtime services list declares workload dependency',
    rel.evidence_source = row.evidence_source`

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

// TestBoltWorkloadDependencyRebuildReplacesLegacyIdentity proves the upgrade
// boundary: the domain retract removes old propertyless relationships before
// replay writes the deterministic keyed identity.
func TestBoltWorkloadDependencyRebuildReplacesLegacyIdentity(t *testing.T) {
	runner := openRunsOnBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { runner.close(context.Background()) })

	sourceID := fmt.Sprintf("pr6634-workload-dependency-legacy-source-%d", time.Now().UTC().UnixNano())
	targetID := fmt.Sprintf("pr6634-workload-dependency-legacy-target-%d", time.Now().UTC().UnixNano())
	repoID := "pr6634-workload-dependency-legacy-repo-" + sourceID
	seedWorkloadDependencyNodesForRepo(t, ctx, runner, sourceID, targetID, repoID)
	t.Cleanup(func() { cleanupWorkloadDependencyNodes(t, runner, sourceID, targetID) })
	if err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
MATCH (source:Workload {id: $source_id})
MATCH (target:Workload {id: $target_id})
CREATE (source)-[:DEPENDS_ON {evidence_source: $evidence_source}]->(target)
CREATE (source)-[:DEPENDS_ON {evidence_source: $evidence_source}]->(target)`, Parameters: map[string]any{
		"source_id":       sourceID,
		"target_id":       targetID,
		"evidence_source": reducer.EvidenceSourceWorkloads,
	}}); err != nil {
		t.Fatalf("seed legacy workload dependency edges: %v", err)
	}

	executor := runsOnBoltExecutor{runner: runner}
	writer := NewEdgeWriter(executor, 1)
	if err := writer.RetractEdges(ctx, reducer.DomainWorkloadDependency, []reducer.SharedProjectionIntentRow{{
		IntentID:     "pr6634-workload-dependency-legacy-retract",
		RepositoryID: repoID,
	}}, reducer.EvidenceSourceWorkloads); err != nil {
		t.Fatalf("retract legacy workload dependency identities: %v", err)
	}
	if rows := readWorkloadDependencyRows(t, ctx, runner, sourceID, targetID); len(rows) != 0 {
		t.Fatalf("legacy workload dependency rows after retract = %#v, want none", rows)
	}
	if err := writeWorkloadDependency(ctx, executor, sourceID, targetID, "legacy-replay"); err != nil {
		t.Fatalf("replay workload dependency identity: %v", err)
	}
	rows := readWorkloadDependencyRows(t, ctx, runner, sourceID, targetID)
	if len(rows) != 1 || rows[0]["identity_key"] != "canonical" ||
		rows[0]["evidence_source"] != reducer.EvidenceSourceWorkloads {
		t.Fatalf("replayed workload dependency rows = %#v, want one canonical current edge", rows)
	}
}

// TestBoltWorkloadDependencyBatchShapeTiming compares the pre-fix
// statement with the exact production keyed MERGE at the
// default 500-row edge batch. It is a live evidence test, not a CI latency
// assertion: backend timings are logged while correctness and cardinality are
// enforced for every paired trial.
func TestBoltWorkloadDependencyBatchShapeTiming(t *testing.T) {
	const (
		batchSize  = sourcecypher.DefaultBatchSize
		trialCount = 5
	)
	runner := openRunsOnBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	t.Cleanup(func() { runner.close(context.Background()) })

	before := make([]time.Duration, 0, trialCount)
	after := make([]time.Duration, 0, trialCount)
	for trial := range trialCount {
		shapes := []struct {
			name      string
			keyed     bool
			durations *[]time.Duration
		}{
			{name: "before", durations: &before},
			{name: "after", keyed: true, durations: &after},
		}
		if trial%2 == 1 {
			shapes[0], shapes[1] = shapes[1], shapes[0]
		}
		for _, shape := range shapes {
			nonce := fmt.Sprintf("%d-%d-%s", time.Now().UTC().UnixNano(), trial, shape.name)
			rows, ids := workloadDependencyBatchRows(nonce, batchSize)
			seedWorkloadDependencyBatch(t, ctx, runner, rows)

			statements := []sourcecypher.Statement{{
				Cypher:     batchCanonicalWorkloadDependencyUnkeyedCypher,
				Parameters: map[string]any{"rows": rows},
			}}
			if shape.keyed {
				statements = sourcecypher.BuildEdgeRouteStatements(
					sourcecypher.BatchCanonicalWorkloadDependencyUpsertCypher,
					rows,
					batchSize,
				)
			}
			started := time.Now()
			if err := executeRunsOnGroup(ctx, runner, statements, nil); err != nil {
				t.Fatalf("%s workload dependency batch trial %d: %v", shape.name, trial+1, err)
			}
			duration := time.Since(started)
			*shape.durations = append(*shape.durations, duration)
			assertWorkloadDependencyBatch(t, ctx, runner, rows, shape.keyed)
			cleanupWorkloadDependencyBatch(t, ctx, runner, ids)
		}
	}

	t.Logf("workload dependency batch timings: rows=%d trials=%d before=%v median=%s after=%v median=%s",
		batchSize, trialCount, before, medianDuration(before), after, medianDuration(after))
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
	err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
	CREATE (source:Workload {id: $source_id})
	CREATE (target:Workload {id: $target_id})`, Parameters: map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
	}})
	if err != nil {
		t.Fatalf("seed workload dependency nodes: %v", err)
	}
}

func seedWorkloadDependencyNodesForRepo(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	sourceID, targetID, repoID string,
) {
	t.Helper()
	err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
	CREATE (source:Workload {id: $source_id, repo_id: $repo_id})
	CREATE (target:Workload {id: $target_id})`, Parameters: map[string]any{
		"source_id": sourceID,
		"target_id": targetID,
		"repo_id":   repoID,
	}})
	if err != nil {
		t.Fatalf("seed repository-scoped workload dependency nodes: %v", err)
	}
}

func workloadDependencyBatchRows(nonce string, count int) ([]map[string]any, []string) {
	rows := make([]map[string]any, 0, count)
	ids := make([]string, 0, count*2)
	for index := range count {
		sourceID := fmt.Sprintf("pr6634-dependency-perf-%s-source-%d", nonce, index)
		targetID := fmt.Sprintf("pr6634-dependency-perf-%s-target-%d", nonce, index)
		rows = append(rows, map[string]any{
			"workload_id":        sourceID,
			"target_workload_id": targetID,
			"evidence_source":    reducer.EvidenceSourceWorkloads,
		})
		ids = append(ids, sourceID, targetID)
	}
	return rows, ids
}

func seedWorkloadDependencyBatch(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	rows []map[string]any,
) {
	t.Helper()
	if err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
UNWIND $rows AS row
CREATE (source:Workload {id: row.workload_id})
CREATE (target:Workload {id: row.target_workload_id})`, Parameters: map[string]any{
		"rows": rows,
	}}); err != nil {
		t.Fatalf("seed workload dependency batch: %v", err)
	}
}

func assertWorkloadDependencyBatch(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	rows []map[string]any,
	keyed bool,
) {
	t.Helper()
	got, err := runner.runCypher(ctx, `
UNWIND $rows AS row
MATCH (:Workload {id: row.workload_id})-[rel:DEPENDS_ON]->(:Workload {id: row.target_workload_id})
RETURN rel.identity_key AS identity_key`, map[string]any{"rows": rows})
	if err != nil {
		t.Fatalf("read workload dependency batch: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("workload dependency batch rows = %d, want %d", len(got), len(rows))
	}
	wantIdentity := any(nil)
	if keyed {
		wantIdentity = "canonical"
	}
	for index, row := range got {
		if row["identity_key"] != wantIdentity {
			t.Fatalf("workload dependency batch row %d identity_key = %#v, want %#v",
				index, row["identity_key"], wantIdentity)
		}
	}
}

func cleanupWorkloadDependencyBatch(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	ids []string,
) {
	t.Helper()
	if err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
MATCH (n:Workload)
WHERE n.id IN $ids
DETACH DELETE n`, Parameters: map[string]any{"ids": ids}}); err != nil {
		t.Fatalf("clean workload dependency batch: %v", err)
	}
	assertWorkloadDependencyNodesAbsent(t, ctx, runner, ids)
}

func medianDuration(durations []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	return sorted[len(sorted)/2]
}

func cleanupWorkloadDependencyNodes(
	t *testing.T,
	runner *boltRetractTestRunner,
	sourceID, targetID string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: `
MATCH (n:Workload)
WHERE n.id IN $ids
DETACH DELETE n`, Parameters: map[string]any{
		"ids": []string{sourceID, targetID},
	}}); err != nil {
		t.Errorf("clean workload dependency nodes: %v", err)
		return
	}
	assertWorkloadDependencyNodesAbsent(t, ctx, runner, []string{sourceID, targetID})
}

func assertWorkloadDependencyNodesAbsent(
	t *testing.T,
	ctx context.Context,
	runner *boltRetractTestRunner,
	ids []string,
) {
	t.Helper()
	rows, err := runner.runCypher(ctx, `
MATCH (n:Workload)
WHERE n.id IN $ids
RETURN n.id AS id`, map[string]any{"ids": ids})
	if err != nil {
		t.Errorf("verify workload dependency node cleanup: %v", err)
		return
	}
	if len(rows) != 0 {
		t.Errorf("workload dependency nodes remain after cleanup: %#v", rows)
	}
}
