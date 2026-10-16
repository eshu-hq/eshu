// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// DeferredPartitionProofSchemaSQL is the minimal scope/generation/fact table set
// the deferred backfill's per-scope fact load selects over. It mirrors the
// production columns the backfill.LatestGenerationCTE and
// backfill.ListDeferredScopedRelationshipFactRecordsQuery reference, plus the
// fact_records_scope_generation_idx the per-scope partition load is bound by, so
// the partition-source proof exercises the same access path the data plane would.
const DeferredPartitionProofSchemaSQL = `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT NULL
);

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    ingested_at TIMESTAMPTZ NOT NULL,
    -- Mirrors schema/data-plane/postgres/002_scope_generations.sql, whose status
    -- column is NOT NULL. The default keeps every existing seed helper's
    -- three-column INSERT valid while letting the replay-floor tests model the
    -- 'failed' lifecycle state failProjectorWorkQuery writes.
    status TEXT NOT NULL DEFAULT 'pending'
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

CREATE TABLE relationship_reference_candidate_keys (
    fact_id TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    generation_id TEXT NOT NULL,
    source_repo_id TEXT NOT NULL,
    reference_key TEXT NOT NULL,
    PRIMARY KEY (fact_id)
);

CREATE INDEX relationship_reference_candidate_keys_partition_idx
    ON relationship_reference_candidate_keys (scope_id, generation_id, fact_id);
`

// DeferredPartitionMemoProofSchemaSQL extends DeferredPartitionProofSchemaSQL
// (the scope/generation/fact tables) with the tables the FULL deferred backfill
// write path touches: relationship_evidence_facts (evidence persistence),
// graph_projection_phase_state (readiness), and the new
// deferred_backfill_partition_memo (issue #3624 Track 1 / B'). This lets the
// tests in this package drive the REAL BackfillAllRelationshipEvidence end to
// end, exercising both the read-side memo gate and the write-side memo commit
// in one pass, not just the fact-load half.
const DeferredPartitionMemoProofSchemaSQL = DeferredPartitionProofSchemaSQL + `
CREATE TABLE relationship_evidence_facts (
    evidence_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    evidence_kind TEXT NOT NULL,
    relationship_type TEXT NOT NULL,
    source_repo_id TEXT,
    target_repo_id TEXT,
    source_entity_id TEXT,
    target_entity_id TEXT,
    confidence DOUBLE PRECISION NOT NULL,
    rationale TEXT NOT NULL,
    details JSONB NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE graph_projection_phase_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    keyspace TEXT NOT NULL,
    phase TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
);

CREATE TABLE deferred_backfill_partition_memo (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    catalog_fingerprint TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);
`

func OpenDeferredPartitionMemoProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	// Single connection, matching the #3710 partition-source proof
	// (openDeferredPartitionProofDB): the proof schema's search_path is set on
	// this one connection below, so every subsequent query — including the
	// deferred backfill's concurrent per-partition fan-out — must reuse that
	// same connection to see the proof schema rather than "public".
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func ProvisionDeferredPartitionMemoSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("deferred_partition_memo_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, DeferredPartitionMemoProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	return schemaName
}

// SeedDeferredPartitionFixture inserts the three partition-coverage cases.
func SeedDeferredPartitionFixture(t *testing.T, ctx context.Context, db *sql.DB) {
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

// MemoProofFixture is one scope/generation/repository triple the tests below
// seed and drive through the real backfill.
type MemoProofFixture struct {
	ScopeID  string
	GenID    string
	RepoID   string
	RepoName string
}

func SeedMemoProofScopesAndFacts(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	fixtures []MemoProofFixture,
	crossRefs map[string]string, // sourceRepoID -> target repo alias referenced in content
	base time.Time,
) {
	t.Helper()

	for _, fx := range fixtures {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL) ON CONFLICT DO NOTHING",
			fx.ScopeID); err != nil {
			t.Fatalf("seed scope %q: %v", fx.ScopeID, err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
			fx.GenID, fx.ScopeID, base); err != nil {
			t.Fatalf("seed generation %q: %v", fx.GenID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)
ON CONFLICT DO NOTHING`,
			"repo-fact-"+fx.RepoID, fx.ScopeID, fx.GenID, base,
			fmt.Sprintf(`{"repo_id":%q,"name":%q}`, fx.RepoID, fx.RepoName)); err != nil {
			t.Fatalf("seed repository fact for %q: %v", fx.RepoID, err)
		}
	}

	for sourceRepoID, targetAlias := range crossRefs {
		var ScopeID, GenID string
		for _, fx := range fixtures {
			if fx.RepoID == sourceRepoID {
				ScopeID, GenID = fx.ScopeID, fx.GenID
			}
		}
		if ScopeID == "" {
			t.Fatalf("no fixture for source repo %q", sourceRepoID)
		}
		factID := "content-" + sourceRepoID
		content := fmt.Sprintf(`{"repo_id":%q,"artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"%s\""}`,
			sourceRepoID, targetAlias)
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content', $1, 'git', $1, $4, $4, $5::jsonb)
ON CONFLICT DO NOTHING`,
			factID, ScopeID, GenID, base, content); err != nil {
			t.Fatalf("seed content fact for %q: %v", sourceRepoID, err)
		}
	}
}
