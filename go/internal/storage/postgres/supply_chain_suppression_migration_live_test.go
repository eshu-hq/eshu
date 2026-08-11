// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSupplyChainSuppressionExpiryMigrationUpgradeLive(t *testing.T) {
	ctx, db := openSuppressionSQLProofDB(t)
	applySuppressionSQLProofDefinitions(t, ctx, db)
	if _, err := db.ExecContext(
		ctx,
		`ALTER TABLE supply_chain_impact_canonical_winners
		   DROP COLUMN suppression_expires_at`,
	); err != nil {
		t.Fatalf("create pre-081 winners shape: %v", err)
	}
	seedSuppressionExpiryMigrationRows(t, ctx, db)

	migrationSQL, err := embeddedMigrations.ReadFile(
		"migrations/083_supply_chain_suppression_expiry.sql",
	)
	if err != nil {
		t.Fatalf("read migration 083: %v", err)
	}

	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin migration lock blocker: %v", err)
	}
	if _, err := blocker.ExecContext(
		ctx,
		`LOCK TABLE supply_chain_impact_canonical_winners IN ACCESS SHARE MODE`,
	); err != nil {
		_ = blocker.Rollback()
		t.Fatalf("lock winners table: %v", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = blocker.Rollback()
		t.Fatalf("open migration contender connection: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `SET lock_timeout = '100ms'`); err != nil {
		_ = conn.Close()
		_ = blocker.Rollback()
		t.Fatalf("set migration lock timeout: %v", err)
	}
	_, lockErr := conn.ExecContext(ctx, string(migrationSQL))
	_ = conn.Close()
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release migration lock blocker: %v", err)
	}
	if lockErr == nil || !strings.Contains(lockErr.Error(), "lock timeout") {
		t.Fatalf("migration under conflicting lock = %v, want lock timeout", lockErr)
	}
	if suppressionExpiryColumnExists(t, ctx, db) {
		t.Fatal("failed migration left suppression_expires_at behind")
	}

	firstStart := time.Now()
	if _, err := db.ExecContext(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("apply migration 083: %v", err)
	}
	firstDuration := time.Since(firstStart)
	first := suppressionExpiryMigrationSnapshot(t, ctx, db)
	want := map[string]string{
		"operator-valid":     "2026-08-01 00:00:00+00",
		"operator-malformed": "-infinity",
		"operator-missing":   "NULL",
		"operator-active":    "NULL",
		"provider-valid":     "NULL",
	}
	if fmt.Sprint(first) != fmt.Sprint(want) {
		t.Fatalf("first migration snapshot = %#v, want %#v", first, want)
	}
	var totalRows, backfilledRows int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*), COUNT(*) FILTER (WHERE suppression_expires_at IS NOT NULL)
FROM supply_chain_impact_canonical_winners`).Scan(&totalRows, &backfilledRows); err != nil {
		t.Fatalf("count migrated winners: %v", err)
	}
	if totalRows != suppressionSQLProofRows || backfilledRows != 2 {
		t.Fatalf(
			"migrated winners total/backfilled = %d/%d, want %d/2",
			totalRows,
			backfilledRows,
			suppressionSQLProofRows,
		)
	}

	repeatStart := time.Now()
	if _, err := db.ExecContext(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("reapply migration 083: %v", err)
	}
	repeatDuration := time.Since(repeatStart)
	repeated := suppressionExpiryMigrationSnapshot(t, ctx, db)
	if fmt.Sprint(repeated) != fmt.Sprint(first) {
		t.Fatalf("repeat migration changed snapshot: first=%#v repeat=%#v", first, repeated)
	}

	updateStart := bytes.Index(migrationSQL, []byte("UPDATE "))
	if updateStart < 0 {
		t.Fatal("migration 083 has no UPDATE statement")
	}
	var rawPlan []byte
	if err := db.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+string(migrationSQL[updateStart:]),
	).Scan(&rawPlan); err != nil {
		t.Fatalf("explain migration 081 backfill: %v", err)
	}
	var plans []suppressionSQLPlan
	if err := json.Unmarshal(rawPlan, &plans); err != nil {
		t.Fatalf("decode migration 081 plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("migration 081 plans = %d, want 1", len(plans))
	}
	if plans[0].Plan.SharedReadBlocks != 0 {
		t.Fatalf(
			"migration 081 read %d shared blocks after warmup",
			plans[0].Plan.SharedReadBlocks,
		)
	}
	t.Logf(
		"migration 083 on %d winners first=%s repeat=%s explain=%.3fms; "+
			"backfilled_operator_rows=2 unrelated_or_ineligible_rows_unchanged=%d "+
			"shared_hit_blocks=%d shared_read_blocks=%d",
		totalRows,
		firstDuration,
		repeatDuration,
		plans[0].ExecutionTime,
		totalRows-backfilledRows,
		plans[0].Plan.SharedHitBlocks,
		plans[0].Plan.SharedReadBlocks,
	)
}

func seedSuppressionExpiryMigrationRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES (
  'scope:5465:migration', 'vulnerability_intelligence', 'synthetic',
  'scope:5465:migration', 'synthetic', 'scope:5465:migration',
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z', 'active',
  'generation:5465:migration', '{}'::jsonb
);
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES (
  'generation:5465:migration', 'scope:5465:migration', 'synthetic',
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z',
  'active', '2026-07-27T12:00:00Z', '{}'::jsonb
);
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES
  ('fact:operator-valid', 'scope:5465:migration', 'generation:5465:migration',
   'vulnerability.suppression', 'operator-valid', 'synthetic', 'operator-valid',
   now(), now(), '{"suppression":{"expires_at":"2026-08-01T00:00:00Z"}}'),
  ('fact:operator-malformed', 'scope:5465:migration', 'generation:5465:migration',
   'vulnerability.suppression', 'operator-malformed', 'synthetic', 'operator-malformed',
   now(), now(), '{"suppression":{"expires_at":"not-a-time"}}'),
  ('fact:operator-missing', 'scope:5465:migration', 'generation:5465:migration',
   'vulnerability.suppression', 'operator-missing', 'synthetic', 'operator-missing',
   now(), now(), '{"suppression":{}}'),
  ('fact:operator-active', 'scope:5465:migration', 'generation:5465:migration',
   'vulnerability.suppression', 'operator-active', 'synthetic', 'operator-active',
   now(), now(), '{"suppression":{"expires_at":"2026-08-02T00:00:00Z"}}'),
  ('fact:provider-valid', 'scope:5465:migration', 'generation:5465:migration',
   'vulnerability.suppression', 'provider-valid', 'synthetic', 'provider-valid',
   now(), now(), '{"suppression":{"expires_at":"2026-08-03T00:00:00Z"}}');
INSERT INTO supply_chain_impact_canonical_winners (
  canonical_key, winner_fact_id, winner_scope_id, finding_id,
  suppression_state, materialized_at
) VALUES
  ('operator-valid', 'fact:operator-valid', 'operator:vulnerability_suppressions',
   'finding:operator-valid', 'accepted_risk', now()),
  ('operator-malformed', 'fact:operator-malformed', 'operator:vulnerability_suppressions',
   'finding:operator-malformed', 'ignored', now()),
  ('operator-missing', 'fact:operator-missing', 'operator:vulnerability_suppressions',
   'finding:operator-missing', 'false_positive', now()),
  ('operator-active', 'fact:operator-active', 'operator:vulnerability_suppressions',
   'finding:operator-active', 'active', now()),
  ('provider-valid', 'fact:provider-valid', 'scope:provider',
   'finding:provider-valid', 'accepted_risk', now());`); err != nil {
		t.Fatalf("seed pre-081 rows: %v", err)
	}
	if _, err := db.ExecContext(
		ctx, `
INSERT INTO supply_chain_impact_canonical_winners (
  canonical_key, winner_fact_id, winner_scope_id, finding_id,
  suppression_state, materialized_at
)
SELECT
  format('provider-bulk:%06s', n),
  'fact:provider-valid',
  'scope:provider',
  format('finding:provider-bulk:%06s', n),
  'accepted_risk',
  now()
FROM generate_series(1, $1::integer) AS n;`,
		suppressionSQLProofRows-5,
	); err != nil {
		t.Fatalf("seed pre-081 bulk winners: %v", err)
	}
}

func suppressionExpiryColumnExists(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = current_schema()
    AND table_name = 'supply_chain_impact_canonical_winners'
    AND column_name = 'suppression_expires_at'
)`).Scan(&exists); err != nil {
		t.Fatalf("query suppression expiry column: %v", err)
	}
	return exists
}

func suppressionExpiryMigrationSnapshot(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT canonical_key, COALESCE(suppression_expires_at::text, 'NULL')
FROM supply_chain_impact_canonical_winners
WHERE canonical_key IN (
  'operator-valid',
  'operator-malformed',
  'operator-missing',
  'operator-active',
  'provider-valid'
)
ORDER BY canonical_key`)
	if err != nil {
		t.Fatalf("query migration snapshot: %v", err)
	}
	defer func() { _ = rows.Close() }()
	snapshot := map[string]string{}
	for rows.Next() {
		var key, expiry string
		if err := rows.Scan(&key, &expiry); err != nil {
			t.Fatalf("scan migration snapshot: %v", err)
		}
		snapshot[key] = expiry
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migration snapshot: %v", err)
	}
	return snapshot
}
