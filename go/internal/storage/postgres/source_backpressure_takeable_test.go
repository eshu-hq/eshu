// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// takeableCensusQuery classifies each source's claimable reducer rows
// (active generation, pending or retrying, visible now) by whether the reducer
// claim could take them: readiness-blocked (the claim's readiness gate fails),
// conflict-fenced (another claimed or running row holds the same conflict key),
// or takeable. It spells the two claim predicates the same way
// claimReducerWorkQuery does and is a census, not production SQL (#11187).
var takeableCensusQuery = `
WITH ` + activeFactWorkItemsPerRowCTE + `,
` + reducerClaimReadinessRequirementsCTE() + `,
classified AS (
    SELECT COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown') AS source_system,
           NOT ` + reducerClaimReadinessGateSQL("work", "readiness_req", "readiness_phase") + ` AS readiness_blocked,
           EXISTS (
               SELECT 1 FROM fact_work_items AS inflight
               WHERE inflight.stage = 'reducer'
                 AND inflight.conflict_domain = work.conflict_domain
                 AND COALESCE(inflight.conflict_key, inflight.scope_id) = COALESCE(work.conflict_key, work.scope_id)
                 AND inflight.work_item_id <> work.work_item_id
                 AND inflight.status IN ('claimed', 'running')
           ) AS fenced
    FROM active_fact_work_items AS work
    JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id
    WHERE work.stage = 'reducer'
      AND work.status IN ('pending', 'retrying')
      AND (work.visible_at IS NULL OR work.visible_at <= $1)
)
SELECT source_system,
       COUNT(*) FILTER (WHERE readiness_blocked) AS readiness_blocked,
       COUNT(*) FILTER (WHERE NOT readiness_blocked AND fenced) AS fenced,
       COUNT(*) FILTER (WHERE NOT readiness_blocked AND NOT fenced) AS takeable
FROM classified
GROUP BY source_system
ORDER BY source_system
`

type takeableCensusRow struct{ blocked, fenced, takeable int64 }

func readTakeableCensus(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time) map[string]takeableCensusRow {
	t.Helper()
	rows, err := conn.QueryContext(ctx, takeableCensusQuery, now.UTC())
	if err != nil {
		t.Fatalf("takeable census: %v", err)
	}
	defer func() { _ = rows.Close() }()
	census := map[string]takeableCensusRow{}
	for rows.Next() {
		var source string
		var row takeableCensusRow
		if err := rows.Scan(&source, &row.blocked, &row.fenced, &row.takeable); err != nil {
			t.Fatalf("scan takeable census: %v", err)
		}
		census[source] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("takeable census rows: %v", err)
	}
	return census
}

// seedTakeableShape inserts count reducer rows for one source in one domain
// and status, all at one age, under one scope. conflictKey, when set, puts
// every row on that conflict key (conflict_domain "scope").
func seedTakeableShape(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time,
	scopeID, source, domain, status, conflictKey string, count int, age time.Duration,
) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, `INSERT INTO ingestion_scopes (
scope_id, scope_kind, source_system, source_key, parent_scope_id,
collector_kind, partition_key, status, active_generation_id, payload,
observed_at, ingested_at) VALUES ($1,'repository',$2,$1,NULL,'git',$1,'active',$3,'{}'::jsonb,$4,$4)
ON CONFLICT (scope_id) DO NOTHING`, scopeID, source, scopeID+"-gen", now); err != nil {
		t.Fatalf("insert scope %s: %v", scopeID, err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO scope_generations (
generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
ingested_at, status, activated_at, superseded_at, payload)
VALUES ($1,$2,'snapshot','takeable',$3,$3,'active',$3,NULL,'{}'::jsonb)
ON CONFLICT (generation_id) DO NOTHING`, scopeID+"-gen", scopeID, now); err != nil {
		t.Fatalf("insert generation %s: %v", scopeID, err)
	}
	created := now.Add(-age).Format(time.RFC3339)
	for i := 0; i < count; i++ {
		var key any
		if conflictKey != "" {
			key = conflictKey
		}
		var claimUntil, lease any
		if status == "claimed" {
			claimUntil, lease = now.Add(time.Hour).Format(time.RFC3339), "holder"
		}
		payload := fmt.Sprintf(`{"entity_key":"%s-%d","source_system":"%s"}`, scopeID, i, source)
		if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
conflict_key, status, attempt_count, lease_owner, claim_until, visible_at,
last_attempt_at, next_attempt_at, failure_class, failure_message,
failure_details, payload, created_at, updated_at) VALUES (
$1,$2,$3,'reducer',$4,'scope',$5,$6,0,$7,$8,NULL,NULL,NULL,NULL,NULL,NULL,$9::jsonb,$10,$10)`,
			fmt.Sprintf("tk-%s-%s-%d", scopeID, status, i), scopeID, scopeID+"-gen", domain, key, status,
			lease, claimUntil, payload, created); err != nil {
			t.Fatalf("insert work %s/%d: %v", scopeID, i, err)
		}
	}
}

// TestSourceBackpressureTakeableSignal is the #11187 live proof. A source's
// claimable rows that the reducer cannot take now, because the claim's
// readiness gate fails for them, must not hold or raise the latch: with
// intake paced, nothing delivers the canonical phases such rows wait on, so a
// latch counting them would never release (and since #11186 a paced item
// writes no phases at all). Conflict-fenced rows stay counted: they wait on a
// holder the reducer itself finishes, so they drain without intake.
//
//   - blocked: 30 aged readiness-gated rows with no phase state. It never
//     engages, and an engaged latch on it releases.
//   - ready: 30 aged takeable rows. It engages.
//   - fenced: one claimed holder plus 30 aged pending siblings on its key. The
//     siblings are census class fenced, count toward the signal, and release as
//     the reducer drains them.
//
// The census also runs over the 10x replay fixture and the bounded probe must
// equal the grouped query for every source.
func TestSourceBackpressureTakeableSignal(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the takeable-signal proof")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated postgres connection: %v", err)
	}
	defer func() { _ = sqlConn.Close() }()
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("source_bp_tk_%d", time.Now().UnixNano())
	defer func() { _, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	for _, def := range []string{"workflow_control_plane", "workflow_claim_pacing"} {
		if _, err := conn.ExecContext(ctx, MigrationSQL(def)); err != nil {
			t.Fatalf("create %s: %v", def, err)
		}
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	now := time.Now().UTC()
	const gated = "aws_relationship_materialization" // a readiness-gated domain, no phase state seeded
	seedTakeableShape(t, ctx, conn, now, "tk-blocked", "blocked", gated, "pending", "", 30, 30*24*time.Hour)
	seedTakeableShape(t, ctx, conn, now, "tk-ready", "ready", "supply_chain_impact", "pending", "", 30, 30*24*time.Hour)
	seedTakeableShape(t, ctx, conn, now, "tk-fenced", "fenced", "supply_chain_impact", "claimed", "k", 1, 30*24*time.Hour)
	seedTakeableShape(t, ctx, conn, now, "tk-fenced", "fenced", "supply_chain_impact", "pending", "k", 30, 30*24*time.Hour)

	census := readTakeableCensus(t, ctx, conn, now)
	for source, want := range map[string]takeableCensusRow{
		"blocked": {blocked: 30},
		"ready":   {takeable: 30},
		"fenced":  {fenced: 30},
	} {
		if got := census[source]; got != want {
			t.Fatalf("census[%s] = %+v, want %+v", source, got, want)
		}
		t.Logf("takeable census: source=%s readiness_blocked=%d fenced=%d takeable=%d",
			source, census[source].blocked, census[source].fenced, census[source].takeable)
	}

	store := NewWorkflowControlStore(conn)
	cfg := sourceBackpressureTestConfig() // floor 25, engage 1h, release 15m
	grouped, err := store.sourceClaimableSignals(ctx, cfg, now)
	if err != nil {
		t.Fatalf("grouped signals: %v", err)
	}
	bySource := map[string]workflow.SourceQueueSignal{}
	for _, signal := range grouped {
		bySource[signal.SourceSystem] = signal
	}
	for _, source := range []string{"blocked", "ready", "fenced"} {
		bounded, err := store.sourceClaimableSignalForSource(ctx, cfg, source, now)
		if err != nil {
			t.Fatalf("bounded probe %s: %v", source, err)
		}
		want := bySource[source]
		want.SourceSystem = source // a source with no counted rows has no grouped row
		if bounded != want {
			t.Fatalf("bounded signal for %s = %+v, grouped = %+v: the probe must equal the grouped query", source, bounded, want)
		}
	}
	if got := bySource["blocked"]; got.ClaimableDepth != 0 || got.RowsPastEngageAge != 0 {
		t.Fatalf("blocked signal = %+v, want readiness-blocked rows excluded from every count", got)
	}

	blocked, err := store.CheckSourceBackpressureForSource(ctx, "blocked", cfg, now)
	if err != nil || blocked.Slowed {
		t.Fatalf("blocked verdict = %+v, %v; want never engaged", blocked, err)
	}
	ready, err := store.CheckSourceBackpressureForSource(ctx, "ready", cfg, now)
	if err != nil || !ready.Slowed {
		t.Fatalf("ready verdict = %+v, %v; want engaged on takeable aged rows", ready, err)
	}

	// An engaged latch on the blocked source releases: its aged rows are
	// not takeable, so nothing the paced intake holds back can clear them.
	if _, err := conn.ExecContext(ctx, claimPacingLatchUpsert, "reducer_source", "blocked", true, now.Add(-2*time.Hour), now); err != nil {
		t.Fatalf("seed engaged latch: %v", err)
	}
	released, err := store.CheckSourceBackpressureForSource(ctx, "blocked", cfg, now)
	if err != nil || released.Slowed {
		t.Fatalf("blocked verdict with an engaged latch = %+v, %v; want released", released, err)
	}

	// Fenced siblings count and release as the reducer drains them.
	fenced, err := store.CheckSourceBackpressureForSource(ctx, "fenced", cfg, now)
	if err != nil || !fenced.Slowed {
		t.Fatalf("fenced verdict = %+v, %v; want engaged while the siblings are a real backlog", fenced, err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded'
WHERE scope_id = 'tk-fenced' AND status = 'pending'`); err != nil {
		t.Fatalf("drain fenced siblings: %v", err)
	}
	drained, err := store.CheckSourceBackpressureForSource(ctx, "fenced", cfg, now)
	if err != nil || drained.Slowed {
		t.Fatalf("fenced verdict after the reducer drained the siblings = %+v, %v; want released", drained, err)
	}
}
