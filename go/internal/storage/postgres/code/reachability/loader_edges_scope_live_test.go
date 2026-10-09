// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/codeintel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
)

// openEdgesScopeLiveDB opens the disposable PostgreSQL 18 database this
// proof enrolls in the live-postgres-readiness runner under, skipping
// without the runner's DSN pair:
//
//	ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DSN=postgres://user:pass@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres/code/reachability/ -run TestLoadCodeReachabilityEdgesReadsConsumerScopeOnly
func openEdgesScopeLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DISPOSABLE")
	if dsn == "" || optIn != "1" {
		t.Skip("ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DSN/ESHU_REACHABILITY_EDGES_SCOPE_PROOF_DISPOSABLE not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("bootstrap schema: %v", err)
	}
	return ctx, db
}

// TestLoadCodeReachabilityEdgesReadsConsumerScopeOnly pins the #7592
// direct-edge contract: the loader reads edges only from the consumer
// repository's own scope, so a chain X -> Z.g -> P is never assembled in X.
// X's loaded input holds X's own edges (both the code_calls and the
// inheritance_edges arms); Z's edges stay in Z's input.
func TestLoadCodeReachabilityEdgesReadsConsumerScopeOnly(t *testing.T) {
	ctx, db := openEdgesScopeLiveDB(t)
	key := routeLivenessTestSuffix(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	scopeX, repoX, runX, genX := "scope-7592-x-"+key, "repo-7592-x-"+key, "run-7592-x-"+key, "gen-7592-x-"+key
	scopeZ, repoZ, runZ, genZ := "scope-7592-z-"+key, "repo-7592-z-"+key, "run-7592-z-"+key, "gen-7592-z-"+key
	registerRouteLivenessCleanup(t, db, scopeX, repoX)
	registerRouteLivenessCleanup(t, db, scopeZ, repoZ)

	now := time.Now().UTC()
	seedScope := func(scopeID, repoID, runID, genID string) {
		t.Helper()
		exec(`INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		   observed_at, ingested_at, status, active_generation_id, payload)
		  VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3,'{}'::jsonb)`,
			scopeID, now, genID)
		exec(`INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		  VALUES ($1,$2,'manual',$3,$3,'active',$3)`, genID, scopeID, now)
		exec(`INSERT INTO shared_projection_acceptance
		  (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
		  VALUES ($1,$2,$3,$4,$5,$5)`, scopeID, repoID, runID, genID, now)
	}
	seedIntent := func(id, domain, scopeID, repoID, runID, genID, payload string) {
		t.Helper()
		exec(`INSERT INTO shared_projection_intents
		  (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id, repository_id,
		   source_run_id, generation_id, payload, created_at, completed_at)
		  VALUES ($1,$2,$3,$4,$3,$3,$5,$6,$7::jsonb,$8,$8)`,
			id, domain, repoID, scopeID, runID, genID, payload, now)
	}
	seedScope(scopeX, repoX, runX, genX)
	seedScope(scopeZ, repoZ, runZ, genZ)
	// X calls into Z; Z calls the producer P. The P edge must never leak
	// into X's loaded input.
	seedIntent("intent-7592-x-call-"+key, "code_calls", scopeX, repoX, runX, genX,
		`{"caller_entity_id":"X:f","callee_entity_id":"Z:g","relationship_type":"CALLS","resolution_method":"scip"}`)
	seedIntent("intent-7592-x-inh-"+key, "inheritance_edges", scopeX, repoX, runX, genX,
		`{"child_entity_id":"X:c","parent_entity_id":"X:p","relationship_type":"INHERITS","resolution_method":"test"}`)
	seedIntent("intent-7592-z-call-"+key, "code_calls", scopeZ, repoZ, runZ, genZ,
		`{"caller_entity_id":"Z:g","callee_entity_id":"P","relationship_type":"CALLS","resolution_method":"scip"}`)

	store := reachabilitystore.NewCodeReachabilityStore(postgres.SQLDB{DB: db})
	xEdges, err := reachabilitystore.LoadCodeReachabilityEdges(store, ctx, scopeX, repoX, runX, genX)
	if err != nil {
		t.Fatalf("load X edges: %v", err)
	}
	zEdges, err := reachabilitystore.LoadCodeReachabilityEdges(store, ctx, scopeZ, repoZ, runZ, genZ)
	if err != nil {
		t.Fatalf("load Z edges: %v", err)
	}

	got := func(edges []codeintel.CodeReachabilityEdge) map[string]bool {
		m := make(map[string]bool, len(edges))
		for _, e := range edges {
			m[e.SourceEntityID+">"+e.TargetEntityID+" "+e.RelationshipType+" "+e.ResolutionMethod] = true
		}
		return m
	}
	wantX := map[string]bool{
		"X:f>Z:g CALLS scip":    true,
		"X:c>X:p INHERITS test": true,
	}
	wantZ := map[string]bool{
		"Z:g>P CALLS scip": true,
	}
	assertSet := func(name string, edges []codeintel.CodeReachabilityEdge, want map[string]bool) {
		t.Helper()
		have := got(edges)
		if len(have) != len(want) {
			t.Fatalf("%s edges = %v, want exactly %v", name, have, want)
		}
		for k := range want {
			if !have[k] {
				t.Fatalf("%s edges = %v, missing %q", name, have, k)
			}
		}
	}
	assertSet("X", xEdges, wantX)
	assertSet("Z", zEdges, wantZ)
	for _, e := range xEdges {
		if e.TargetEntityID == "P" {
			t.Fatalf("X input contains chained edge to P: %+v (loader must not chain across scopes)", e)
		}
	}
}
