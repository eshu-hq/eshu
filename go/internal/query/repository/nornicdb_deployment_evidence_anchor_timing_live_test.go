// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Timing probe for #6811. It measures ONE statement shape per process so the
// harness can give every shape its own fresh container: NornicDB keeps
// executing a statement after the client gives up, so a timed-out run poisons
// every later measurement in the same container. The probe applies Eshu's
// NornicDB schema first (the uniqueness constraint alone creates no index on
// this build), seeds the #6811 fixture at ESHU_6811_FILLER, then runs the
// selected shape ESHU_6811_RUNS times against the hub with a varying unused
// $nonce parameter (the result cache otherwise answers repeats instantly) and
// a 60 s client timeout. Durations are logged, never asserted.
//
//	ESHU_NEO4J_URI=bolt://127.0.0.1:27691 ESHU_6811_TIME_SHAPE=old-incoming \
//	  ESHU_6811_FILLER=3000 go test ./internal/query/repository \
//	  -tags live_nornicdb_answer_truth -run TestLiveNornicDBDeploymentEvidenceAnchorTiming -count=1 -v -timeout 30m
package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	deployEvidenceAnchorShapeEnv   = "ESHU_6811_TIME_SHAPE"
	deployEvidenceAnchorRunsEnv    = "ESHU_6811_RUNS"
	deployEvidenceAnchorRunTimeout = 60 * time.Second
)

// deployEvidenceSchemaExecutor adapts the live driver to graph.CypherExecutor
// so the probe can apply the production NornicDB schema before seeding.
type deployEvidenceSchemaExecutor struct {
	driver neo4jdriver.DriverWithContext
}

func (e deployEvidenceSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, e.driver, stmt.Cypher, stmt.Parameters,
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase("nornic"))
	return err
}

// deployEvidenceAnchorShapes maps the harness shape names to the statement
// under test and the parameters it needs for the hub.
func deployEvidenceAnchorShapes(seed deployEvidenceAnchorSeed) map[string]struct {
	cypher string
	params map[string]any
} {
	artifactIDs := make([]string, 0, len(seed.srcIDs))
	for i := range seed.srcIDs {
		artifactIDs = append(artifactIDs, fmt.Sprintf("%sart-hub-%04d", deployEvidenceAnchorPrefix, i))
	}
	sort.Strings(artifactIDs)
	incoming := map[string]any{"repo_id": seed.hubID, "limit": 1000}
	flux := map[string]any{"repo_id": seed.hubID, "artifact_ids": artifactIDs, "source_repo_ids": seed.srcIDs}
	return map[string]struct {
		cypher string
		params map[string]any
	}{
		"old-incoming":       {deployEvidenceAnchorOldIncoming, incoming},
		"candidate-incoming": {deployEvidenceAnchorCandidateIncoming, incoming},
		"old-flux":           {deployEvidenceAnchorShippedFlux, flux},
		"candidate-flux":     {deployEvidenceAnchorCandidateFlux, flux},
	}
}

func TestLiveNornicDBDeploymentEvidenceAnchorTiming(t *testing.T) {
	shape := strings.TrimSpace(os.Getenv(deployEvidenceAnchorShapeEnv))
	if shape == "" {
		t.Skipf("%s not set; this is the #6811 timing probe, not a correctness test", deployEvidenceAnchorShapeEnv)
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	runs := 3
	if raw := strings.TrimSpace(os.Getenv(deployEvidenceAnchorRunsEnv)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			t.Fatalf("%s=%q is not a positive integer", deployEvidenceAnchorRunsEnv, raw)
		}
		runs = parsed
	}
	fillerCount := deployEvidenceAnchorFillerCount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(ctx, deployEvidenceSchemaExecutor{driver: driver}, logger, graph.SchemaBackendNornicDB); err != nil {
		t.Fatalf("apply NornicDB schema: %v", err)
	}

	reader := repoLiveReader{driver: driver}
	reader.write(ctx, t, deployEvidenceAnchorCleanup)
	seedStart := time.Now()
	seed := seedDeployEvidenceAnchor(ctx, t, reader, fillerCount)
	// Best-effort cleanup with its own short bound: after a TIMEOUT the
	// container is still executing the runaway statement and the harness
	// discards it, so a cleanup that cannot finish must not hold the process.
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = neo4jdriver.ExecuteQuery(cleanupCtx, driver, deployEvidenceAnchorCleanup, nil,
			neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase("nornic"))
	}()
	t.Logf("SEED filler=%d seconds=%.1f", fillerCount, time.Since(seedStart).Seconds())

	shapes := deployEvidenceAnchorShapes(seed)
	selected, ok := shapes[shape]
	if !ok {
		t.Fatalf("%s=%q is not one of old-incoming, candidate-incoming, old-flux, candidate-flux", deployEvidenceAnchorShapeEnv, shape)
	}
	for run := 1; run <= runs; run++ {
		params := make(map[string]any, len(selected.params)+1)
		for k, v := range selected.params {
			params[k] = v
		}
		params["nonce"] = fmt.Sprintf("%s-%d-%d", shape, run, time.Now().UnixNano())
		runCtx, runCancel := context.WithTimeout(ctx, deployEvidenceAnchorRunTimeout)
		start := time.Now()
		rows, err := reader.Run(runCtx, selected.cypher, params)
		elapsed := time.Since(start)
		runCancel()
		switch {
		case err != nil && (errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline")):
			t.Logf("TIMING shape=%s filler=%d run=%d ms=TIMEOUT after=%.0f", shape, fillerCount, run, elapsed.Seconds()*1000)
			return // the container is poisoned from here; the harness discards it
		case err != nil:
			t.Fatalf("TIMING shape=%s filler=%d run=%d error=%v", shape, fillerCount, run, err)
		default:
			t.Logf("TIMING shape=%s filler=%d run=%d ms=%.1f rows=%d", shape, fillerCount, run, float64(elapsed.Microseconds())/1000, len(rows))
		}
	}
}
