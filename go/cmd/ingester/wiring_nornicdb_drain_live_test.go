// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestIngesterNornicDBDrainLiveDeletesEntireBacklog is the #5198 representative
// live proof: the ingester's real drain path — ingesterNeo4jExecutor.RunWrite
// wrapped by ingesterTimeoutDrainReader (per-iteration client deadline) and the
// canonical gate — must drain a multi-iteration full-refresh backlog to zero
// against a live NornicDB, with the per-iteration budget resetting each
// iteration so a correctly progressing drain is never canceled.
//
// Gate: set ESHU_INGESTER_DRAIN_PROVE_LIVE=1 and ESHU_NEO4J_URI
// (e.g. bolt://127.0.0.1:17688); ESHU_NEO4J_DATABASE defaults to "nornic".
func TestIngesterNornicDBDrainLiveDeletesEntireBacklog(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_INGESTER_DRAIN_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_INGESTER_DRAIN_PROVE_LIVE=1 to run the #5198 live drain proof")
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
		repoID   = "repository:5198-live-drain"
		oldGen   = "gen-1"
		newGen   = "gen-2"
		seedRows = 250
	)
	run := func(cypher string, params map[string]any) {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: database,
		})
		defer func() { _ = session.Close(ctx) }()
		if _, err := session.Run(ctx, cypher, params); err != nil {
			t.Fatalf("run %q: %v", cypher, err)
		}
	}
	countFiles := func() int64 {
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeRead,
			DatabaseName: database,
		})
		defer func() { _ = session.Close(ctx) }()
		res, err := session.Run(ctx,
			`MATCH (f:File {repo_id: $repo_id}) RETURN count(f) AS count`,
			map[string]any{"repo_id": repoID})
		if err != nil {
			t.Fatalf("count files: %v", err)
		}
		rec, err := res.Single(ctx)
		if err != nil {
			t.Fatalf("count files single: %v", err)
		}
		v, _ := rec.Get("count")
		n, _ := v.(int64)
		return n
	}

	// Clean any prior run, then seed a multi-iteration backlog of stale-generation
	// File nodes (each with a distinct path so they are independent nodes).
	run(`MATCH (f:File {repo_id: $repo_id}) DETACH DELETE f`, map[string]any{"repo_id": repoID})
	t.Cleanup(func() {
		run(`MATCH (f:File {repo_id: $repo_id}) DETACH DELETE f`, map[string]any{"repo_id": repoID})
	})
	run(`UNWIND range(1, $n) AS i
CREATE (:File {repo_id: $repo_id, path: $repo_id + '/f' + toString(i), evidence_source: 'projector/canonical', generation_id: $gen})`,
		map[string]any{"n": seedRows, "repo_id": repoID, "gen": oldGen})
	if got := countFiles(); got != seedRows {
		t.Fatalf("seeded File count = %d, want %d", got, seedRows)
	}

	// Build the real ingester drain path with a small retract batch so the drain
	// spans several iterations, and a per-iteration client budget.
	rawExecutor := ingesterNeo4jExecutor{Driver: driver, DatabaseName: database, TxTimeout: 30 * time.Second}
	executor := canonicalExecutorForGraphBackend(
		rawExecutor,
		runtimecfg.GraphBackendNornicDB,
		30*time.Second, // per-iteration client budget
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		4,
		50, // small retract batch -> 5 drain iterations for 250 rows
		nil,
		nil,
		newIngesterCanonicalGate(func(string) string { return "" }, nil),
	)
	phase, ok := executor.(nornicDBPhaseGroupExecutor)
	if !ok {
		t.Fatalf("executor type = %T, want nornicDBPhaseGroupExecutor", executor)
	}

	stmts := []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalRetract,
		Cypher: `MATCH (f:File {repo_id: $repo_id})
WHERE f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
DETACH DELETE f`,
		Parameters: map[string]any{"repo_id": repoID, "generation_id": newGen},
		Drain:      true,
		DrainVar:   "f",
	}}

	if err := phase.ExecutePhaseGroup(ctx, stmts); err != nil {
		t.Fatalf("live drain ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got := countFiles(); got != 0 {
		t.Fatalf("File count after drain = %d, want 0 (backlog not fully drained)", got)
	}
}
