// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// Edge-retract coverage (C-14 #4367 retract axis, canonical-projector edges).
// This proves DIRECT DEFINES_JOB edge retraction between endpoints that both
// survive — the same standard the CONTAINS/NEEDS coverage holds — not an edge
// vanishing because an endpoint was deleted.
//
// A "mover" gitlab job lives in pipeline A's file (.gitlab-ci-a.yml) in gen1 and
// moves to pipeline B's file (.gitlab-ci-b.yml) in gen2. Pipeline A keeps a
// "stayer" job in its file across both generations, so A remains a DEFINES_JOB
// source in gen2 and its edges are reconciled (the retract path only touches
// source pipelines that still define a job). Both pipelines and the mover job
// survive; only the DEFINES_JOB relationship changes. The test asserts
// DEFINES_JOB(pipelineA -> mover) is present in gen1 and retracted in gen2 while
// every node survives, and DEFINES_JOB(pipelineB -> mover) is created in gen2.
// This is the retractGitlabDefinesJobEdges direct-retract path.
//
// DEFINES_JOB is the only still-uncovered edge type the offline canonical writer
// creates (CONTAINS/NEEDS already covered). Every other retractable edge type is
// reducer-materialized (code-call, inheritance, repository-relationship, cloud,
// IAM, SQL, taint) and is not reachable through the offline canonical-writer
// tier, so those need a reducer delta-replay harness, tracked separately.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
)

const (
	edgePipelineOneUID = deltaRepoID + ":gitlab:pipeline:a"
	edgePipelineTwoUID = deltaRepoID + ":gitlab:pipeline:b"
	edgeMoverJobUID    = deltaRepoID + ":gitlab:job:mover"
)

// TestDeltaEdgeRetractGraphTruth proves direct DEFINES_JOB edge retraction
// between surviving endpoints on a real NornicDB.
func TestDeltaEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the edge-retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	cleanupDeltaScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupDeltaScope(cleanCtx, t, exec)
	})

	src := loadDeltaCassette(t)
	gen1, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen1: err=%v ok=%v", err, ok)
	}
	gen2, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen2: err=%v ok=%v", err, ok)
	}
	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("delta materialization: %v", err)
	}

	definesQ := "MATCH (:GitlabPipeline {uid: $p})-[r:DEFINES_JOB]->(:GitlabJob {uid: $j}) RETURN count(r)"
	nodeQ := "MATCH (n {uid: $u}) RETURN count(n)"
	fromOne := map[string]any{"p": edgePipelineOneUID, "j": edgeMoverJobUID}
	fromTwo := map[string]any{"p": edgePipelineTwoUID, "j": edgeMoverJobUID}

	if err := writer.Write(ctx, dm.Gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}
	assertEdgeCount(ctx, t, exec, definesQ, fromOne, 1, "gen1: DEFINES_JOB(pipelineOne->mover) present")
	assertEdgeCount(ctx, t, exec, definesQ, fromTwo, 0, "gen1: DEFINES_JOB(pipelineTwo->mover) absent")

	if err := writer.Write(ctx, dm.Gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}
	// Direct edge retraction: the mover and both pipelines survive, only the edge moved.
	assertEdgeCount(ctx, t, exec, definesQ, fromOne, 0, "gen2: DEFINES_JOB(pipelineOne->mover) retracted")
	assertEdgeCount(ctx, t, exec, definesQ, fromTwo, 1, "gen2: DEFINES_JOB(pipelineTwo->mover) present")
	assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": edgePipelineOneUID}, 1, "gen2: pipelineOne survives")
	assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": edgePipelineTwoUID}, 1, "gen2: pipelineTwo survives")
	assertEdgeCount(ctx, t, exec, nodeQ, map[string]any{"u": edgeMoverJobUID}, 1, "gen2: mover job survives")
}

// assertEdgeCount asserts a parameterized count query returns want.
func assertEdgeCount(ctx context.Context, t *testing.T, exec liveExecutor, cypherText string, params map[string]any, want int64, msg string) {
	t.Helper()
	got, err := exec.count(ctx, cypherText, params)
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	if got != want {
		t.Fatalf("%s: count = %d, want %d", msg, got, want)
	}
}
