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
)

// deferredPartitionProofSchemaSQL is the minimal scope/generation/fact table set
// the deferred backfill's per-scope fact load selects over. It mirrors the
// production columns the latestGenerationCTE and
// listDeferredScopedRelationshipFactRecordsQuery reference, plus the
// fact_records_scope_generation_idx the per-scope partition load is bound by, so
// the partition-source proof exercises the same access path the data plane would.
const deferredPartitionProofSchemaSQL = `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT NULL
);

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    ingested_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX scope_generations_scope_latest_lookup_idx
    ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);

CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX fact_records_scope_generation_idx
    ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC);
`

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

	loaded, _, err := store.loadDeferredAnchorScopedRelationshipFacts(
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

// seedDeferredPartitionFixture inserts the three partition-coverage cases.
func seedDeferredPartitionFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	scopes := []string{
		"git:scope-a",
		"git:scope-b",
		"git:scope-ref",
		"gcp:project:demo:relationship:global",
	}
	for _, scopeID := range scopes {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", scopeID); err != nil {
			t.Fatalf("seed scope %q: %v", scopeID, err)
		}
	}
	gens := map[string]string{
		"git:scope-a":                          "gen-a",
		"git:scope-b":                          "gen-b",
		"git:scope-ref":                        "gen-ref",
		"gcp:project:demo:relationship:global": "gen-gcp",
	}
	for scopeID, genID := range gens {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			genID, scopeID, base); err != nil {
			t.Fatalf("seed generation %q: %v", genID, err)
		}
	}

	insertFact := func(factID, scopeID, genID, kind, payload string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $5, 'git', $5, $6, $6, $7::jsonb)`,
			factID, scopeID, genID, kind, factID, base, payload); err != nil {
			t.Fatalf("seed fact %q: %v", factID, err)
		}
	}

	// (b) Two distinct git scopes whose repository fact derives the SAME repo_id
	// ("shared-id"). Each carries a content fact referencing the OTHER's alias so
	// both must load and resolve a cross-repo edge.
	insertFact("repo-a", "git:scope-a", "gen-a", "repository", `{"repo_id":"shared-id","name":"alpha-service"}`)
	insertFact("repo-b", "git:scope-b", "gen-b", "repository", `{"graph_id":"shared-id","name":"beta-service"}`)
	insertFact("fact-alpha-ref-beta", "git:scope-a", "gen-a", "content",
		`{"repo_id":"shared-id","artifact_type":"terraform","relative_path":"main.tf","content":"target = beta-service"}`)
	insertFact("fact-beta-ref-alpha", "git:scope-b", "gen-b", "content",
		`{"repo_id":"shared-id","artifact_type":"terraform","relative_path":"main.tf","content":"target = alpha-service"}`)

	// (c) Cross-scope content reference in its own git scope.
	insertFact("repo-ref", "git:scope-ref", "gen-ref", "repository", `{"repo_id":"repo-ref-target","name":"billing-service"}`)
	insertFact("fact-cross-scope-ref", "git:scope-ref", "gen-ref", "content",
		`{"repo_id":"repo-ref-target","artifact_type":"terraform","relative_path":"refs.tf","content":"uses payments-service"}`)

	// (a) gcp_cloud_relationship fact in a CLOUD scope with NO repository fact.
	gcpPayload := `{"source_full_resource_name":"//run.googleapis.com/projects/demo/locations/us-central1/services/order-gateway","source_asset_type":"run.googleapis.com/Service","relationship_type":"run_service_uses_secret","target_full_resource_name":"//secretmanager.googleapis.com/projects/demo/secrets/payments-service","target_asset_type":"secretmanager.googleapis.com/Secret","support_state":"supported"}`
	insertFact("fact-gcp-edge", "gcp:project:demo:relationship:global", "gen-gcp", "gcp_cloud_relationship", gcpPayload)
}

func evidenceHasEdge(evidence []relationships.EvidenceFact, source, target string) bool {
	for _, fact := range evidence {
		if fact.SourceRepoID == source && fact.TargetRepoID == target {
			return true
		}
	}
	return false
}
