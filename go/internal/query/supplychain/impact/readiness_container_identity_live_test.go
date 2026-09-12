// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	_ "github.com/jackc/pgx/v5/stdlib"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestSupplyChainImpactReadinessMutableRefIncludesEveryCurrentDigestLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the mutable-ref readiness proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	cleanupReadinessMutableRefProof(t, ctx, db)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupReadinessMutableRefProof(t, cleanupCtx, db)
	})
	seedReadinessMutableRefProof(t, ctx, db)

	args := []any{
		pgarray.Array(vulnerabilityAdvisoryFactKinds),
		pgarray.Array(vulnerabilityExploitabilityFactKinds),
		pgarray.Array(packageConsumptionCorrelationFactKinds),
		pgarray.Array(packageRegistryFactKinds),
		pgarray.Array(sbomComponentFactKinds),
		pgarray.Array(sbomAttestationFactKinds),
		pgarray.Array(containerImageIdentityFactKinds),
		pgarray.Array(vulnerabilitySourceSnapshotFactKinds),
		"", "", "", "", "", readinessMutableRef,
		pgarray.Array(vulnerabilityOSPackageFactKinds),
		pgarray.Array(scannerWorkerAnalysisFactKinds),
	}
	rows, err := db.QueryContext(ctx, ListSupplyChainImpactReadinessQuery, args...)
	if err != nil {
		t.Fatalf("query production readiness shape: %v", err)
	}
	defer func() { _ = rows.Close() }()
	families := make(map[string]int)
	for rows.Next() {
		var family string
		var factCount int
		var latest sql.NullTime
		var incomplete sql.NullBool
		var reasons pgarray.StringArray
		var sourceSnapshots, sourceStates, unsupported sql.NullString
		if err := rows.Scan(
			&family,
			&factCount,
			&latest,
			&incomplete,
			&reasons,
			&sourceSnapshots,
			&sourceStates,
			&unsupported,
		); err != nil {
			t.Fatalf("scan readiness row: %v", err)
		}
		families[family] = factCount
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read readiness rows: %v", err)
	}
	for _, family := range []string{"container_image.identity", "scanner_worker.analysis"} {
		if got := families[family]; got != readinessMutableRefDigestCount {
			t.Fatalf("%s count = %d, want complete %d-digest mutable-ref set", family, got, readinessMutableRefDigestCount)
		}
	}
}

const (
	readinessMutableRef            = "registry.example.com/team/readiness-over500:prod"
	readinessMutableRefDigestCount = 513
)

func seedReadinessMutableRefProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
)
SELECT
    'readiness-over500-scope-' || n, 'repository', 'git', 'readiness-over500-' || n,
    'git', 'readiness-over500-' || n, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM generate_series(1, 513) AS n;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
)
SELECT
    'readiness-over500-generation-' || n, 'readiness-over500-scope-' || n,
    'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
FROM generate_series(1, 513) AS n;

UPDATE ingestion_scopes
SET active_generation_id = 'readiness-over500-generation-' ||
    regexp_replace(scope_id, '^readiness-over500-scope-', '')
WHERE scope_id LIKE 'readiness-over500-scope-%';

INSERT INTO container_image_identity_support_sets (set_id, scope_id, content_hash, support_count)
SELECT
    sha256(convert_to('readiness-over500-set-' || n, 'UTF8')),
    'readiness-over500-scope-' || n,
    sha256(convert_to('readiness-over500-content-' || n, 'UTF8')),
    1
FROM generate_series(1, 513) AS n;

INSERT INTO container_image_identity_supports (
    set_id, digest, support_id, image_ref, repository_id, outcome,
    identity_strength, canonical_writes, source_repository_ids, source_layers
)
SELECT
    sha256(convert_to('readiness-over500-set-' || n, 'UTF8')),
    'sha256:' || lpad(to_hex(n), 64, '0'),
    sha256(convert_to('readiness-over500-support-' || n, 'UTF8')),
    'registry.example.com/team/readiness-over500:prod',
    'registry.example.com/team/readiness-over500',
    'exact_digest', 'digest', 1,
    ARRAY['repository:readiness-over500'],
    ARRAY['observed_resource', 'source_declaration']
FROM generate_series(1, 513) AS n;

UPDATE container_image_identity_scope_state AS state
SET active_set_id = sha256(convert_to(
        'readiness-over500-set-' || regexp_replace(state.scope_id, '^readiness-over500-scope-', ''),
        'UTF8'
    )),
    last_set_id = sha256(convert_to(
        'readiness-over500-set-' || regexp_replace(state.scope_id, '^readiness-over500-scope-', ''),
        'UTF8'
    )),
    last_set_hash = sha256(convert_to(
        'readiness-over500-content-' || regexp_replace(state.scope_id, '^readiness-over500-scope-', ''),
        'UTF8'
    )),
    source_system = 'git', collector_kind = 'git', source_confidence = 'inferred',
    source_fact_key = 'intent:readiness-over500', observed_at = clock_timestamp(), ingested_at = clock_timestamp()
WHERE state.scope_id LIKE 'readiness-over500-scope-%';

INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES (
    'readiness-over500-scan', 'container_image', 'scanner_worker', 'readiness-over500-scan',
    'scanner_worker', 'readiness-over500-scan', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES (
    'readiness-over500-scan-generation', 'readiness-over500-scan',
    'test', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb
);
UPDATE ingestion_scopes
SET active_generation_id = 'readiness-over500-scan-generation'
WHERE scope_id = 'readiness-over500-scan';

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    'readiness-over500-analysis-' || n,
    'readiness-over500-scan',
    'readiness-over500-scan-generation',
    'scanner_worker.analysis',
    'readiness-over500-analysis-' || n,
    'scanner_worker',
    'readiness-over500-analysis-' || n,
    clock_timestamp(), clock_timestamp(), FALSE,
    jsonb_build_object(
        'image_reference', 'registry.example.com/team/readiness-over500:prod',
        'image_digest', 'sha256:' || lpad(to_hex(n), 64, '0'),
        'analysis_status', 'completed',
        'coverage_status', 'supported',
        'analyzer', 'ospkg'
    )
FROM generate_series(1, 513) AS n;

ANALYZE ingestion_scopes;
ANALYZE scope_generations;
ANALYZE container_image_identity_scope_state;
ANALYZE container_image_identity_support_sets;
ANALYZE container_image_identity_supports;
ANALYZE fact_records;
`); err != nil {
		t.Fatalf("seed mutable-ref readiness proof: %v", err)
	}
}

func cleanupReadinessMutableRefProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
DELETE FROM ingestion_scopes
WHERE scope_id LIKE 'readiness-over500-scope-%'
   OR scope_id = 'readiness-over500-scan'`); err != nil {
		t.Fatalf("clean mutable-ref readiness proof: %v", err)
	}
}
