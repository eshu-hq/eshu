// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9227 disposable PostgreSQL API proof that only a committed, current, complete NVD snapshot certifies zero findings
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

func TestSupplyChainImpactReadinessCommittedCoverageEdgesLive(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, context.Context, *sql.DB, time.Time)
	}{
		{"legacy missing descriptor", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE fact_records SET payload = payload - 'coverage_descriptor'::text`)
		}},
		{"legacy missing endpoint", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE fact_records SET payload = JSONB_SET(payload,
                '{coverage_descriptor}', (payload->'coverage_descriptor') - 'executed_endpoint'::text)`)
		}},
		{"incomplete snapshot", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE fact_records SET payload = JSONB_SET(payload, '{complete}', 'false'::jsonb)`)
		}},
		{"warning snapshot", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE fact_records SET payload = JSONB_SET(payload, '{warning_code}', '"partial"'::jsonb)`)
		}},
		{"nonzero result snapshot", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE fact_records SET payload = JSONB_SET(payload, '{result_count}', '1'::jsonb)`)
		}},
		{"string zero result snapshot", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE fact_records SET payload = JSONB_SET(payload, '{result_count}', '"0"'::jsonb)`)
		}},
		{"tombstoned snapshot", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE fact_records SET is_tombstone = TRUE`)
		}},
		{"inactive generation", func(t *testing.T, ctx context.Context, db *sql.DB, _ time.Time) {
			execReadinessCoverageMutation(t, ctx, db, `UPDATE scope_generations SET status = 'superseded'`)
		}},
		{"expired snapshot", func(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) {
			_, err := db.ExecContext(ctx, `UPDATE fact_records
                SET observed_at = $1, payload = JSONB_SET(payload, '{cache_updated_at}', TO_JSONB($2::text))`,
				now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour).Format(time.RFC3339))
			if err != nil {
				t.Fatalf("expire source snapshot: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedReadinessScopeInstance(t, ctx, db, now, "coverage-edge", []map[string]any{{
				"source": "nvd", "scope_id": "coverage-edge-scope", "cve_ids": []string{"CVE-2026-9227"},
			}})
			seedReadinessScopeState(t, ctx, db, now, "coverage-edge", "coverage-edge-scope", "nvd", "fresh", "succeeded")
			seedReadinessNVDCommittedSnapshot(t, ctx, db, now, "coverage-edge-scope", "coverage-edge", "CVE-2026-9227")
			tc.mutate(t, ctx, db, now)
			readiness := readReadinessScopeAPI(t, db, "CVE-2026-9227")
			if readiness.State != impact.ReadinessStateTargetIncomplete {
				t.Fatalf("%s readiness = %+v, want target_incomplete", tc.name, readiness)
			}
		})
	}
}

func execReadinessCoverageMutation(t *testing.T, ctx context.Context, db *sql.DB, command string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, command); err != nil {
		t.Fatalf("mutate committed coverage: %v", err)
	}
}

func TestSupplyChainImpactReadinessRolledBackGenerationCannotCertifyZeroLive(t *testing.T) {
	ctx, database, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, database, now, "rollback-instance", []map[string]any{{
		"source": "nvd", "scope_id": "rollback-scope", "cve_ids": []string{"CVE-2026-9227"},
	}})
	seedReadinessScopeState(t, ctx, database, now, "rollback-instance", "rollback-scope", "nvd", "fresh", "succeeded")
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin simulated generation commit: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ingestion_scopes
        (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
        VALUES ('rollback-scope', 'vulnerability_intelligence', 'vulnerability_intelligence', 'rollback-scope',
            'vulnerability_intelligence', 'rollback-scope', $1, $1, 'active')`, now); err != nil {
		t.Fatalf("insert rolled-back ingestion scope: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO scope_generations
        (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
        VALUES ('rollback-scope', 'rolled-back-generation', 'schedule', $1, $1, 'active')`, now); err != nil {
		t.Fatalf("insert rolled-back generation: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ingestion_scopes
        SET active_generation_id = 'rolled-back-generation' WHERE scope_id = 'rollback-scope'`); err != nil {
		t.Fatalf("activate rolled-back generation: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fact_records
        (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
         source_fact_key, observed_at, ingested_at, is_tombstone, payload)
        VALUES ('rolled-back-snapshot', 'rollback-scope', 'rolled-back-generation',
            'vulnerability.source_snapshot', 'rolled-back-snapshot', 'vulnerability_intelligence',
            'rolled-back-snapshot', $1, $1, FALSE,
            '{"source":"nvd","collector_instance_id":"rollback-instance","complete":true,"result_count":0}')`, now); err != nil {
		t.Fatalf("insert rolled-back source snapshot: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback failed generation commit: %v", err)
	}
	readiness := readReadinessScopeAPI(t, database, "CVE-2026-9227")
	if readiness.State != impact.ReadinessStateTargetIncomplete {
		t.Fatalf("rolled-back generation certified zero: %+v", readiness)
	}
}
