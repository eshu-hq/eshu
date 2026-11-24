// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10972 drain-gated priority tier: a live generation a gate waits on must win a claim slot ahead of older ordinary backlog (capped at one per batch, FIFO among gating generations), and non-gating relative order plus auto-expiry on drain must hold; disposable PostgreSQL through postgresproof with the production claim path
//eshu:readiness:env ESHU_REDUCER_GATE_PRIORITY_PROOF_DISPOSABLE ESHU_REDUCER_GATE_PRIORITY_PROOF_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestClaimBatchGatePriorityBoostsDrainingGenerationLive is the failing-first
// proof for #10972: a reducer row whose generation a drain gate waits on
// (here: an NVD baseline year whose last committed chunk has not drained)
// takes a claim slot ahead of older ordinary backlog. The tier is bounded:
// reclaim rows still lead, and the boost applies to the gating set only.
func TestClaimBatchGatePriorityBoostsDrainingGenerationLive(t *testing.T) {
	t.Parallel()

	db := openGatePriorityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	now := time.Date(2026, time.November, 19, 22, 0, 0, 0, time.UTC)
	const (
		scopeOrdOld   = "vuln-intel://test-gate-priority/ordinary-old"
		scopeOrdMid   = "vuln-intel://test-gate-priority/ordinary-mid"
		scopeOrdYoung = "vuln-intel://test-gate-priority/ordinary-young"
		scopeReclaim  = "vuln-intel://test-gate-priority/reclaim"
		scopeGate     = "vuln-intel://nvd/_baseline/test-gate-priority/2026/1767225600000000"
		genGate       = "vulnerability_intelligence:test-gate-priority:ed9d4d573"
		runID         = "test-gate-priority"
	)
	// Ordinary backlog, strictly older than the gating row: without the
	// tier, updated_at ASC puts every one of these ahead of it.
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_priority_ord_old", scopeOrdOld, "gen:gate-priority-ord-old", now.Add(-2*time.Hour), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_priority_ord_mid", scopeOrdMid, "gen:gate-priority-ord-mid", now.Add(-time.Hour), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_priority_ord_young", scopeOrdYoung, "gen:gate-priority-ord-young", now.Add(-30*time.Minute), "pending", nil)
	// The drain gate waits on this generation: incomplete year, last
	// committed chunk still open (live pending row below).
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2026, false, scopeGate, genGate, now.Add(-time.Hour))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_priority_gating", scopeGate, genGate, now, "pending", nil)
	// An expired claimed hold keeps its lead: the tier sits behind reclaim.
	expired := now.Add(-time.Hour)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_priority_reclaim", scopeReclaim, "gen:gate-priority-reclaim", now.Add(-3*time.Hour), "claimed", &expired)

	intents, err := gatePriorityClaimer(db, now).ClaimBatch(ctx, 2)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	got := make(map[string]bool, len(intents))
	for _, in := range intents {
		got[in.GenerationID] = true
	}
	if len(got) != 2 || !got["gen:gate-priority-reclaim"] || !got[genGate] {
		t.Fatalf("batch of 2 must claim the reclaim row and the gating generation, got generations %v", keysOf(got))
	}
}

// TestClaimBatchGatePriorityCapsAtOneBoostedRowLive pins the bound: with two
// gate-waited generations only the longest-waiting one takes the boosted
// slot per batch (FIFO among gating generations); the other keeps its
// ordinary place in line.
func TestClaimBatchGatePriorityCapsAtOneBoostedRowLive(t *testing.T) {
	t.Parallel()

	db := openGatePriorityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	now := time.Date(2026, time.November, 19, 22, 0, 0, 0, time.UTC)
	const (
		scopeOrdA    = "vuln-intel://test-gate-cap/ordinary-a"
		scopeOrdB    = "vuln-intel://test-gate-cap/ordinary-b"
		scopeReclaim = "vuln-intel://test-gate-cap/reclaim"
		scopeOld     = "vuln-intel://nvd/_baseline/test-gate-cap/2025/1735689600000000"
		scopeNew     = "vuln-intel://nvd/_baseline/test-gate-cap/2026/1767225600000000"
		genOld       = "vulnerability_intelligence:test-gate-cap:old2025"
		genNew       = "vulnerability_intelligence:test-gate-cap:new2026"
		runID        = "test-gate-cap"
	)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_cap_ord_a", scopeOrdA, "gen:gate-cap-ord-a", now.Add(-2*time.Hour), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_cap_ord_b", scopeOrdB, "gen:gate-cap-ord-b", now.Add(-time.Hour), "pending", nil)
	expired := now.Add(-time.Hour)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_cap_reclaim", scopeReclaim, "gen:gate-cap-reclaim", now.Add(-3*time.Hour), "claimed", &expired)
	// Two waited-on generations; the 2025 lane has waited longer.
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2025, false, scopeOld, genOld, now.Add(-time.Hour))
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2026, false, scopeNew, genNew, now.Add(-10*time.Minute))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_cap_gating_old", scopeOld, genOld, now.Add(-5*time.Minute), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_cap_gating_new", scopeNew, genNew, now, "pending", nil)

	intents, err := gatePriorityClaimer(db, now).ClaimBatch(ctx, 3)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	got := make(map[string]bool, len(intents))
	for _, in := range intents {
		got[in.GenerationID] = true
	}
	if len(got) != 3 || !got["gen:gate-cap-reclaim"] || !got[genOld] {
		t.Fatalf("batch of 3 must claim reclaim, the longest-waiting gating generation, and one ordinary row, got %v", keysOf(got))
	}
	if got[genNew] {
		t.Fatalf("cap is one boosted row per batch: the newer gating generation must wait its turn, got %v", keysOf(got))
	}
}

// TestClaimBatchGatePriorityExpiresOnDrainLive is a no-regression guard,
// not a failing-first test: a completed year must not boost, and ordinary
// age order decides that with or without the tier. The failing-first proof
// lives in the boost, cap, and superseded tests.
func TestClaimBatchGatePriorityExpiresOnDrainLive(t *testing.T) {
	t.Parallel()

	db := openGatePriorityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	now := time.Date(2026, time.November, 19, 22, 0, 0, 0, time.UTC)
	const (
		scopeOrd  = "vuln-intel://test-gate-expire/ordinary"
		scopeRecl = "vuln-intel://test-gate-expire/reclaim"
		scopeGate = "vuln-intel://nvd/_baseline/test-gate-expire/2026/1767225600000000"
		genGate   = "vulnerability_intelligence:test-gate-expire:ed9d4d573"
		runID     = "test-gate-expire"
	)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_expire_ord", scopeOrd, "gen:gate-expire-ord", now.Add(-2*time.Hour), "pending", nil)
	expired := now.Add(-time.Hour)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_expire_reclaim", scopeRecl, "gen:gate-expire-reclaim", now.Add(-3*time.Hour), "claimed", &expired)
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2026, true, scopeGate, genGate, now.Add(-time.Hour))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_expire_gating", scopeGate, genGate, now, "pending", nil)

	intents, err := gatePriorityClaimer(db, now).ClaimBatch(ctx, 2)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	got := make(map[string]bool, len(intents))
	for _, in := range intents {
		got[in.GenerationID] = true
	}
	if len(got) != 2 || !got["gen:gate-expire-reclaim"] || !got["gen:gate-expire-ord"] {
		t.Fatalf("completed year must not boost: batch of 2 must claim reclaim and the older ordinary row, got %v", keysOf(got))
	}
}

func openGatePriorityProofDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(t,
		strings.TrimSpace(os.Getenv("ESHU_REDUCER_GATE_PRIORITY_PROOF_DSN")),
		os.Getenv("ESHU_REDUCER_GATE_PRIORITY_PROOF_DISPOSABLE"),
		90*time.Second)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply gate priority schema: %v", err)
	}
	return db
}

func gatePriorityClaimer(db *sql.DB, now time.Time) ReducerQueue {
	return ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "gate-priority-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
}

func seedGatePriorityScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status
) VALUES (
    $1, 'vulnerability_intelligence', 'vulnerability_intelligence', $1, 'reducer', $1,
    clock_timestamp(), clock_timestamp(), 'active'
) ON CONFLICT (scope_id) DO NOTHING`, scopeID); err != nil {
		t.Fatalf("seed gate priority scope: %v", err)
	}
}

func seedGatePriorityWorkItem(t *testing.T, ctx context.Context, db *sql.DB, workItemID, scopeID, generationID string, updatedAt time.Time, status string, claimUntil *time.Time) {
	t.Helper()
	seedGatePriorityScope(t, ctx, db, scopeID)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES (
    $2, $1, 'synthetic', FALSE,
    clock_timestamp(), clock_timestamp(), 'active'
) ON CONFLICT DO NOTHING`, scopeID, generationID); err != nil {
		t.Fatalf("seed gate priority generation: %v", err)
	}
	var claimUntilVal any
	if claimUntil != nil {
		claimUntilVal = *claimUntil
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain,
    conflict_domain, conflict_key, status, attempt_count, payload,
    created_at, updated_at, visible_at, claim_until
) VALUES (
    $1, $2, $3, 'reducer', 'supply_chain_impact',
    'scope', $2, $4, 0, '{}',
    $5, $5, $5, $6
)`, workItemID, scopeID, generationID, status, updatedAt, claimUntilVal); err != nil {
		t.Fatalf("seed gate priority work item: %v", err)
	}
}

func seedGatePriorityCheckpoint(t *testing.T, ctx context.Context, db *sql.DB, runID string, year int, complete bool, scopeID, generationID string, updatedAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO vulnerability_nvd_baseline_checkpoints (
    baseline_run_id, year, resume_unix_micro, year_sha256, year_complete,
    updated_at, resume_cve_id, year_generated_unix_micro,
    last_scope_id, last_generation_id, records_done, records_total
) VALUES ($1, $2, 1, 'test', $3, $4, '', 0, $5, $6, 0, 0)`,
		runID, year, complete, updatedAt, scopeID, generationID); err != nil {
		t.Fatalf("seed gate priority checkpoint: %v", err)
	}
}

// TestClaimBatchGatePriorityIgnoresSupersededGenerationLive pins the
// de-boost path: when the checkpoint advances to a new last committed
// chunk, live rows of the superseded generation keep ordinary order. Only
// the checkpoint's current last_generation_id joins the gating set, so a
// year whose checkpoint moved on drops its old chunk even while that chunk
// still has live rows.
func TestClaimBatchGatePriorityIgnoresSupersededGenerationLive(t *testing.T) {
	t.Parallel()

	db := openGatePriorityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	now := time.Date(2026, time.November, 19, 22, 0, 0, 0, time.UTC)
	const (
		scopeOrd  = "vuln-intel://test-gate-superseded/ordinary"
		scopeOld  = "vuln-intel://test-gate-superseded/old-chunk"
		scopeNew  = "vuln-intel://test-gate-superseded/new-chunk"
		genOld    = "vulnerability_intelligence:test-gate-superseded:old"
		genNew    = "vulnerability_intelligence:test-gate-superseded:new"
		runID     = "test-gate-superseded"
		scopeRecl = "vuln-intel://test-gate-superseded/reclaim"
	)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_sup_ord", scopeOrd, "gen:gate-sup-ord", now.Add(-3*time.Hour), "pending", nil)
	expired := now.Add(-time.Hour)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_sup_reclaim", scopeRecl, "gen:gate-sup-reclaim", now.Add(-4*time.Hour), "claimed", &expired)
	// The checkpoint moved on: only the new chunk's generation is waited on.
	// The old chunk still has a live row (e.g. a retry leftover).
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2026, false, scopeNew, genNew, now.Add(-time.Hour))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_sup_old", scopeOld, genOld, now.Add(-time.Hour), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_sup_new", scopeNew, genNew, now, "pending", nil)

	intents, err := gatePriorityClaimer(db, now).ClaimBatch(ctx, 2)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	got := make(map[string]bool, len(intents))
	for _, in := range intents {
		got[in.GenerationID] = true
	}
	if len(got) != 2 || !got["gen:gate-sup-reclaim"] || !got[genNew] {
		t.Fatalf("batch of 2 must claim reclaim and the current waited-on generation, got %v", keysOf(got))
	}
	if got[genOld] {
		t.Fatalf("superseded generation must keep ordinary order and miss this batch, got %v", keysOf(got))
	}
}

func keysOf(got map[string]bool) []string {
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	return keys
}

// TestClaimBatchGatePriorityBoostsOlderBaselineWindowLive is the failing-first
// proof for eshu-hq/eshu#11366: the gate boost must cover every undrained
// window of an incomplete baseline year, not just the checkpoint's last
// committed generation. An older window whose reducer never ran (its
// checkpoint moved on when a newer window committed) sorts boosted ahead of
// older ordinary backlog; the newer window keeps its ordinary place, so the
// one-boosted-slot bound still holds. The superseded-generation de-boost
// (TestClaimBatchGatePriorityIgnoresSupersededGenerationLive) is untouched:
// it uses non-baseline scopes, which never join the gating set.
func TestClaimBatchGatePriorityBoostsOlderBaselineWindowLive(t *testing.T) {
	t.Parallel()

	db := openGatePriorityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	now := time.Date(2026, time.November, 19, 22, 0, 0, 0, time.UTC)
	const (
		runID     = "test-gate-older-window"
		scopeOrdO = "vuln-intel://test-gate-older-window/ordinary-old"
		scopeOrdM = "vuln-intel://test-gate-older-window/ordinary-mid"
		scopeOld  = "vuln-intel://nvd/_baseline/test-gate-older-window/2026/1767225600000000"
		genOld    = "vulnerability_intelligence:test-gate-older-window:old"
		scopeNew  = "vuln-intel://nvd/_baseline/test-gate-older-window/2026/1781691075297000"
		genNew    = "vulnerability_intelligence:test-gate-older-window:new"
		genOrdOld = "gen:gate-older-window-ord-old"
		genOrdMid = "gen:gate-older-window-ord-mid"
	)
	// Ordinary backlog strictly older than the orphaned window: without the
	// fix, updated_at ASC puts both of these ahead of it.
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_older_ord_old", scopeOrdO, genOrdOld, now.Add(-4*time.Hour), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_older_ord_mid", scopeOrdM, genOrdMid, now.Add(-3*time.Hour), "pending", nil)
	// The checkpoint moved on to the newer window, but the older window's
	// reducer never ran.
	seedGatePriorityCheckpoint(t, ctx, db, runID, 2026, false, scopeNew, genNew, now.Add(-30*time.Minute))
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_older_old", scopeOld, genOld, now.Add(-2*time.Hour), "pending", nil)
	seedGatePriorityWorkItem(t, ctx, db, "reducer_gate_older_new", scopeNew, genNew, now.Add(-time.Hour), "pending", nil)

	intents, err := gatePriorityClaimer(db, now).ClaimBatch(ctx, 2)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	got := make(map[string]bool, len(intents))
	for _, in := range intents {
		got[in.GenerationID] = true
	}
	if len(got) != 2 || !got[genOld] || !got[genOrdOld] {
		t.Fatalf("batch of 2 must claim the orphaned older window and the oldest ordinary row, got generations %v", keysOf(got))
	}
	if got[genNew] {
		t.Fatalf("newer window must keep its ordinary place (one boosted slot per batch), got %v", keysOf(got))
	}
}
