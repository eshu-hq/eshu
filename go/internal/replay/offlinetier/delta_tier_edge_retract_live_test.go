// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// Edge-retract coverage (C-14 #4367 retract axis, canonical-projector edges). An
// edge whose endpoint entity is retracted is removed with it on a real NornicDB.
// This covers DEFINES_JOB (GitlabPipeline->GitlabJob): the doomed gitlab job
// present in gen1 and absent from gen2 is retracted, and its incoming
// DEFINES_JOB edge goes with it, while the surviving jobs keep their DEFINES_JOB
// edges.
//
// DEFINES_JOB is the only still-uncovered edge type the offline canonical writer
// creates from the delta cassette (verified by probe: CONTAINS and NEEDS are
// already covered; every other retractable edge is reducer-materialized —
// code-call, inheritance, repository-relationship, cloud, IAM, SQL, taint — and
// is not reachable through the offline canonical-writer tier, so those need a
// reducer delta-replay harness tracked separately).
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
	edgeSurvivingJobUID = deltaRepoID + ":gitlab:job:build"
	edgeDoomedJobUID    = deltaRepoID + ":gitlab:job:doomed"
)

// TestDeltaEdgeRetractGraphTruth proves the doomed job's DEFINES_JOB edge is
// present in gen1 and retracted (count=0) after gen2 on a real NornicDB, while a
// surviving job keeps its DEFINES_JOB edge.
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

	definesQ := "MATCH (:GitlabPipeline)-[r:DEFINES_JOB]->(:GitlabJob {uid: $u}) RETURN count(r)"

	if err := writer.Write(ctx, dm.Gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}
	assertEdgeCount(ctx, t, exec, definesQ, map[string]any{"u": edgeDoomedJobUID}, 1, "gen1: doomed DEFINES_JOB present")

	if err := writer.Write(ctx, dm.Gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}
	assertEdgeCount(ctx, t, exec, definesQ, map[string]any{"u": edgeDoomedJobUID}, 0, "gen2: doomed DEFINES_JOB retracted")
	// A surviving DEFINES_JOB edge must remain — scoped retract, not a wipe.
	assertEdgeCount(ctx, t, exec, definesQ, map[string]any{"u": edgeSurvivingJobUID}, 1, "gen2: surviving DEFINES_JOB present")
}

// assertEdgeCount asserts a parameterized edge count query returns want.
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
