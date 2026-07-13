// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// atlantisShapedAllRetractPhase builds the exact statement shape
// atlantisEdgeStatements emits when a later generation keeps the
// AtlantisProject nodes but removes EVERY dir/depends_on/workflow
// relationship: the structural_edges phase then carries ONLY the three
// Drain-marked relationship retracts (empty DrainVar — they are bounded
// UNWIND deletes, not full-refresh DETACH DELETE drains) and no sibling
// MERGE upserts. The GitLab NEEDS retract can produce the same all-retract
// phase on its own (jobs present, no pipeline entity, no needs), so this
// shape is not Atlantis-specific.
func atlantisShapedAllRetractPhase() []sourcecypher.Statement {
	stmts := make([]sourcecypher.Statement, 0, 3)
	for _, cypherText := range []string{
		`UNWIND $source_uids AS uid
MATCH (p:AtlantisProject {uid: uid})-[r:MANAGES]->(:Directory)
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`,
		`UNWIND $source_uids AS uid
MATCH (p:AtlantisProject {uid: uid})-[r:ATLANTIS_DEPENDS_ON]->(:AtlantisProject)
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`,
		`UNWIND $source_uids AS uid
MATCH (p:AtlantisProject {uid: uid})-[r:USES_WORKFLOW]->(:AtlantisWorkflow)
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`,
	} {
		stmts = append(stmts, sourcecypher.Statement{
			Operation:  sourcecypher.OperationCanonicalRetract,
			Cypher:     cypherText,
			Parameters: map[string]any{"source_uids": []string{"proj:1"}, "generation_id": "gen-2"},
			Drain:      true,
		})
	}
	return stmts
}

// TestNornicDBPhaseGroupExecutorAllRetractPhaseRunsEmptyDrainVarAutocommit
// covers the edge case raised in review on #5155: when the structural_edges
// phase contains ONLY Drain-marked relationship retracts (all
// OperationCanonicalRetract), ExecutePhaseGroup routes to
// executeSequentialRetractPhase — not the mixed-phase
// executeGroupedChunksWithDrain path. There, a Drain=true statement with the
// production drainReader wired entered executeDrainLoop, whose
// BuildBoundedRetractDrainCypher rejects the empty DrainVar these bounded
// relationship retracts intentionally carry ("drainVar must not be empty"),
// failing the whole canonical write instead of retracting. A Drain-marked
// statement with an empty DrainVar means "run me as my own autocommit
// statement, no bounded drain loop" everywhere else (see
// executeAutocommitRetract), so the all-retract path must honor the same
// semantics.
func TestNornicDBPhaseGroupExecutorAllRetractPhaseRunsEmptyDrainVarAutocommit(t *testing.T) {
	t.Parallel()

	// drainReader wired exactly as cmd/ingester production wiring threads
	// rawExecutor: this is what makes the drain-loop branch reachable.
	reader := &drainCountReader{counts: []int64{0, 0, 0}}
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      reader,
	}

	stmts := atlantisShapedAllRetractPhase()

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil (empty-DrainVar relationship retracts must run autocommit, not enter the bounded drain loop)", err)
	}

	// Each retract ran exactly once as its own autocommit statement through
	// the RunWrite reader (the executeAutocommitRetract route), preserving the
	// drift-retract telemetry the reader result feeds.
	if reader.callIdx != len(stmts) {
		t.Fatalf("autocommit RunWrite calls = %d, want %d (one per retract statement)", reader.callIdx, len(stmts))
	}
	// Never grouped: a grouped relationship DELETE is the no-op #4476 guards
	// against.
	if inner.callCount != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 (retracts must never group)", inner.callCount)
	}
}

// TestNornicDBPhaseGroupExecutorAllRetractPhaseEmptyDrainVarNilReader proves
// the same all-retract empty-DrainVar phase still runs (via plain inner
// Execute) when no RunWrite-capable drainReader is wired, mirroring
// executeAutocommitRetract's nil-reader fallback.
func TestNornicDBPhaseGroupExecutorAllRetractPhaseEmptyDrainVarNilReader(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 2000,
		drainReader:      nil,
	}

	stmts := atlantisShapedAllRetractPhase()

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got := len(inner.executeStatements); got != len(stmts) {
		t.Fatalf("inner Execute calls = %d, want %d (one autocommit per retract)", got, len(stmts))
	}
	if inner.callCount != 0 {
		t.Fatalf("ExecuteGroup calls = %d, want 0 (retracts must never group)", inner.callCount)
	}
}

// TestNornicDBPhaseGroupExecutorAllRetractPhaseKeepsDrainLoopForDrainVar
// guards the other side of the split: a Drain-marked statement WITH a
// DrainVar (an unbounded full-refresh DETACH DELETE, e.g. the canonical file
// retract) must still take the bounded drain loop in an all-retract phase,
// not the single-shot autocommit route.
func TestNornicDBPhaseGroupExecutorAllRetractPhaseKeepsDrainLoopForDrainVar(t *testing.T) {
	t.Parallel()

	// Two drain iterations: first deletes a full batch, second returns 0.
	reader := &drainCountReader{counts: []int64{5, 0}}
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:            inner,
		maxStatements:    100,
		retractBatchSize: 5,
		drainReader:      reader,
	}

	stmts := []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher: `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
DETACH DELETE f`,
		Parameters: map[string]any{"repo_id": "repo-1", "generation_id": "gen-2"},
		Drain:      true,
		DrainVar:   "f",
	}}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	// Drain loop: one RunWrite per iteration until __drained == 0.
	if reader.callIdx != 2 {
		t.Fatalf("drain-loop RunWrite calls = %d, want 2 (batch then terminating zero)", reader.callIdx)
	}
	if got := len(inner.executeStatements); got != 0 {
		t.Fatalf("inner Execute calls = %d, want 0 (DrainVar statements take the drain loop, not plain Execute)", got)
	}
}
