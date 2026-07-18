// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Reducer-materialized semantic Variable delta-retract coverage (C-14 #4367
// retract axis): retractable_node:Variable (delta_tombstone).
//
// Variable is the one retractable graph node label whose base node is created
// ONLY by the reducer-owned semantic entity path (issue #5156): the canonical
// content-entity projection deliberately skips it
// (internal/projector/canonical_builder.go), so the content_entity retract
// cassette in delta_tier_entity_retract_live_test.go cannot cover it. This test
// drives the production cypher.SemanticEntityWriter directly: it upserts a
// Variable node (gen1), then delta-retracts it by file path (gen2, the file
// absent from the new generation), and proves the in-scope Variable is gone
// while a same-label Variable in another file survives — a genuine
// create-then-delta-tombstone vehicle for the Variable label.
//
// The writer uses WithSequentialRetract (the NornicDB dispatch), so the delta
// retract runs sequentially through Execute (one autocommit transaction per
// DETACH DELETE, one per semantic label) instead of grouping through
// ExecuteGroup. That is what this PR added for NornicDB: grouped-transaction
// DETACH DELETEs under-apply on the pinned NornicDB v1.1.11 and silently leave
// the Variable node. This test is the failing-then-green regression — it fails
// (Variable survives gen2, count=1) when the grouped-writes NornicDB path
// retracts through ExecuteGroup, and passes (count=0) with sequential retract.
// Neo4j keeps the atomic grouped retract+upsert (default, no WithSequentialRetract).
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	svMarker  = "replay-semantic-variable"
	svRepo    = "replay-semantic-variable:repo"
	svInFile  = "replay-semantic-variable/in/config.py"
	svOutFile = "replay-semantic-variable/out/config.py"
	svInUID   = "replay-semantic-variable:var:in"
	svOutUID  = "replay-semantic-variable:var:out"
	svSource  = "parser/semantic-entities"
)

// TestReducerSemanticVariableRetractGraphTruth proves the Variable
// delta_tombstone path: a Variable created by the semantic writer in gen1 is
// removed by the semantic delta retract in gen2 (its file absent from the new
// generation), while a same-label Variable in an unrelated file survives.
func TestReducerSemanticVariableRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the semantic-variable retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cleanupSemanticVariableScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupSemanticVariableScope(cleanCtx, t, exec)
	})

	// The Variable upsert MATCHes a pre-existing File{path}; seed both files.
	seedStmt := cypher.Statement{
		Cypher: `CREATE (:File {path: $in, marker: $marker}),
       (:File {path: $out, marker: $marker})`,
		Parameters: map[string]any{"in": svInFile, "out": svOutFile, "marker": svMarker},
	}
	if err := exec.Execute(ctx, seedStmt); err != nil {
		t.Fatalf("seed files: %v", err)
	}

	// Production semantic writer for the NornicDB path: canonical-node-rows mode
	// + label-scoped retract + sequential retract, exactly as
	// semanticEntityWriterForGraphBackend wires it for NornicDB (the
	// WithSequentialRetract dispatch is what makes the grouped-DETACH-DELETE
	// under-apply not bite).
	writer := cypher.NewSemanticEntityWriterWithCanonicalNodeRows(exec, 500).
		WithLabelScopedRetract().
		WithSequentialRetract()

	variableRow := func(uid, file string) reducer.SemanticEntityRow {
		return reducer.SemanticEntityRow{
			RepoID:       svRepo,
			EntityID:     uid,
			EntityType:   "Variable",
			EntityName:   "SETTING",
			FilePath:     file,
			RelativePath: file,
			Language:     "python",
			StartLine:    1,
			EndLine:      1,
		}
	}

	// gen1: upsert both Variables (SkipRetract so the first generation only
	// writes). Each connects to its File via CONTAINS.
	if _, err := writer.WriteSemanticEntities(ctx, reducer.SemanticEntityWrite{
		RepoIDs:     []string{svRepo},
		Rows:        []reducer.SemanticEntityRow{variableRow(svInUID, svInFile), variableRow(svOutUID, svOutFile)},
		SkipRetract: true,
	}); err != nil {
		t.Fatalf("gen1 WriteSemanticEntities: %v", err)
	}

	varQ := "MATCH (n:Variable {uid: $u}) RETURN count(n)"
	assertEdgeCount(ctx, t, exec, varQ, map[string]any{"u": svInUID}, 1, "gen1: in-scope Variable present")
	assertEdgeCount(ctx, t, exec, varQ, map[string]any{"u": svOutUID}, 1, "gen1: out-of-scope Variable present")

	// gen2: the in-scope file is absent from the new generation, so the delta
	// projection retracts every semantic entity for that file path. Rows empty,
	// DeltaProjection true, DeltaFilePaths naming only the in-scope file.
	if _, err := writer.WriteSemanticEntities(ctx, reducer.SemanticEntityWrite{
		RepoIDs:         []string{svRepo},
		DeltaProjection: true,
		DeltaFilePaths:  []string{svInFile},
	}); err != nil {
		t.Fatalf("gen2 delta WriteSemanticEntities: %v", err)
	}

	assertEdgeCount(ctx, t, exec, varQ, map[string]any{"u": svInUID}, 0, "gen2: in-scope Variable retracted")
	assertEdgeCount(ctx, t, exec, varQ, map[string]any{"u": svOutUID}, 1, "gen2: out-of-scope Variable survives")
	// The File nodes are not retracted by the semantic delta path.
	fileQ := "MATCH (n:File {path: $p}) RETURN count(n)"
	assertEdgeCount(ctx, t, exec, fileQ, map[string]any{"p": svInFile}, 1, "gen2: in-scope File survives semantic retract")
	assertEdgeCount(ctx, t, exec, fileQ, map[string]any{"p": svOutFile}, 1, "gen2: out-of-scope File survives")
}

// cleanupSemanticVariableScope removes every node these tests create, including
// the write-MERGEd Variable nodes (which carry no marker).
func cleanupSemanticVariableScope(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, stmt := range []cypher.Statement{
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": svMarker},
		},
		{
			Cypher:     `MATCH (n:Variable) WHERE n.uid IN [$in, $out] DETACH DELETE n`,
			Parameters: map[string]any{"in": svInUID, "out": svOutUID},
		},
	} {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup semantic-variable scope: %v", err)
		}
	}
}
