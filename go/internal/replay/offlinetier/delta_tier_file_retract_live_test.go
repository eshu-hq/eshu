// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// File-retract coverage (C-14 #4367). A File tombstoned in gen2 is removed by the
// production delta file retract (canonicalNodeRetractDeltaDeletedFilesCypher) on a
// real NornicDB, while a surviving File remains. This exercises the offline
// delta tier's new tombstoned-file collection: DeltaMaterializationFromGenerations
// now maps gen2 tombstoned git.file facts into DeltaDeletedFilePaths, mirroring
// the directory-tombstone path.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor, cypher-query-rigor.

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
)

const (
	doomedFilePath    = deltaRepoPath + "/doomed.txt"
	survivingFilePath = deltaRepoPath + "/.gitlab-ci.yml"
)

// TestDeltaFileRetractGraphTruth proves a File present in gen1 and tombstoned in
// gen2 is retracted (count=0) on a real NornicDB while a surviving File remains.
func TestDeltaFileRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the file-retract tier against a real NornicDB", liveTierEnv)
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
	if len(dm.TombstonedFilePaths) != 1 || dm.TombstonedFilePaths[0] != doomedFilePath {
		t.Fatalf("TombstonedFilePaths = %v, want [%s]", dm.TombstonedFilePaths, doomedFilePath)
	}

	fileQ := "MATCH (n:File {path: $p}) RETURN count(n)"

	if err := writer.Write(ctx, dm.Gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}
	assertFileCount(ctx, t, exec, fileQ, doomedFilePath, 1, "gen1: doomed File present")
	assertFileCount(ctx, t, exec, fileQ, survivingFilePath, 1, "gen1: surviving File present")

	if err := writer.Write(ctx, dm.Gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}
	assertFileCount(ctx, t, exec, fileQ, doomedFilePath, 0, "gen2: doomed File retracted")
	// Scoped retract: the surviving File must remain, not be wiped.
	assertFileCount(ctx, t, exec, fileQ, survivingFilePath, 1, "gen2: surviving File present")
}

// assertFileCount asserts a File count query returns want.
func assertFileCount(ctx context.Context, t *testing.T, exec liveExecutor, cypherText, path string, want int64, msg string) {
	t.Helper()
	got, err := exec.count(ctx, cypherText, map[string]any{"p": path})
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
	if got != want {
		t.Fatalf("%s: count = %d, want %d", msg, got, want)
	}
}
