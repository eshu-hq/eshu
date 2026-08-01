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

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestContainerImageIdentitySupportPaginationPreservesPrefixScopesPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the prefix-order pagination proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	const (
		shortScope = "repository:cursor-prefix-a"
		longScope  = "repository:cursor-prefix-aa"
		wantRows   = listFactsByKindPageSize + 2
	)
	digest := "sha256:" + strings.Repeat("75", 32)
	cleanupContainerImageIdentityPrefixPaginationLive(t, ctx, db, shortScope, longScope)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentityPrefixPaginationLive(t, cleanupCtx, db, shortScope, longScope)
	})
	seedContainerImageIdentityPrefixPaginationLive(t, ctx, db, digest, shortScope, longScope)

	store := NewFactStore(SQLDB{DB: db})
	ciRows, err := store.ListActiveCICDRunCorrelationFacts(
		ctx,
		[]string{digest},
		nil,
	)
	if err != nil {
		t.Fatalf("list CI/CD support facts: %v", err)
	}
	assertContainerImageIdentityPrefixPaginationRows(t, ciRows, longScope, wantRows)

	sbomRows, err := store.ListActiveSBOMAttestationAttachmentFacts(ctx, []string{digest})
	if err != nil {
		t.Fatalf("list SBOM support facts: %v", err)
	}
	assertContainerImageIdentityPrefixPaginationRows(t, sbomRows, longScope, wantRows)

	impactRows, truncated, err := store.ListActiveSupplyChainImpactFacts(
		ctx,
		reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{digest}},
	)
	if err != nil {
		t.Fatalf("list supply-chain support facts: %v", err)
	}
	if truncated {
		t.Fatal("supply-chain support facts unexpectedly truncated")
	}
	assertContainerImageIdentityPrefixPaginationRows(t, impactRows, longScope, wantRows)
}

func TestContainerImageIdentityLegacySupportPaginationPreservesPrefixScopesPostgresLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the legacy prefix-order pagination proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}

	const (
		shortScope = "repository:legacy-cursor-prefix-a"
		longScope  = "repository:legacy-cursor-prefix-aa"
		wantRows   = listFactsByKindPageSize + 2
	)
	digest := "sha256:" + strings.Repeat("76", 32)
	cleanupContainerImageIdentityPrefixPaginationLive(t, ctx, db, shortScope, longScope)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupContainerImageIdentityPrefixPaginationLive(t, cleanupCtx, db, shortScope, longScope)
	})
	seedContainerImageIdentityLegacyPrefixPaginationLive(t, ctx, db, digest, shortScope, longScope)

	store := NewFactStore(SQLDB{DB: db})
	ciRows, err := store.ListActiveCICDRunCorrelationFacts(ctx, []string{digest}, nil)
	if err != nil {
		t.Fatalf("list legacy CI/CD support facts: %v", err)
	}
	assertContainerImageIdentityPrefixPaginationRows(t, ciRows, longScope, wantRows)

	sbomRows, err := store.ListActiveSBOMAttestationAttachmentFacts(ctx, []string{digest})
	if err != nil {
		t.Fatalf("list legacy SBOM support facts: %v", err)
	}
	assertContainerImageIdentityPrefixPaginationRows(t, sbomRows, longScope, wantRows)

	impactRows, truncated, err := store.ListActiveSupplyChainImpactFacts(
		ctx,
		reducer.SupplyChainImpactFactFilter{SubjectDigests: []string{digest}},
	)
	if err != nil {
		t.Fatalf("list legacy supply-chain support facts: %v", err)
	}
	if truncated {
		t.Fatal("legacy supply-chain support facts unexpectedly truncated")
	}
	assertContainerImageIdentityPrefixPaginationRows(t, impactRows, longScope, wantRows)
}

func assertContainerImageIdentityPrefixPaginationRows(
	t *testing.T,
	rows []facts.Envelope,
	longScope string,
	wantRows int,
) {
	t.Helper()
	if len(rows) != wantRows {
		t.Fatalf("paged support rows = %d, want %d", len(rows), wantRows)
	}
	var longScopeRows int
	for _, row := range rows {
		if row.ScopeID == longScope {
			longScopeRows++
		}
	}
	if longScopeRows != 1 {
		t.Fatalf("long prefix scope rows = %d, want 1", longScopeRows)
	}
}

func seedContainerImageIdentityPrefixPaginationLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	digest string,
	shortScope string,
	longScope string,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin prefix-order support seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{{query: `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
)
SELECT scope_id, 'repository', 'git', scope_id, 'git', scope_id,
       clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM unnest(ARRAY[$1::text, $2::text]) AS scope_id
`, args: []any{shortScope, longScope}}, {query: `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
)
SELECT 'generation:' || scope_id, scope_id, 'test', clock_timestamp(),
       clock_timestamp(), 'active', '{}'::jsonb
FROM unnest(ARRAY[$1::text, $2::text]) AS scope_id
`, args: []any{shortScope, longScope}}, {query: `
UPDATE ingestion_scopes
SET active_generation_id = 'generation:' || scope_id
WHERE scope_id = ANY(ARRAY[$1::text, $2::text])
`, args: []any{shortScope, longScope}}, {query: `
INSERT INTO container_image_identity_support_sets (
    set_id, scope_id, content_hash, support_count
)
SELECT sha256(convert_to('set:' || scope_id, 'UTF8')), scope_id,
       sha256(convert_to('content:' || scope_id, 'UTF8')),
       CASE WHEN scope_id = $1 THEN $3::integer + 1 ELSE 1 END
FROM unnest(ARRAY[$1::text, $2::text]) AS scope_id
`, args: []any{shortScope, longScope, listFactsByKindPageSize}}, {query: `
INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
)
SELECT
    sha256(convert_to('set:' || scope_id, 'UTF8')),
    $4::text,
    sha256(convert_to(format('support:%s:%s', scope_id, n), 'UTF8')),
    format('registry.example.com/team/prefix-%s@%s', n, $4::text),
    'registry.example.com/team/prefix',
    'exact_digest', 'digest', 1,
    ARRAY['repository:prefix-proof']::text[],
    ARRAY['observed_resource', 'source_declaration']::text[]
FROM unnest(ARRAY[$1::text, $2::text]) AS scope_id
CROSS JOIN LATERAL generate_series(
    1,
    CASE WHEN scope_id = $1 THEN $3::integer + 1 ELSE 1 END
) AS n
`, args: []any{shortScope, longScope, listFactsByKindPageSize, digest}}, {query: `
UPDATE container_image_identity_scope_state AS state
SET active_set_id = sha256(convert_to('set:' || state.scope_id, 'UTF8')),
    last_set_id = sha256(convert_to('set:' || state.scope_id, 'UTF8')),
    last_set_hash = sha256(convert_to('content:' || state.scope_id, 'UTF8')),
    source_system = 'git', collector_kind = 'git', source_confidence = 'inferred',
    source_fact_key = 'intent:prefix-pagination-proof',
    observed_at = clock_timestamp(), ingested_at = clock_timestamp()
WHERE state.scope_id = ANY(ARRAY[$1::text, $2::text])
	`, args: []any{shortScope, longScope}}}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed prefix-order support rows: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit prefix-order support seed: %v", err)
	}
}

func seedContainerImageIdentityLegacyPrefixPaginationLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	digest string,
	shortScope string,
	longScope string,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin legacy prefix-order seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{{query: `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
)
SELECT scope_id, 'repository', 'git', scope_id, 'git', scope_id,
       clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM unnest(ARRAY[$1::text, $2::text]) AS scope_id
`, args: []any{shortScope, longScope}}, {query: `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
)
SELECT 'generation:' || scope_id, scope_id, 'test', clock_timestamp(),
       clock_timestamp(), 'active', '{}'::jsonb
FROM unnest(ARRAY[$1::text, $2::text]) AS scope_id
`, args: []any{shortScope, longScope}}, {query: `
UPDATE ingestion_scopes
SET active_generation_id = 'generation:' || scope_id
WHERE scope_id = ANY(ARRAY[$1::text, $2::text])
`, args: []any{shortScope, longScope}}}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed legacy prefix-order scope: %v", err)
		}
	}
	insertFacts := `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
    format('legacy:prefix:%s:%s', $1::text, n),
    $1::text,
    'generation:' || $1::text,
    'reducer_container_image_identity',
    format('container_image_identity:prefix:%s:%s', $1::text, n),
    'git',
    'intent:legacy-prefix-pagination-proof',
    clock_timestamp(),
    clock_timestamp(),
    jsonb_build_object(
        'digest', $3::text,
        'image_ref', format('registry.example.com/team/legacy-prefix-%s@%s', n, $3::text),
        'repository_id', 'registry.example.com/team/legacy-prefix',
        'outcome', 'exact_digest',
        'identity_strength', 'digest',
        'canonical_writes', 1,
        'source_repository_ids', jsonb_build_array('repository:legacy-prefix-proof')
    )
FROM generate_series(1, $2::integer) AS n
`
	for _, scope := range []struct {
		id    string
		count int
	}{{id: shortScope, count: listFactsByKindPageSize + 1}, {id: longScope, count: 1}} {
		if _, err := tx.ExecContext(ctx, insertFacts, scope.id, scope.count, digest); err != nil {
			t.Fatalf("seed legacy prefix-order support rows: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit legacy prefix-order support seed: %v", err)
	}
}

func cleanupContainerImageIdentityPrefixPaginationLive(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeIDs ...string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = ANY($1::text[])`, scopeIDs); err != nil {
		t.Fatalf("clean prefix-order support rows: %v", err)
	}
}
