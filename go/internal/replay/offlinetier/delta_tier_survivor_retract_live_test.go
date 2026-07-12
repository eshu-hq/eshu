// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// Residual retractable-node coverage (C-14 #4367). GitlabJob and GitlabPipeline
// have surviving instances in the base delta cassette, so a bare-label count
// cannot prove their retract. Each gets a doomed instance in gen1 (absent from
// gen2); this test proves the doomed instance is retracted (count=0) while a
// survivor of the same label remains (count=1), on a real NornicDB — a scoped
// retract, not a wholesale label delete. (File uses a structural file-retract
// path the offline delta tier does not yet drive; it is a tracked follow-up.)
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
	doomedGitlabJobUID     = deltaRepoID + ":gitlab:job:doomed"
	doomedGitlabPipeUID    = deltaRepoID + ":gitlab:pipeline:doomed"
	survivingGitlabJobUID  = deltaRepoID + ":gitlab:job:build"
	survivingGitlabPipeUID = deltaRepoID + ":gitlab:pipeline"
)

// TestDeltaSurvivorScopedRetractGraphTruth proves GitlabJob and GitlabPipeline
// instances present in gen1 and absent from gen2 are retracted to count=0, while
// a same-label survivor remains, on a real NornicDB.
func TestDeltaSurvivorScopedRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the survivor-scoped retract tier against a real NornicDB", liveTierEnv)
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

	if err := writer.Write(ctx, dm.Gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}
	assertScopedCount(ctx, t, exec, "MATCH (n:GitlabJob {uid: $v}) RETURN count(n)", doomedGitlabJobUID, 1, "gen1: doomed GitlabJob present")
	assertScopedCount(ctx, t, exec, "MATCH (n:GitlabPipeline {uid: $v}) RETURN count(n)", doomedGitlabPipeUID, 1, "gen1: doomed GitlabPipeline present")

	if err := writer.Write(ctx, dm.Gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}
	assertScopedCount(ctx, t, exec, "MATCH (n:GitlabJob {uid: $v}) RETURN count(n)", doomedGitlabJobUID, 0, "gen2: doomed GitlabJob retracted")
	assertScopedCount(ctx, t, exec, "MATCH (n:GitlabPipeline {uid: $v}) RETURN count(n)", doomedGitlabPipeUID, 0, "gen2: doomed GitlabPipeline retracted")
	// Same-label survivors must remain — this is a scoped retract, not a label
	// wipe — for both labels the doomed instances belong to.
	assertScopedCount(ctx, t, exec, "MATCH (n:GitlabJob {uid: $v}) RETURN count(n)", survivingGitlabJobUID, 1, "gen2: surviving GitlabJob present")
	assertScopedCount(ctx, t, exec, "MATCH (n:GitlabPipeline {uid: $v}) RETURN count(n)", survivingGitlabPipeUID, 1, "gen2: surviving GitlabPipeline present")
}

// assertScopedCount asserts a single-parameter count query returns want.
func assertScopedCount(ctx context.Context, t *testing.T, exec liveExecutor, cypherText, value string, want int64, msg string) {
	t.Helper()
	got, err := exec.count(ctx, cypherText, map[string]any{"v": value})
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	if got != want {
		t.Fatalf("%s: count = %d, want %d", msg, got, want)
	}
}
