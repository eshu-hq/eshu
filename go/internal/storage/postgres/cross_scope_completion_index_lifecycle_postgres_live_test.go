// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const crossScopeCompletionSourceIndex = "fact_work_items_cross_scope_source_idx"

func TestCrossScopeCompletionQueueMigrationLifecycleLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	db.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	migration := crossScopeCompletionDefinition(t, "cross_scope_completion_queue")

	if _, err := db.ExecContext(ctx, `
DROP TABLE cross_scope_completion_events;
DROP TABLE cross_scope_completion_upgrade_markers;
ALTER TABLE fact_work_items
    DROP COLUMN cross_scope_replay_required CASCADE,
    DROP COLUMN cross_scope_completion_ack_epoch CASCADE;
DROP FUNCTION enforce_cross_scope_required_replay();
DROP FUNCTION enqueue_cross_scope_completion_event()
`); err != nil {
		t.Fatalf("reset completion queue migration: %v", err)
	}

	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin completion migration blocker: %v", err)
	}
	if _, err := blocker.ExecContext(ctx, "LOCK TABLE fact_work_items IN ROW EXCLUSIVE MODE"); err != nil {
		_ = blocker.Rollback()
		t.Fatalf("lock completion migration target: %v", err)
	}
	applyErr := ApplyDefinitionsWithLockTimeout(
		ctx,
		SQLDB{DB: db},
		[]Definition{migration},
		100*time.Millisecond,
	)
	if applyErr == nil {
		_ = blocker.Rollback()
		t.Fatal("first completion migration under writer lock succeeded, want lock timeout")
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release completion migration blocker: %v", err)
	}
	assertCrossScopeCompletionMigrationAbsent(t, ctx, db)

	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, []Definition{migration}); err != nil {
		t.Fatalf("apply completion queue migration after lock release: %v", err)
	}
	before := readCrossScopeCompletionTriggerOIDs(t, ctx, db)

	const (
		writerID         = "reducer_5740_migration_writer"
		writerScope      = "repository:5740-migration-writer"
		writerGeneration = "generation:5740-migration-writer"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, writerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, writerScope, writerGeneration)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at
) VALUES (
    $1, $2, $3,
    'reducer', 'runtime_resolution', 'pending', 'intent', $1,
    to_jsonb(repeat('x', 32768)), clock_timestamp(), clock_timestamp()
)
`, writerID, writerScope, writerGeneration); err != nil {
		t.Fatalf("seed completion migration writer: %v", err)
	}
	writes := runCrossScopeCompletionWriter(t, ctx, db, writerID, func() error {
		return ApplyDefinitionsWithLockTimeout(
			ctx,
			SQLDB{DB: db},
			[]Definition{migration},
			500*time.Millisecond,
		)
	})
	if writes == 0 {
		t.Fatal("completion migration reapply admitted no concurrent writer")
	}
	after := readCrossScopeCompletionTriggerOIDs(t, ctx, db)
	if before != after {
		t.Fatalf("completion migration reapply changed trigger OIDs: before=%v after=%v", before, after)
	}
}

func TestCrossScopeCompletionSourceIndexLifecycleLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	db.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	const (
		scopeID    = "repository:5740-index-lifecycle"
		generation = "generation:5740-index-lifecycle"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate index-lifecycle scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at
)
SELECT 'reducer_5740_index_' || series, $1, $2, 'reducer',
       CASE WHEN series % 2 = 0 THEN 'ci_cd_run_correlation' ELSE 'supply_chain_impact' END,
       'succeeded', 'intent', 'reducer_5740_index_' || series,
       '{}'::jsonb, $3, $3
FROM generate_series(1, 50000) AS series
`, scopeID, generation, now); err != nil {
		t.Fatalf("seed populated completion index: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+crossScopeCompletionSourceIndex); err != nil {
		t.Fatalf("drop completion source index before lifecycle proof: %v", err)
	}
	migration := crossScopeCompletionIndexDefinition(t)

	writerCtx, stopWriter := context.WithCancel(ctx)
	var writes atomic.Int64
	writerDone := make(chan error, 1)
	writerStarted := make(chan struct{})
	go func() {
		for writerCtx.Err() == nil {
			if _, err := db.ExecContext(writerCtx, `
UPDATE fact_work_items
SET updated_at = clock_timestamp()
WHERE work_item_id = 'reducer_5740_index_1'
`); err != nil {
				if writerCtx.Err() != nil {
					writerDone <- nil
					return
				}
				writerDone <- err
				return
			}
			if writes.Add(1) == 1 {
				close(writerStarted)
			}
		}
		writerDone <- nil
	}()
	select {
	case <-writerStarted:
	case err := <-writerDone:
		stopWriter()
		t.Fatalf("completion source index writer before build: %v", err)
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, []Definition{migration}); err != nil {
		stopWriter()
		<-writerDone
		t.Fatalf("apply populated completion source index: %v", err)
	}
	stopWriter()
	if err := <-writerDone; err != nil {
		t.Fatalf("completion source index concurrent writer: %v", err)
	}
	if writes.Load() == 0 {
		t.Fatal("completion source index build admitted no concurrent writer")
	}
	assertCrossScopeCompletionIndexReady(t, ctx, db)
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, []Definition{migration}); err != nil {
		t.Fatalf("reapply completion source index: %v", err)
	}
	assertCrossScopeCompletionIndexReady(t, ctx, db)

	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+crossScopeCompletionSourceIndex); err != nil {
		t.Fatalf("drop completion index before invalid recovery: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE UNIQUE INDEX CONCURRENTLY fact_work_items_cross_scope_source_idx
ON fact_work_items (domain)
WHERE stage = 'reducer'
  AND status IN ('claimed', 'running', 'succeeded')
  AND domain IN ('ci_cd_run_correlation', 'supply_chain_impact')
`); err == nil {
		t.Fatal("invalid same-name completion index creation unexpectedly succeeded")
	}
	if valid := crossScopeCompletionIndexValid(t, ctx, db); valid {
		t.Fatal("failed same-name completion index is valid, want invalid")
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, []Definition{migration}); err != nil {
		t.Fatalf("recover invalid completion source index: %v", err)
	}
	assertCrossScopeCompletionIndexReady(t, ctx, db)
	assertCrossScopeCompletionIndexUsed(t, ctx, db, scopeID, generation)
}

func crossScopeCompletionIndexDefinition(t *testing.T) Definition {
	return crossScopeCompletionDefinition(t, "fact_work_items_cross_scope_source_idx")
}

func crossScopeCompletionDefinition(t *testing.T, name string) Definition {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("completion definition %q missing", name)
	return Definition{}
}

func assertCrossScopeCompletionMigrationAbsent(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range []string{
		"cross_scope_completion_events",
		"cross_scope_completion_upgrade_markers",
	} {
		var tableName sql.NullString
		if err := db.QueryRowContext(
			ctx,
			"SELECT to_regclass(current_schema() || '.' || $1)::text",
			table,
		).Scan(&tableName); err != nil {
			t.Fatalf("read completion table %s after failed migration: %v", table, err)
		}
		if tableName.Valid {
			t.Fatalf("failed completion migration retained table %q", tableName.String)
		}
	}
	var columnCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_attribute
WHERE attrelid = 'fact_work_items'::regclass
  AND attname IN ('cross_scope_replay_required', 'cross_scope_completion_ack_epoch')
  AND NOT attisdropped
`).Scan(&columnCount); err != nil {
		t.Fatalf("read completion columns after failed migration: %v", err)
	}
	if columnCount != 0 {
		t.Fatalf("failed completion migration retained %d columns", columnCount)
	}
	var functionCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_proc AS procedure
JOIN pg_namespace AS namespace ON namespace.oid = procedure.pronamespace
WHERE namespace.nspname = current_schema()
  AND procedure.proname IN (
      'enforce_cross_scope_required_replay',
      'enqueue_cross_scope_completion_event'
  )
`).Scan(&functionCount); err != nil {
		t.Fatalf("read completion functions after failed migration: %v", err)
	}
	if functionCount != 0 {
		t.Fatalf("failed completion migration retained %d functions", functionCount)
	}
	var triggerCount int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_trigger
WHERE tgrelid = 'fact_work_items'::regclass
  AND tgname IN (
      'fact_work_items_enforce_cross_scope_required_replay',
      'fact_work_items_cross_scope_completion'
  )
  AND NOT tgisinternal
`).Scan(&triggerCount); err != nil {
		t.Fatalf("read completion triggers after failed migration: %v", err)
	}
	if triggerCount != 0 {
		t.Fatalf("failed completion migration retained %d triggers", triggerCount)
	}
}

type crossScopeCompletionTriggerOIDs struct {
	replay int64
	event  int64
}

func readCrossScopeCompletionTriggerOIDs(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) crossScopeCompletionTriggerOIDs {
	t.Helper()
	var oids crossScopeCompletionTriggerOIDs
	if err := db.QueryRowContext(ctx, `
SELECT
    MAX(oid) FILTER (WHERE tgname = 'fact_work_items_enforce_cross_scope_required_replay'),
    MAX(oid) FILTER (WHERE tgname = 'fact_work_items_cross_scope_completion')
FROM pg_trigger
WHERE tgrelid = 'fact_work_items'::regclass
  AND NOT tgisinternal
`).Scan(&oids.replay, &oids.event); err != nil {
		t.Fatalf("read completion trigger OIDs: %v", err)
	}
	if oids.replay == 0 || oids.event == 0 {
		t.Fatalf("completion trigger OIDs incomplete: %v", oids)
	}
	return oids
}

func runCrossScopeCompletionWriter(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	whileWriting func() error,
) int64 {
	t.Helper()
	writerCtx, stopWriter := context.WithCancel(ctx)
	var writes atomic.Int64
	writerDone := make(chan error, 1)
	writerStarted := make(chan struct{})
	go func() {
		for writerCtx.Err() == nil {
			if _, err := db.ExecContext(writerCtx, `
UPDATE fact_work_items SET updated_at = clock_timestamp() WHERE work_item_id = $1
`, workItemID); err != nil {
				if writerCtx.Err() != nil {
					writerDone <- nil
					return
				}
				writerDone <- err
				return
			}
			if writes.Add(1) == 1 {
				close(writerStarted)
			}
		}
		writerDone <- nil
	}()
	select {
	case <-writerStarted:
	case err := <-writerDone:
		stopWriter()
		t.Fatalf("completion definition writer before apply: %v", err)
	}
	err := whileWriting()
	stopWriter()
	writerErr := <-writerDone
	if err != nil {
		t.Fatalf("apply completion definition with concurrent writer: %v", err)
	}
	if writerErr != nil {
		t.Fatalf("completion definition concurrent writer: %v", writerErr)
	}
	return writes.Load()
}

func crossScopeCompletionIndexValid(t *testing.T, ctx context.Context, db *sql.DB) bool {
	t.Helper()
	var valid bool
	if err := db.QueryRowContext(ctx, `
SELECT index_state.indisvalid AND index_state.indisready
FROM pg_index AS index_state
JOIN pg_class AS index_class ON index_class.oid = index_state.indexrelid
JOIN pg_namespace AS namespace ON namespace.oid = index_class.relnamespace
WHERE index_class.relname = $1
  AND namespace.nspname = current_schema()
`, crossScopeCompletionSourceIndex).Scan(&valid); err != nil {
		t.Fatalf("read completion source index state: %v", err)
	}
	return valid
}

func assertCrossScopeCompletionIndexReady(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if !crossScopeCompletionIndexValid(t, ctx, db) {
		t.Fatal("completion source index is not valid and ready")
	}
}

func assertCrossScopeCompletionIndexUsed(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generation string,
) {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve connection for completion source explain: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatalf("disable sequential scans for completion source explain: %v", err)
	}
	defer func() {
		resetCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, resetErr := conn.ExecContext(resetCtx, "RESET enable_seqscan"); resetErr != nil {
			t.Errorf("reset completion source explain planner setting: %v", resetErr)
		}
	}()
	rows, err := conn.QueryContext(ctx, `
EXPLAIN SELECT work_item_id, status, cross_scope_replay_required
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain = 'supply_chain_impact'
  AND scope_id = $1
  AND generation_id = $2
  AND status IN ('claimed', 'running', 'succeeded')
`, scopeID, generation)
	if err != nil {
		t.Fatalf("explain completion source query: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan completion source plan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("completion source plan rows: %v", err)
	}
	if joined := strings.Join(plan, "\n"); !strings.Contains(joined, crossScopeCompletionSourceIndex) {
		t.Fatalf("completion source plan does not use %s:\n%s", crossScopeCompletionSourceIndex, joined)
	}
}
