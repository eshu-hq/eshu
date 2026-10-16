// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

const deferredPartitionProofSchemaSQL = testfixtures.DeferredPartitionProofSchemaSQL

// TestDeferredBackfillPartitionSourceCoversAllScopes is the #3710 partition-source
// regression proof. It drives the REAL partitioned fact loader
// (loadDeferredAnchorScopedRelationshipFacts) against a real Postgres and asserts
// the loaded fact set survives the deferred partitioning for three cases the
// rejected per-repository partition source dropped:
//
//	(a) A gcp_cloud_relationship cross-repo edge whose fact lives in a CLOUD scope
//	    that carries NO repository fact. The old partition source filtered
//	    fact_kind = 'repository', so the cloud scope never appeared in the partition
//	    map and the fact never loaded — the gcp arm was dead.
//	(b) Two DISTINCT git scopes whose repository facts derive the SAME repo_id
//	    (COALESCE(repo_id, graph_id, name) collides). The old source keyed
//	    DISTINCT ON (repo_id), so one scope's facts were dropped. The
//	    (scope_id, generation_id) partition keeps both.
//	(c) A cross-scope content reference (one git repo's content names another
//	    repo's alias) must still load from its own scope partition.
//
// Gated on ESHU_DEFERRED_PARTITION_PROOF_DSN so it runs only where a Postgres is
// available; the string-shape and fake-driven gates run everywhere.
func TestDeferredBackfillPartitionSourceCoversAllScopes(t *testing.T) {
	dsn := dsnForDeferredPartitionProof(t)
	ctx := context.Background()
	db := openDeferredPartitionProofDB(t, dsn)
	provisionDeferredPartitionSchema(t, db)
	adapter := SQLDB{DB: db}

	seedDeferredPartitionFixture(t, ctx, db)

	// The catalog the deferred backfill resolves targets against. Two collapsing
	// git repos share the derived repo_id "shared-id" but are distinct scopes; the
	// gcp source/target repos and the cross-scope reference target are also present.
	catalog := []relationships.CatalogEntry{
		{RepoID: "shared-id", Aliases: []string{"shared-id", "alpha-service"}},
		{RepoID: "shared-id-b", Aliases: []string{"shared-id", "beta-service"}},
		{RepoID: "repo-gcp-source", Aliases: []string{"repo-gcp-source", "order-gateway"}},
		{RepoID: "repo-gcp-target", Aliases: []string{"repo-gcp-target", "payments-service"}},
		{RepoID: "repo-ref-target", Aliases: []string{"repo-ref-target", "billing-service"}},
	}

	store := NewIngestionStore(adapter)
	store.maintenanceWorkers = 4

	loaded, _, _, err := store.backfillStore().LoadDeferredAnchorScopedRelationshipFacts(
		ctx, adapter, catalog, nil,
	)
	if err != nil {
		t.Fatalf("loadDeferredAnchorScopedRelationshipFacts: %v", err)
	}

	loadedByID := make(map[string]bool, len(loaded))
	for _, env := range loaded {
		loadedByID[env.FactID] = true
	}

	// (a) the gcp_cloud_relationship fact in the cloud scope must survive.
	if !loadedByID["fact-gcp-edge"] {
		t.Error("gcp_cloud_relationship fact in a cloud scope was dropped by the partition source (P0): the gcp arm is dead")
	}
	// (b) both collapsing scopes' cross-repo facts must survive.
	if !loadedByID["fact-alpha-ref-beta"] {
		t.Error("scope-a fact dropped: the two scopes collapsing to repo_id \"shared-id\" lost one partition (P1)")
	}
	if !loadedByID["fact-beta-ref-alpha"] {
		t.Error("scope-b fact dropped: the two scopes collapsing to repo_id \"shared-id\" lost one partition (P1)")
	}
	// (c) the cross-scope content reference must survive.
	if !loadedByID["fact-cross-scope-ref"] {
		t.Error("cross-scope content reference fact dropped by the partition source")
	}

	// The loaded facts must produce the expected cross-repo evidence end to end.
	evidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(loaded, catalog))
	if !evidenceHasEdge(evidence, "repo-gcp-source", "repo-gcp-target") {
		t.Error("gcp cross-cloud edge repo-gcp-source -> repo-gcp-target missing from discovered evidence (P0)")
	}
}

// dsnForDeferredPartitionProof returns the proof DSN or skips the test. It reuses
// the shared latest-generation proof DSN when the dedicated one is unset so a
// single configured Postgres serves every gated proof in this package.
func dsnForDeferredPartitionProof(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_LATEST_GENERATION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DSN (or ESHU_LATEST_GENERATION_PROOF_DSN) to run the deferred backfill partition-source Postgres proof")
	return ""
}

func openDeferredPartitionProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	// Single connection so the proof schema's search_path (set on this one
	// connection below) applies to every partition query; the loader's per-scope
	// fan-out then self-serializes on the single connection, which proves the
	// partition coverage without needing per-connection search_path wiring.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func provisionDeferredPartitionSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("deferred_partition_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	// Keep public on the search_path so any shared extensions/operator classes
	// installed there resolve from the proof schema, matching the production
	// search_path.
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, deferredPartitionProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
}

// seedDeferredPartitionFixture inserts the unchanged shared fixture.
func seedDeferredPartitionFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	testfixtures.SeedDeferredPartitionFixture(t, ctx, db)
}

func evidenceHasEdge(evidence []relationships.EvidenceFact, source, target string) bool {
	for _, fact := range evidence {
		if fact.SourceRepoID == source && fact.TargetRepoID == target {
			return true
		}
	}
	return false
}
