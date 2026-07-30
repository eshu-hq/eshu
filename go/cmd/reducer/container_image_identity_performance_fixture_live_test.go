//go:build perf5854_head || perf5854_main

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	containerImageIdentityPerfRepositoryID     = "repository:r_5854_perf"
	containerImageIdentityPerfRepoScope        = "git-repository-scope:" + containerImageIdentityPerfRepositoryID
	containerImageIdentityPerfRepoGeneration   = "generation:5854:performance:repository"
	containerImageIdentityPerfRegistryScope    = "oci-registry://registry.example.com/performance/team-api"
	containerImageIdentityPerfRegistryGen      = "generation:5854:performance:registry"
	containerImageIdentityPerfStaleRegistryGen = "generation:5854:performance:registry:stale"
)

type containerImageIdentityPerfAccuracy struct {
	visibleRows      int
	outcomeKeyedRows int
	checksum         string
}

type containerImageIdentityPerfTableStats struct {
	dead    int64
	updated int64
	deleted int64
}

func openContainerImageIdentityPerfSchema(
	t *testing.T,
	ctx context.Context,
	dsn string,
	variant string,
	scenario string,
) *sql.DB {
	t.Helper()
	adminDB := openContainerImageIdentityPerfDB(t, dsn)
	schema := containerImageIdentityPerfVariantSchema(variant, scenario)
	if _, err := adminDB.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatalf("create performance schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(
			cleanupCtx,
			`DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`,
		); err != nil {
			t.Errorf("drop performance schema: %v", err)
		}
	})

	schemaDB := openContainerImageIdentityPerfDB(
		t,
		containerImageIdentityPerfSchemaDSN(t, dsn, schema),
	)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: schemaDB}); err != nil {
		t.Fatalf("apply production bootstrap: %v", err)
	}
	return schemaDB
}

func seedContainerImageIdentityPerfFixture(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	references int,
	staleWarnings int,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES
    ($1, 'repository', 'git', $1, 'git', $1, '2026-07-29T20:00:00Z', '2026-07-29T20:00:00Z', 'active'),
    ($2, 'oci_registry', 'oci_registry', $2, 'oci_registry', $2, '2026-07-29T20:00:00Z', '2026-07-29T20:00:00Z', 'active')
`, containerImageIdentityPerfRepoScope, containerImageIdentityPerfRegistryScope); err != nil {
		t.Fatalf("seed performance scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES
    ($3, $1, 'synthetic', '2026-07-29T20:00:00Z', '2026-07-29T20:00:00Z', 'active', '2026-07-29T20:00:00Z'),
    ($4, $2, 'synthetic', '2026-07-29T20:00:00Z', '2026-07-29T20:00:00Z', 'active', '2026-07-29T20:00:00Z'),
    ($5, $2, 'synthetic', '2026-07-29T19:00:00Z', '2026-07-29T19:00:00Z', 'superseded', NULL)
`, containerImageIdentityPerfRepoScope, containerImageIdentityPerfRegistryScope,
		containerImageIdentityPerfRepoGeneration, containerImageIdentityPerfRegistryGen,
		containerImageIdentityPerfStaleRegistryGen,
	); err != nil {
		t.Fatalf("seed performance generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET active_generation_id = CASE scope_id WHEN $1 THEN $3 ELSE $4 END
WHERE scope_id IN ($1, $2)
`, containerImageIdentityPerfRepoScope, containerImageIdentityPerfRegistryScope,
		containerImageIdentityPerfRepoGeneration, containerImageIdentityPerfRegistryGen,
	); err != nil {
		t.Fatalf("activate performance generations: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, payload
) VALUES (
    'repository-5854-performance', $1, $2, 'repository',
    'repository-5854-performance', '1.0.0', 'git', 'reported', 'git',
    'repository-5854-performance', '2026-07-29T20:00:00Z',
    '2026-07-29T20:00:00Z',
    jsonb_build_object(
        'repo_id', $3::text,
        'graph_id', $3::text,
        'remote_url', 'https://github.com/example/performance-team-api'
    )
)
`, containerImageIdentityPerfRepoScope, containerImageIdentityPerfRepoGeneration,
		containerImageIdentityPerfRepositoryID,
	); err != nil {
		t.Fatalf("seed performance repository fact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, payload
)
SELECT
    'content-5854-performance', $1, $2, 'content_entity',
    'content-5854-performance', '1.0.0', 'git', 'reported', 'git',
    'content-5854-performance', '2026-07-29T20:00:00Z',
    '2026-07-29T20:00:00Z',
    jsonb_build_object(
        'uid', 'entity:5854:performance',
        'entity_type', 'KubernetesResource',
        'metadata', jsonb_build_object(
            'container_images',
            (
                SELECT jsonb_agg(
                    'registry.example.com/performance/team-api:tag-' ||
                    lpad(series::text, 6, '0')
                    ORDER BY series
                )
                FROM generate_series(1, $3) AS series
            )
        )
    )
`, containerImageIdentityPerfRepoScope, containerImageIdentityPerfRepoGeneration,
		references,
	); err != nil {
		t.Fatalf("seed performance content fact: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, payload
)
SELECT
    'tag-observation-5854-performance-' || lpad(series::text, 6, '0'),
    $1::text, $2::text, 'oci_registry.image_tag_observation',
    'tag-observation-5854-performance-' || lpad(series::text, 6, '0'),
    '1.0.0', 'oci_registry', 'reported', 'oci_registry',
    'tag-observation-5854-performance-' || lpad(series::text, 6, '0'),
    '2026-07-29T20:00:00Z'::timestamptz + series * interval '1 microsecond',
    '2026-07-29T20:00:00Z'::timestamptz,
    jsonb_build_object(
        'registry', 'registry.example.com',
        'repository', 'performance/team-api',
        'repository_id', $1::text,
        'tag', 'tag-' || lpad(series::text, 6, '0'),
        'resolved_digest', 'sha256:' || lpad(to_hex(series), 64, '0'),
        'digest', 'sha256:' || lpad(to_hex(series), 64, '0'),
        'mutated', false,
        'previous_digest', ''
    )
FROM generate_series(1, $3) AS series;
`, containerImageIdentityPerfRegistryScope, containerImageIdentityPerfRegistryGen, references); err != nil {
		t.Fatalf("seed performance tag observations: %v", err)
	}

	if staleWarnings > 0 {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, payload
)
SELECT
    'stale-warning-5854-performance-' || lpad(series::text, 7, '0'),
    $1::text, $2::text, 'oci_registry.warning',
    'stale-warning-5854-performance-' || lpad(series::text, 7, '0'),
    '1.0.0', 'oci_registry', 'reported', 'oci_registry',
    'stale-warning-5854-performance-' || lpad(series::text, 7, '0'),
    '2026-07-29T19:00:00Z'::timestamptz + series * interval '1 microsecond',
    '2026-07-29T19:00:00Z'::timestamptz,
    jsonb_build_object(
        'warning_code', 'tag_list_truncated',
        'warning_key', 'tag_list_truncated',
        'repository_id', $1::text
    )
FROM generate_series(1, $3) AS series;
`, containerImageIdentityPerfRegistryScope, containerImageIdentityPerfStaleRegistryGen,
			staleWarnings,
		); err != nil {
			t.Fatalf("seed stale warning facts: %v", err)
		}
	}
}

func prepareContainerImageIdentityPerfStats(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "ALTER TABLE fact_records SET (autovacuum_enabled = false)"); err != nil {
		t.Fatalf("disable fact-record autovacuum for measurement: %v", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM (ANALYZE) fact_records"); err != nil {
		t.Fatalf("vacuum fact records before measurement: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		"SELECT pg_stat_reset_single_table_counters('fact_records'::regclass)",
	); err != nil {
		t.Fatalf("reset fact-record table counters: %v", err)
	}
}

func currentContainerImageIdentityPerfWAL(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var lsn string
	if err := db.QueryRowContext(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&lsn); err != nil {
		t.Fatalf("read current WAL LSN: %v", err)
	}
	return lsn
}

func containerImageIdentityPerfWALDiff(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	after string,
	before string,
) int64 {
	t.Helper()
	var bytes int64
	if err := db.QueryRowContext(
		ctx,
		"SELECT pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)::bigint",
		after,
		before,
	).Scan(&bytes); err != nil {
		t.Fatalf("calculate WAL difference: %v", err)
	}
	return bytes
}

func readContainerImageIdentityPerfTableStats(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) containerImageIdentityPerfTableStats {
	t.Helper()
	if _, err := db.ExecContext(ctx, "SELECT pg_stat_force_next_flush()"); err != nil {
		t.Fatalf("flush fact-record table stats: %v", err)
	}
	var stats containerImageIdentityPerfTableStats
	if err := db.QueryRowContext(ctx, `
SELECT n_dead_tup, n_tup_upd, n_tup_del
FROM pg_stat_user_tables
WHERE schemaname = current_schema()
  AND relname = 'fact_records'
`).Scan(&stats.dead, &stats.updated, &stats.deleted); err != nil {
		t.Fatalf("read fact-record table stats: %v", err)
	}
	return stats
}

func assertContainerImageIdentityPerfAccuracy(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	references int,
	headVariant bool,
) containerImageIdentityPerfAccuracy {
	t.Helper()
	var accuracy containerImageIdentityPerfAccuracy
	if err := db.QueryRowContext(ctx, `
SELECT
    count(*) FILTER (WHERE is_tombstone = FALSE),
    count(*) FILTER (
        WHERE is_tombstone = FALSE
          AND stable_fact_key LIKE '%:tag_resolved'
    ),
    COALESCE(
        md5(
            string_agg(
                concat_ws(
                    '|',
                    payload->>'image_ref',
                    payload->>'digest',
                    payload->>'repository_id',
                    payload->>'outcome'
                ),
                E'\n'
                ORDER BY payload->>'image_ref'
            ) FILTER (WHERE is_tombstone = FALSE)
        ),
        md5('')
    )
FROM fact_records
WHERE fact_kind = 'reducer_container_image_identity'
  AND scope_id = $1
  AND generation_id = $2
`, containerImageIdentityPerfRepoScope, containerImageIdentityPerfRepoGeneration).Scan(
		&accuracy.visibleRows,
		&accuracy.outcomeKeyedRows,
		&accuracy.checksum,
	); err != nil {
		t.Fatalf("read performance accuracy snapshot: %v", err)
	}
	if accuracy.visibleRows != references {
		t.Fatalf("visible identity rows = %d, want %d", accuracy.visibleRows, references)
	}
	wantOutcomeKeyed := references
	if headVariant {
		wantOutcomeKeyed = 0
	}
	if accuracy.outcomeKeyedRows != wantOutcomeKeyed {
		t.Fatalf(
			"outcome-keyed identity rows = %d, want %d for head=%t",
			accuracy.outcomeKeyedRows,
			wantOutcomeKeyed,
			headVariant,
		)
	}
	return accuracy
}

func openContainerImageIdentityPerfDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open performance Postgres: %v", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("ping performance Postgres: %v", err)
	}
	return db
}

func containerImageIdentityPerfSchemaDSN(t *testing.T, dsn string, schema string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse performance Postgres DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
