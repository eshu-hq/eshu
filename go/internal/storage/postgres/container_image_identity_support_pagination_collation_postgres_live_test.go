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

func TestContainerImageIdentitySupportPaginationIsCompleteUnderICUCollationLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_ICU_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_ICU_TEST_DSN to run the locale-aware support pagination proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open ICU postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var collation, locale string
	var provider string
	if err := db.QueryRowContext(ctx, `
SELECT datcollate, datlocprovider::text, COALESCE(datlocale, '')
FROM pg_database
WHERE datname = current_database()`).Scan(&collation, &provider, &locale); err != nil {
		t.Fatalf("read database collation: %v", err)
	}
	if provider != "i" || locale == "" {
		t.Fatalf("locale-aware proof requires ICU database collation, got collate=%q provider=%q locale=%q", collation, provider, locale)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	cleanupContainerImageIdentityCollationProof(t, ctx, db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentityCollationProof(t, cleanupCtx, db)
	})
	seedContainerImageIdentityCollationProof(t, ctx, db)

	seen := make(map[string]struct{}, 4)
	cursor := ""
	for pageNumber := 0; pageNumber < 5; pageNumber++ {
		rows, err := db.QueryContext(ctx, `
SELECT fact_id
FROM container_image_identity_current_support_facts_for(
    '{}'::text[], '{}'::text[], '{}'::text[],
    ARRAY['repository:collation-proof']::text[], '{}'::text[], $1, 2
) AS fact
ORDER BY fact.fact_id ASC`, cursor)
		if err != nil {
			t.Fatalf("load support page %d: %v", pageNumber, err)
		}
		var page []string
		for rows.Next() {
			var factID string
			if err := rows.Scan(&factID); err != nil {
				_ = rows.Close()
				t.Fatalf("scan support page %d: %v", pageNumber, err)
			}
			page = append(page, factID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("read support page %d: %v", pageNumber, err)
		}
		_ = rows.Close()
		for _, factID := range page {
			if _, duplicate := seen[factID]; duplicate {
				t.Fatalf("duplicate support %q after cursor %q under %s/%s collation", factID, cursor, locale, collation)
			}
			seen[factID] = struct{}{}
		}
		if len(page) < 2 {
			break
		}
		cursor = page[len(page)-1]
	}
	if len(seen) != 4 {
		t.Fatalf("paged support count = %d, want 4 under %s/%s collation", len(seen), locale, collation)
	}
}

func seedContainerImageIdentityCollationProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
)
SELECT
    'collation-scope-' || value, 'repository', 'git', 'collation-' || value,
    'git', 'collation-' || value, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM unnest(ARRAY['A', 'a', 'Z', 'z']) AS value;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
)
SELECT
    'collation-generation-' || value, 'collation-scope-' || value,
    'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM unnest(ARRAY['A', 'a', 'Z', 'z']) AS value;

UPDATE ingestion_scopes
SET active_generation_id = 'collation-generation-' ||
    regexp_replace(scope_id, '^collation-scope-', '')
WHERE scope_id LIKE 'collation-scope-%';

INSERT INTO container_image_identity_support_sets (set_id, scope_id, content_hash, support_count)
SELECT
    sha256(convert_to('collation-set-' || value, 'UTF8')),
    'collation-scope-' || value,
    sha256(convert_to('collation-content-' || value, 'UTF8')),
    1
FROM unnest(ARRAY['A', 'a', 'Z', 'z']) AS value;

INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
)
SELECT
    sha256(convert_to('collation-set-' || value, 'UTF8')),
    'sha256:' || repeat('57', 32),
    sha256(convert_to('collation-support-' || value, 'UTF8')),
    'registry.example.com/team/collation-' || value || '@sha256:' || repeat('57', 32),
    'registry.example.com/team/collation-' || value,
    'exact_digest', 'digest', 1,
    ARRAY['repository:collation-proof'],
    ARRAY['observed_resource', 'source_declaration']
FROM unnest(ARRAY['A', 'a', 'Z', 'z']) AS value;

UPDATE container_image_identity_scope_state AS state
SET active_set_id = sha256(convert_to(
        'collation-set-' || regexp_replace(state.scope_id, '^collation-scope-', ''),
        'UTF8'
    )),
    last_set_id = sha256(convert_to(
        'collation-set-' || regexp_replace(state.scope_id, '^collation-scope-', ''),
        'UTF8'
    )),
    last_set_hash = sha256(convert_to(
        'collation-content-' || regexp_replace(state.scope_id, '^collation-scope-', ''),
        'UTF8'
    )),
    source_system = 'git', collector_kind = 'git', source_confidence = 'inferred',
    source_fact_key = 'intent:collation-proof', observed_at = clock_timestamp(), ingested_at = clock_timestamp()
WHERE state.scope_id LIKE 'collation-scope-%';
`); err != nil {
		t.Fatalf("seed locale-aware support pagination proof: %v", err)
	}
}

func cleanupContainerImageIdentityCollationProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id LIKE 'collation-scope-%'`); err != nil {
		t.Fatalf("clean locale-aware support pagination proof: %v", err)
	}
}
