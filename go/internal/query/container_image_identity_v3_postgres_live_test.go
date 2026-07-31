// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContainerImageIdentityV3CanonicalReadPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the digest-v3 query proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	scopes := []string{"repository:v3-query-a", "repository:v3-query-b"}
	cleanupContainerImageIdentityV3QueryScopes(t, ctx, db, scopes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentityV3QueryScopes(t, cleanupCtx, db, scopes)
	})

	digest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	seedContainerImageIdentityV3QueryScope(t, ctx, db, scopes[0], "generation:v3-query-a", "repository:example-a", digest, "01")
	seedContainerImageIdentityV3QueryScope(t, ctx, db, scopes[1], "generation:v3-query-b", "repository:example-b", digest, "02")
	if _, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
) VALUES (
    decode(repeat('02', 32), 'hex'), $1, decode(repeat('03', 32), 'hex'),
    'registry.example.com/team/sidecar@' || $1::text,
    'registry.example.com/team/sidecar', 'exact_digest', 'digest', 1,
    ARRAY['repository:example-b']::text[],
    ARRAY['observed_resource', 'source_declaration']::text[]
)
`, digest); err != nil {
		t.Fatalf("seed second repository support: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE container_image_identity_support_sets
SET support_count = 2
WHERE set_id = decode(repeat('02', 32), 'hex')
`); err != nil {
		t.Fatalf("update second support-set count: %v", err)
	}

	store := NewPostgresContainerImageIdentityStore(db)
	rows, err := store.ListContainerImageIdentities(ctx, ContainerImageIdentityFilter{
		Digest: digest,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("list canonical identities: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("canonical identity rows = %d, want 1", len(rows))
	}
	if got, want := strings.Join(rows[0].SourceRepositoryIDs, ","), "repository:example-a,repository:example-b"; got != want {
		t.Fatalf("folded source repositories = %q, want %q", got, want)
	}

	var payloadBytes []byte
	if err := db.QueryRowContext(
		ctx,
		listContainerImageIdentitiesQuery,
		digest, "", "", "", "", "", 10, []string{},
	).Scan(new(string), new(string), &payloadBytes); err != nil {
		t.Fatalf("read canonical winner payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("decode canonical winner payload: %v", err)
	}
	if got := StringVal(payload, "scope_id"); got != scopes[0] {
		t.Fatalf("canonical winner scope = %q, want %q", got, scopes[0])
	}

	authorized, err := store.ListContainerImageIdentities(ctx, ContainerImageIdentityFilter{
		Digest:                     digest,
		Limit:                      10,
		AllowedSourceRepositoryIDs: []string{"repository:example-a"},
	})
	if err != nil {
		t.Fatalf("list authorized canonical identities: %v", err)
	}
	if len(authorized) != 1 || strings.Join(authorized[0].SourceRepositoryIDs, ",") != "repository:example-a" {
		t.Fatalf("authorized identity leaked another scope: %+v", authorized)
	}

	aggregates := NewPostgresContainerImageIdentityAggregateStore(db)
	count, err := aggregates.CountContainerImageIdentities(ctx, ContainerImageIdentityAggregateFilter{Digest: digest})
	if err != nil {
		t.Fatalf("count canonical identities: %v", err)
	}
	if count.TotalIdentities != 1 {
		t.Fatalf("canonical total = %d, want 1", count.TotalIdentities)
	}
	inventory, err := aggregates.ContainerImageIdentityInventory(
		ctx,
		ContainerImageIdentityAggregateFilter{Digest: digest},
		ContainerImageIdentityInventoryByRepository,
		10,
		0,
	)
	if err != nil {
		t.Fatalf("inventory canonical identities: %v", err)
	}
	if len(inventory) != 2 || inventory[0].Count != 1 || inventory[1].Count != 1 {
		t.Fatalf("repository inventory = %+v, want one digest in each of two repositories", inventory)
	}
	values := map[string]bool{inventory[0].Value: true, inventory[1].Value: true}
	if !values["registry.example.com/team/app"] || !values["registry.example.com/team/sidecar"] {
		t.Fatalf("repository inventory values = %+v", inventory)
	}
}

func seedContainerImageIdentityV3QueryScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
	sourceRepositoryID string,
	digest string,
	setByte string,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin v3 query scope %s: %v", scopeID, err)
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{
		{query: `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)
`, args: []any{scopeID}},
		{query: `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($2, $1, 'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)
`, args: []any{scopeID, generationID}},
		{query: `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, args: []any{scopeID, generationID}},
		{query: `
INSERT INTO container_image_identity_support_sets (set_id, scope_id, content_hash, support_count)
VALUES (decode(repeat($2, 32), 'hex'), $1, decode(repeat($2, 32), 'hex'), 1)
`, args: []any{scopeID, setByte}},
		{query: `
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
) VALUES (
    decode(repeat($3, 32), 'hex'), $2, decode(repeat($3, 32), 'hex'),
    'registry.example.com/team/app@' || $2::text,
    'registry.example.com/team/app', 'exact_digest', 'digest', 1,
    ARRAY[$1]::text[], ARRAY['observed_resource', 'source_declaration']::text[]
)
`, args: []any{sourceRepositoryID, digest, setByte}},
		{query: `
UPDATE container_image_identity_scope_state
SET active_set_id = decode(repeat($3, 32), 'hex'),
    last_set_id = decode(repeat($3, 32), 'hex'),
    last_set_hash = decode(repeat($3, 32), 'hex'),
    source_system = 'git', collector_kind = 'git', source_confidence = 'inferred',
    source_fact_key = 'intent:v3-query', observed_at = clock_timestamp(), ingested_at = clock_timestamp()
WHERE scope_id = $1 AND active_generation_id = $2
`, args: []any{scopeID, generationID, setByte}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed v3 query scope %s: %v", scopeID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit v3 query scope %s: %v", scopeID, err)
	}
}

func cleanupContainerImageIdentityV3QueryScopes(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopes []string,
) {
	t.Helper()
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM ingestion_scopes WHERE scope_id = ANY($1::text[])`,
		scopes,
	); err != nil {
		t.Fatalf("clean v3 query scopes: %v", err)
	}
}
