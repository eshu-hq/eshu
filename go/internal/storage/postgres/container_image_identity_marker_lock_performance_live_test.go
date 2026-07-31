// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build perf5854_ack

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestContainerImageIdentityMarkerLockPerformanceLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 20, 0, 0, 0, time.UTC)
	const (
		historicalRows  = 100000
		samples         = 100
		handlerBaseline = 26023 * time.Microsecond
	)

	historicalScope := "repository:5854-marker-lock-historical"
	historicalGeneration := "generation:5854-marker-lock-historical"
	seedContainerImageIdentityAckScope(t, ctx, db, historicalScope)
	seedContainerImageIdentityAckGeneration(
		t, ctx, db, historicalScope, historicalGeneration,
	)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count,
    lease_owner, claim_until, payload, created_at, updated_at
)
SELECT
    'ack-5854-marker-history-' || series,
    $1,
    $2,
    'reducer',
    'container_image_identity',
    'intent',
    'ack-5854-marker-history-' || series,
    'claimed',
    1,
    'reducer-5854-marker-history',
    $3::timestamptz + interval '1 minute',
    jsonb_build_object('reason', 'synthetic marker lock history'),
    $3::timestamptz,
    $3::timestamptz
FROM generate_series(1, $4) AS series
`, historicalScope, historicalGeneration, now, historicalRows); err != nil {
		t.Fatalf("seed marker-lock historical queue: %v", err)
	}

	var (
		scopeOne      string
		generationOne string
	)
	for _, activeRows := range []int{0, 1} {
		scopeID := fmt.Sprintf("repository:5854-marker-lock-%d", activeRows)
		generationID := fmt.Sprintf("generation:5854-marker-lock-%d", activeRows)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
		for index := range activeRows {
			seedContainerImageIdentityAckWorkItem(
				t,
				ctx,
				db,
				fmt.Sprintf("ack-5854-marker-lock-%d-%02d", activeRows, index),
				scopeID,
				generationID,
				"reducer-5854-marker-lock",
				now.Add(time.Minute),
				now,
			)
		}
		if _, err := db.ExecContext(ctx, "ANALYZE fact_work_items"); err != nil {
			t.Fatalf("analyze marker-lock queue for %d active rows: %v", activeRows, err)
		}
		plan := explainContainerImageIdentityMarkerLock(
			t, ctx, db, scopeID, generationID,
		)
		indexBacked := strings.Contains(
			plan,
			"fact_work_items_scope_generation_idx",
		) || activeRows == 0 && strings.Contains(plan, "Index Scan using")
		if strings.Contains(plan, "Seq Scan on fact_work_items") || !indexBacked {
			t.Fatalf(
				"marker lock plan for %d active rows is not scope-generation index-backed:\n%s",
				activeRows,
				plan,
			)
		}
		if activeRows == 1 {
			scopeOne = scopeID
			generationOne = generationID
		}
	}

	durations := make([]time.Duration, 0, samples)
	for range samples {
		if _, err := db.ExecContext(ctx, `
DELETE FROM container_image_identity_cutovers
WHERE scope_id = $1
  AND generation_id = $2
`, scopeOne, generationOne); err != nil {
			t.Fatalf("reset marker-lock sample marker: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed',
    container_image_identity_v2_required = FALSE,
    container_image_identity_v2_authorized_status = ''
WHERE scope_id = $1
  AND generation_id = $2
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
`, scopeOne, generationOne); err != nil {
			t.Fatalf("reset marker-lock sample queue fence: %v", err)
		}
		started := time.Now()
		if _, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id,
    generation_id,
    activated_by_work_item_id,
    activated_by_claim_epoch
)
SELECT
    $1,
    $2,
    work_item_id,
    container_image_identity_claim_epoch
FROM fact_work_items
WHERE scope_id = $1
  AND generation_id = $2
  AND stage = 'reducer'
  AND domain = 'container_image_identity'
`, scopeOne, generationOne); err != nil {
			t.Fatalf("measure marker-lock insert: %v", err)
		}
		durations = append(durations, time.Since(started))
	}
	stats := ackPerfStats(durations)
	contributionBudget := time.Duration(float64(handlerBaseline) * 0.05)
	if stats.p95 > contributionBudget {
		t.Errorf(
			"one-row marker fence p95 = %s, exceeds 5%% handler contribution budget %s",
			stats.p95,
			contributionBudget,
		)
	}
	t.Logf(
		"MARKERLOCK5854 historical_rows=%d active_rows=1 samples=%d median_us=%.3f p95_us=%.3f handler_baseline_ms=%.3f contribution_budget_us=%.3f",
		historicalRows,
		samples,
		ackPerfMicros(stats.median),
		ackPerfMicros(stats.p95),
		ackPerfMillis(handlerBaseline),
		ackPerfMicros(contributionBudget),
	)
}

func explainContainerImageIdentityMarkerLock(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) string {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin marker-lock EXPLAIN transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
EXPLAIN (ANALYZE, BUFFERS, WAL)
WITH locked_work_item AS MATERIALIZED (
    SELECT work_item_id
    FROM fact_work_items
    WHERE scope_id = $1
      AND generation_id = $2
      AND stage = 'reducer'
      AND domain = 'container_image_identity'
    ORDER BY work_item_id
    FOR UPDATE
)
UPDATE fact_work_items AS work_item
SET status = 'running',
    container_image_identity_v2_required = TRUE,
    container_image_identity_v2_authorized_status = 'running'
FROM locked_work_item
WHERE work_item.work_item_id = locked_work_item.work_item_id
`, scopeID, generationID)
	if err != nil {
		t.Fatalf("EXPLAIN marker-lock query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan marker-lock plan: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read marker-lock plan: %v", err)
	}
	return strings.Join(lines, "\n")
}
