// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestIngesterNornicDBProbedDrainLiveKeepsCurrentGeneration drives the real
// ingester NornicDB executor over the production bare-label entity retract
// shape (#6822). It proves the probed drain deletes every stale node across
// several batches, keeps every current-generation node, and that a retract
// with nothing to delete leaves the graph unchanged.
//
// Run against a disposable NornicDB, for example:
//
//	ESHU_INGESTER_DRAIN_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:17687 \
//	  go test ./cmd/ingester -run ProbedDrainLive -count=1 -v
func TestIngesterNornicDBProbedDrainLiveKeepsCurrentGeneration(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_INGESTER_DRAIN_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_INGESTER_DRAIN_PROVE_LIVE=1 to run the #6822 live probed drain proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "nornic"
	}

	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open bolt driver %q: %v", uri, err)
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = driver.Close(ctx) })
	verifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(verifyCtx); err != nil {
		t.Fatalf("verify bolt connectivity %q: %v", uri, err)
	}

	const (
		label    = "Eshu6822ProbedDrain"
		repoID   = "repository:6822-probed-live"
		oldGen   = "gen-1"
		newGen   = "gen-2"
		stale    = 130
		current  = 25
		batchCap = 50 // 130 stale rows -> three delete batches
	)
	run := func(cypher string, params map[string]any) {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
		defer func() { _ = session.Close(ctx) }()
		if _, err := session.Run(ctx, cypher, params); err != nil {
			t.Fatalf("run %q: %v", cypher, err)
		}
	}
	count := func(generation string) int64 {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead, DatabaseName: database})
		defer func() { _ = session.Close(ctx) }()
		res, err := session.Run(ctx,
			"MATCH (n:"+label+") WHERE n.repo_id = $repo_id AND n.generation_id = $generation_id RETURN count(n) AS count",
			map[string]any{"repo_id": repoID, "generation_id": generation})
		if err != nil {
			t.Fatalf("count %s: %v", generation, err)
		}
		rec, err := res.Single(ctx)
		if err != nil {
			t.Fatalf("count %s single: %v", generation, err)
		}
		v, _ := rec.Get("count")
		n, _ := v.(int64)
		return n
	}

	cleanup := func() {
		run("MATCH (n:"+label+") WHERE n.repo_id = $repo_id DETACH DELETE n", map[string]any{"repo_id": repoID})
	}
	cleanup()
	t.Cleanup(cleanup)
	seed := func(n int, generation string) {
		run("UNWIND range(1, $n) AS i CREATE (:"+label+" {repo_id: $repo_id, uid: $repo_id + '/' + $gen + '/' + toString(i), evidence_source: 'projector/canonical', generation_id: $gen})",
			map[string]any{"n": n, "repo_id": repoID, "gen": generation})
	}
	seed(stale, oldGen)
	seed(current, newGen)
	if got := count(oldGen); got != stale {
		t.Fatalf("seeded stale count = %d, want %d", got, stale)
	}

	rawExecutor := ingesterNeo4jExecutor{Driver: driver, DatabaseName: database, TxTimeout: 30 * time.Second}
	executor := canonicalExecutorForGraphBackend(
		rawExecutor,
		runtimecfg.GraphBackendNornicDB,
		30*time.Second,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBStructuralEdgePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		4,
		batchCap,
		nil,
		nil,
		newIngesterCanonicalGate(func(string) string { return "" }, nil),
	)
	phase, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}
	retractRepo := func(repo string) error {
		return phase.ExecutePhaseGroup(ctx, []sourcecypher.Statement{{
			Operation: sourcecypher.OperationCanonicalRetract,
			// The production canonicalNodeRetractEntityTemplate shape.
			Cypher: fmt.Sprintf("MATCH (n:%s)\n"+
				"WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id\n"+
				"DETACH DELETE n", label),
			Parameters: map[string]any{"repo_id": repo, "generation_id": newGen},
			Drain:      true,
			DrainVar:   "n",
		}})
	}
	retract := func() error { return retractRepo(repoID) }

	start := time.Now()
	if err := retract(); err != nil {
		t.Fatalf("probed drain ExecutePhaseGroup() error = %v, want nil", err)
	}
	t.Logf("backlog drain of %d stale rows took %s", stale, time.Since(start))
	if got := count(oldGen); got != 0 {
		t.Fatalf("stale count after drain = %d, want 0", got)
	}
	if got := count(newGen); got != current {
		t.Fatalf("current-generation count after drain = %d, want %d (the drain deleted live nodes)", got, current)
	}

	start = time.Now()
	if err := retract(); err != nil {
		t.Fatalf("no-op probed drain ExecutePhaseGroup() error = %v, want nil", err)
	}
	t.Logf("no-op drain took %s", time.Since(start))
	if got := count(newGen); got != current {
		t.Fatalf("current-generation count after no-op drain = %d, want %d", got, current)
	}

	// A repository that has nothing in the graph, with parameters no earlier
	// statement used, so NornicDB cannot answer from its result cache. This is
	// the shape of most production retracts.
	start = time.Now()
	if err := retractRepo(fmt.Sprintf("repository:6822-empty-%d", time.Now().UnixNano())); err != nil {
		t.Fatalf("zero-match probed drain ExecutePhaseGroup() error = %v, want nil", err)
	}
	t.Logf("zero-match drain on an uncached repo took %s", time.Since(start))
}
