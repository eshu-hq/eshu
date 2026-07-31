// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContainerImageIdentityHeldSupportStorePostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the held-support loader proof")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	scopeID := "repository:v3-held-loader-live"
	generationID := "generation:v3-held-loader-live"
	legacyRef := "registry.example.com/team/legacy@sha256:aaaaaaaa"
	typedRef := "registry.example.com/team/typed@sha256:bbbbbbbb"
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	}()
	seedContainerImageIdentityHeldSupportScope(t, db, scopeID, generationID)
	epoch := containerImageIdentityHeldSupportEpoch(t, db, scopeID, generationID)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES (
    'legacy:v3-held-loader-live', $1, $2, 'reducer_container_image_identity',
    'container_image_identity:legacy', 'git', 'intent:legacy', $4, $4,
    jsonb_build_object(
        'digest', 'sha256:aaaaaaaa',
        'image_ref', $3::TEXT,
        'repository_id', 'repository:legacy',
        'outcome', 'exact_digest',
        'canonical_writes', 1,
        'source_repository_ids', jsonb_build_array('repository:legacy')
    )
)
`, scopeID, generationID, legacyRef, time.Unix(1_700_000_000, 0).UTC()); err != nil {
		t.Fatalf("insert legacy support: %v", err)
	}

	store := NewContainerImageIdentityHeldSupportStore(SQLDB{DB: db})
	legacy, err := store.LoadHeldContainerImageIdentitySupports(
		ctx, scopeID, generationID, epoch, []string{legacyRef},
	)
	if err != nil {
		t.Fatalf("load legacy held support: %v", err)
	}
	if len(legacy) != 1 || legacy[0].ImageRef != legacyRef ||
		legacy[0].RepositoryID != "repository:legacy" {
		t.Fatalf("legacy supports = %#v, want exact active-generation reference", legacy)
	}

	if _, err := db.ExecContext(ctx, `
WITH inserted_set AS (
    INSERT INTO container_image_identity_support_sets (
        set_id, scope_id, content_hash, support_count
    ) VALUES (
        decode(repeat('0b', 32), 'hex'), $1, decode(repeat('1b', 32), 'hex'), 1
    )
    RETURNING set_id
)
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    canonical_writes, source_repository_ids
)
SELECT
    set_id, 'sha256:bbbbbbbb', decode(repeat('2b', 32), 'hex'), $2,
    'repository:typed', 'exact_digest', 1, ARRAY['repository:typed']::TEXT[]
FROM inserted_set
`, scopeID, typedRef); err != nil {
		t.Fatalf("insert typed support: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE container_image_identity_scope_state
SET active_set_id = decode(repeat('0b', 32), 'hex'),
    last_set_id = decode(repeat('0b', 32), 'hex'),
    last_set_hash = decode(repeat('1b', 32), 'hex')
WHERE scope_id = $1
`, scopeID); err != nil {
		t.Fatalf("activate typed support set: %v", err)
	}

	typed, err := store.LoadHeldContainerImageIdentitySupports(
		ctx, scopeID, generationID, epoch, []string{legacyRef, typedRef},
	)
	if err != nil {
		t.Fatalf("load typed held support: %v", err)
	}
	if len(typed) != 1 || typed[0].ImageRef != typedRef ||
		typed[0].RepositoryID != "repository:typed" {
		t.Fatalf("typed supports = %#v, want only exact active-set support", typed)
	}
	stale, err := store.LoadHeldContainerImageIdentitySupports(
		ctx, scopeID, generationID, epoch+1, []string{typedRef},
	)
	if err != nil {
		t.Fatalf("load stale-epoch held support: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("stale epoch loaded supports = %#v, want none", stale)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'failed' WHERE generation_id = $1
`, generationID); err != nil {
		t.Fatalf("mark generation failed: %v", err)
	}
	nonActive, err := store.LoadHeldContainerImageIdentitySupports(
		ctx, scopeID, generationID, epoch, []string{typedRef},
	)
	if err != nil {
		t.Fatalf("load non-active held support: %v", err)
	}
	if len(nonActive) != 0 {
		t.Fatalf("non-active generation loaded supports = %#v, want none", nonActive)
	}
	stateStore := NewContainerImageIdentityScopeStateStore(SQLDB{DB: db})
	if _, err := stateStore.ContainerImageIdentityActivationEpoch(
		ctx, scopeID, generationID,
	); err == nil {
		t.Fatal("non-active generation returned an activation epoch")
	}
}

func seedContainerImageIdentityHeldSupportScope(
	t *testing.T,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := db.Exec(`
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)
`, scopeID, now); err != nil {
		t.Fatalf("seed held-support scope: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($2, $1, 'test', $3, $3, 'active', '{}'::jsonb)
`, scopeID, generationID, now); err != nil {
		t.Fatalf("seed held-support generation: %v", err)
	}
	if _, err := db.Exec(`
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generationID); err != nil {
		t.Fatalf("activate held-support generation: %v", err)
	}
}

func containerImageIdentityHeldSupportEpoch(
	t *testing.T,
	db *sql.DB,
	scopeID string,
	generationID string,
) int64 {
	t.Helper()
	var epoch int64
	if err := db.QueryRow(`
SELECT activation_epoch
FROM container_image_identity_scope_state
WHERE scope_id = $1 AND active_generation_id = $2
`, scopeID, generationID).Scan(&epoch); err != nil {
		t.Fatalf("read activation epoch: %v", err)
	}
	return epoch
}
