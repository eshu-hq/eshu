//go:build perf5854_legacy

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func containerImageIdentityLegacyPerfRows(
	prefix string,
	scopeID string,
	generationID string,
	count int,
) []reducerFactRow {
	rows := make([]reducerFactRow, 0, count)
	now := time.Date(2026, time.July, 30, 1, 0, 0, 0, time.UTC)
	for index := range count {
		factID := fmt.Sprintf(
			"reducer_container_image_identity:5854-legacy-perf:%s:%06d",
			prefix,
			index,
		)
		row := containerImageIdentityLegacyLiveRow(factID, 0, false)
		row.ScopeID = scopeID
		row.GenerationID = generationID
		row.SourceFactKey = "5854-legacy-perf:" + prefix
		row.ObservedAt = now
		row.IngestedAt = now
		rows = append(rows, row)
	}
	return rows
}

func containerImageIdentityLegacyPerfScenarioRows(
	prefix string,
	scopeID string,
	generationID string,
	scenario containerImageIdentityLegacyPerfScenario,
) []reducerFactRow {
	rows := containerImageIdentityLegacyPerfRows(
		prefix,
		scopeID,
		generationID,
		scenario.rows,
	)
	if !scenario.unrelated {
		return rows
	}
	for index := range rows {
		rows[index].FactKind = "reducer_ownership"
		rows[index].Payload = `{"subject":"synthetic-unrelated"}`
	}
	return rows
}

func preseedContainerImageIdentityLegacyPerfRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	rows []reducerFactRow,
	fraction float64,
) {
	t.Helper()
	count := int(float64(len(rows)) * fraction)
	if count == 0 {
		return
	}
	if err := reducerBatchInsertFacts(ctx, db, rows[:count]); err != nil {
		t.Fatalf("preseed legacy performance conflicts: %v", err)
	}
}

func seedContainerImageIdentityLegacyPerfParents(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    $1, 'repository', 'git', $1, 'reducer', $1,
    clock_timestamp(), clock_timestamp(), 'active'
)
ON CONFLICT (scope_id) DO NOTHING
`, scopeID); err != nil {
		t.Fatalf("seed legacy perf scope %s: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    $2, $1, 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
)
ON CONFLICT (generation_id) DO NOTHING
`, scopeID, generationID); err != nil {
		t.Fatalf("seed legacy perf generation %s: %v", generationID, err)
	}
	seedContainerImageIdentityLiveWorkItem(t, ctx, db, scopeID, generationID)
}

func insertContainerImageIdentityLegacyPerfMarker(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	generationID string,
) {
	t.Helper()
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
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeID, generationID); err != nil {
		t.Fatalf("seed legacy perf marker: %v", err)
	}
}

func disableContainerImageIdentityLegacyGuard(t *testing.T, db *sql.DB) {
	t.Helper()
	setContainerImageIdentityLegacyGuard(t, db, false)
}

func enableContainerImageIdentityLegacyGuard(t *testing.T, db *sql.DB) {
	t.Helper()
	setContainerImageIdentityLegacyGuard(t, db, true)
}

func setContainerImageIdentityLegacyGuard(
	t *testing.T,
	db *sql.DB,
	enabled bool,
) {
	t.Helper()
	action := "DISABLE"
	if enabled {
		action = "ENABLE"
	}
	for _, trigger := range containerImageIdentityLegacyGuardTriggers {
		if _, err := db.Exec(
			"ALTER TABLE fact_records " + action + " TRIGGER " + trigger,
		); err != nil {
			t.Fatalf("%s legacy cutover guard %s: %v", action, trigger, err)
		}
	}
}

func deleteContainerImageIdentityLegacyPerfRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	prefix string,
) {
	t.Helper()
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM fact_records WHERE source_fact_key = $1`,
		"5854-legacy-perf:"+prefix,
	); err != nil {
		t.Fatalf("delete legacy perf rows %s: %v", prefix, err)
	}
}

func assertContainerImageIdentityLegacyPerfRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	prefix string,
	want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(
		ctx,
		`SELECT count(*) FROM fact_records WHERE source_fact_key = $1`,
		"5854-legacy-perf:"+prefix,
	).Scan(&got); err != nil {
		t.Fatalf("count legacy perf rows %s: %v", prefix, err)
	}
	if got != want {
		t.Fatalf("legacy perf rows %s = %d, want %d", prefix, got, want)
	}
}

func containerImageIdentityLegacyPerfWAL(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) string {
	t.Helper()
	var lsn string
	if err := db.QueryRowContext(ctx, "SELECT pg_current_wal_insert_lsn()::text").Scan(&lsn); err != nil {
		t.Fatalf("read legacy perf WAL position: %v", err)
	}
	return lsn
}

func checkpointContainerImageIdentityLegacyPerf(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		t.Fatalf("checkpoint legacy performance database: %v", err)
	}
}

func containerImageIdentityLegacyPerfWALDiff(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	after string,
	before string,
) float64 {
	t.Helper()
	var bytes float64
	if err := db.QueryRowContext(
		ctx,
		"SELECT pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)",
		after,
		before,
	).Scan(&bytes); err != nil {
		t.Fatalf("read legacy perf WAL delta: %v", err)
	}
	return bytes
}

func containerImageIdentityLegacyPerfTriggerCalls(
	explain containerImageIdentityLegacyExplain,
) int64 {
	var calls int64
	for _, trigger := range explain.Triggers {
		if strings.Contains(trigger.Name, containerImageIdentityLegacyGuardTrigger) {
			calls += trigger.Calls
		}
	}
	return calls
}

func containerImageIdentityLegacyPerfTriggerTime(
	explain containerImageIdentityLegacyExplain,
) float64 {
	var duration float64
	for _, trigger := range explain.Triggers {
		if strings.Contains(trigger.Name, containerImageIdentityLegacyGuardTrigger) {
			duration += trigger.Time
		}
	}
	return duration
}

func containerImageIdentityLegacyPerfMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func cleanupContainerImageIdentityLegacyPerfScope(
	t *testing.T,
	db *sql.DB,
	scopeID string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.ExecContext(
		ctx,
		"DELETE FROM ingestion_scopes WHERE scope_id = $1",
		scopeID,
	); err != nil {
		t.Errorf("clean legacy perf scope %s: %v", scopeID, err)
	}
}
