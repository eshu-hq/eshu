// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func seedAuditClockWorkItem(t *testing.T, ctx context.Context, db *sql.DB, workItemID, scopeID, generationID string, claimedAt time.Time, status string) {
	t.Helper()
	claimUntil := time.Now().UTC().Add(time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key, status, attempt_count, payload, created_at, updated_at, visible_at, last_attempt_at, claim_until) VALUES ($1,$2,$3,'reducer','aws_cloud_runtime_drift','scope',$1,$4,1,'{}'::jsonb,$5,$5,$5,$5,$6) ON CONFLICT (work_item_id) DO UPDATE SET status = EXCLUDED.status, last_attempt_at = EXCLUDED.last_attempt_at, claim_until = EXCLUDED.claim_until, attempt_count = fact_work_items.attempt_count + 1`,
		workItemID, scopeID, generationID, status, claimedAt, claimUntil); err != nil {
		t.Fatalf("seed work item: %v", err)
	}
}

// TestAWSDriftReopenFingerprintAuditClockLive pins the D5 record contract
// against real Postgres: a full run advances last_audit_at, a delta run
// preserves it (even when the delta carries a later claim), and the loader
// returns the clock the planner audits off.
func TestAWSDriftReopenFingerprintAuditClockLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftIsolatedLiveDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	suffix := fmt.Sprintf("audit-%d", time.Now().UnixNano())
	scopeID := "aws:" + suffix
	generationID := "gen-" + suffix
	workItemID := "work-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)

	store := NewIngestionStore(SQLDB{DB: sqlDB})
	snapshot := reducercontract.AWSDriftReopenSnapshot{
		ScopeID: scopeID, GenerationID: generationID,
		ARNIDs: []string{"arn:1"}, ARNPayloadHashes: []string{"h1"},
		Fingerprint: "fp-1",
	}

	fullAt := now.Add(-time.Hour)
	seedAuditClockWorkItem(t, ctx, sqlDB, workItemID, scopeID, generationID, fullAt, "claimed")
	if err := store.RecordAWSDriftReopenFingerprint(ctx, workItemID, fullAt, snapshot, true); err != nil {
		t.Fatalf("record full: %v", err)
	}
	_, gotFull, ok, err := store.LoadAWSDriftRecordedSnapshot(ctx, workItemID)
	if err != nil || !ok {
		t.Fatalf("load after full: ok=%v err=%v", ok, err)
	}
	if gotFull.IsZero() {
		t.Fatal("last_audit_at is zero after a full run, want the run time")
	}

	// A racing later-claim delta must not erase the full run's clock.
	deltaAt := fullAt.Add(time.Minute)
	seedAuditClockWorkItem(t, ctx, sqlDB, workItemID, scopeID, generationID, deltaAt, "claimed")
	if err := store.RecordAWSDriftReopenFingerprint(ctx, workItemID, deltaAt, snapshot, false); err != nil {
		t.Fatalf("record delta: %v", err)
	}
	_, gotDelta, ok, err := store.LoadAWSDriftRecordedSnapshot(ctx, workItemID)
	if err != nil || !ok {
		t.Fatalf("load after delta: ok=%v err=%v", ok, err)
	}
	if !gotDelta.Equal(gotFull) {
		t.Fatalf("last_audit_at moved on delta: %v -> %v, want preserved", gotFull, gotDelta)
	}

	// A later full run advances the clock again.
	fullAt2 := deltaAt.Add(time.Minute)
	seedAuditClockWorkItem(t, ctx, sqlDB, workItemID, scopeID, generationID, fullAt2, "claimed")
	if err := store.RecordAWSDriftReopenFingerprint(ctx, workItemID, fullAt2, snapshot, true); err != nil {
		t.Fatalf("record second full: %v", err)
	}
	_, gotFull2, ok, err := store.LoadAWSDriftRecordedSnapshot(ctx, workItemID)
	if err != nil || !ok {
		t.Fatalf("load after second full: ok=%v err=%v", ok, err)
	}
	if !gotFull2.After(gotFull) {
		t.Fatalf("last_audit_at did not advance on full: %v -> %v", gotFull, gotFull2)
	}
}

// TestAWSDriftReopenCandidatesAuditStaleLive pins the D3 listing leg: a
// fingerprint-equal row re-opens when its audit clock is NULL or older than
// the interval, and stays skipped when the audit is fresh.
func TestAWSDriftReopenCandidatesAuditStaleLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftIsolatedLiveDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	suffix := fmt.Sprintf("stale-%d", time.Now().UnixNano())
	scopeID := "aws:" + suffix
	generationID := "gen-" + suffix
	workItemID := "work-" + suffix
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, now)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", now)
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload) VALUES ($1,$2,$3,'aws_resource','k','aws','k','uri',$4,$4,FALSE,'{"arn":"arn:1"}'::jsonb)`,
		"f-"+suffix, scopeID, generationID, now); err != nil {
		t.Fatalf("seed aws fact: %v", err)
	}
	seedAuditClockWorkItem(t, ctx, sqlDB, workItemID, scopeID, generationID, now, "succeeded")

	var fingerprint string
	if err := sqlDB.QueryRowContext(ctx,
		"SELECT ("+backfill.AWSDriftARNFingerprintSQL("$1", "$2")+")",
		scopeID, generationID).Scan(&fingerprint); err != nil {
		t.Fatalf("current fingerprint: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO aws_drift_reopen_fingerprints (work_item_id, scope_id, generation_id, arn_ids, arn_payload_hashes, arn_fingerprint, logic_version, claimed_at, recorded_at, last_audit_at) VALUES ($1,$2,$3,ARRAY['arn:1'],ARRAY['h1'],$4,$5,$6,$6,NULL)`,
		workItemID, scopeID, generationID, fingerprint, reducercontract.AWSRuntimeDriftLogicVersion, now); err != nil {
		t.Fatalf("seed fingerprint row: %v", err)
	}

	list := func() (reopen []string, skipped int) {
		t.Helper()
		rows, err := sqlDB.QueryContext(ctx, backfill.ListAWSDriftReopenCandidatesQuery, "aws_cloud_runtime_drift")
		if err != nil {
			t.Fatalf("listing: %v", err)
		}
		reopen, skipped, err = backfill.ScanAWSDriftReopenCandidates(rows)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		return reopen, skipped
	}

	// NULL clock (never audited): fingerprint matches but the row re-opens.
	if reopen, _ := list(); len(reopen) != 1 || reopen[0] != workItemID {
		t.Fatalf("NULL-clock listing reopen = %v, want [%s]", reopen, workItemID)
	}

	// Fresh audit: fingerprint matches and the clock is fresh → skipped.
	if _, err := sqlDB.ExecContext(ctx, `UPDATE aws_drift_reopen_fingerprints SET last_audit_at = clock_timestamp() WHERE work_item_id = $1`, workItemID); err != nil {
		t.Fatalf("fresh clock: %v", err)
	}
	if reopen, skipped := list(); len(reopen) != 0 || skipped != 1 {
		t.Fatalf("fresh-clock listing reopen = %v skipped = %d, want [] and 1", reopen, skipped)
	}

	// Stale audit: fingerprint matches but the clock is older than the
	// interval → re-opens.
	if _, err := sqlDB.ExecContext(ctx, `UPDATE aws_drift_reopen_fingerprints SET last_audit_at = clock_timestamp() - INTERVAL '25 hours' WHERE work_item_id = $1`, workItemID); err != nil {
		t.Fatalf("stale clock: %v", err)
	}
	if reopen, _ := list(); len(reopen) != 1 || reopen[0] != workItemID {
		t.Fatalf("stale-clock listing reopen = %v, want [%s]", reopen, workItemID)
	}
}
