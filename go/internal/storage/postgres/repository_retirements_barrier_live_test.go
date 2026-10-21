// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
	"time"
)

// TestRetirementBarrierWaitsPreBarrierLeaseLive is the #8659 RED test for
// step 2b': the graph retraction must wait out partition leases claimed
// before the intent-delete barrier.
//
// A lease claimed before intent_barrier_at blocks the retraction while it
// is live; a lease claimed after the barrier, an expired lease, and a
// released lease do not. The readback names the oldest blocking lease so
// an operator sees which partition to look at.
func TestRetirementBarrierWaitsPreBarrierLeaseLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_barrier",
		"set ESHU_POSTGRES_DSN to run the retirement barrier Postgres proof")

	store := NewRetirementStore(SQLDB{DB: database})
	marker, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_barrier_live", RepoID: "PARTITION:barrier-live",
		ScopeID:    "barrier-live-main",
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "barrier-live-key",
	})
	if err != nil {
		t.Fatalf("InsertPending: %v", err)
	}

	intents := NewSharedIntentStore(SQLDB{DB: database})
	// A pre-barrier lease: claimed before the barrier stamp.
	if _, err := database.ExecContext(ctx, `INSERT INTO shared_projection_intents
		(intent_id, projection_domain, partition_key, scope_id, repository_id,
		 source_run_id, generation_id, payload, created_at)
		VALUES ('barrier-live-intent', 'code_calls', 'PARTITION:barrier-live',
		 'barrier-live-main', 'PARTITION:barrier-live', 'run-1', 'gen-1',
		 '{}'::jsonb, NOW())`); err != nil {
		t.Fatalf("seed intent: %v", err)
	}
	claimed, err := intents.ClaimPartitionLease(ctx, "code_calls", 0, 4, "barrier-live-worker", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim pre-barrier lease: claimed=%v err=%v", claimed, err)
	}

	barrier := NewRetirementBarrierStore(SQLDB{DB: database})
	// The 2b intent delete runs before the barrier stamp.
	if _, err := database.ExecContext(ctx, `DELETE FROM shared_projection_intents
		WHERE repository_id = 'PARTITION:barrier-live'`); err != nil {
		t.Fatalf("2b intent delete: %v", err)
	}
	stamped, err := barrier.StampIntentBarrier(ctx, marker.RetirementID)
	if err != nil {
		t.Fatalf("StampIntentBarrier: %v", err)
	}
	if stamped.IsZero() {
		t.Fatal("StampIntentBarrier returned a zero barrier")
	}

	// A post-barrier lease on another partition must not block.
	if _, err := database.ExecContext(ctx, `INSERT INTO shared_projection_intents
		(intent_id, projection_domain, partition_key, scope_id, repository_id,
		 source_run_id, generation_id, payload, created_at)
		VALUES ('barrier-live-intent-2', 'code_calls', 'PARTITION:barrier-live',
		 'barrier-live-main', 'PARTITION:barrier-live', 'run-2', 'gen-2',
		 '{}'::jsonb, NOW())`); err != nil {
		t.Fatalf("seed second intent: %v", err)
	}
	claimed, err = intents.ClaimPartitionLease(ctx, "code_calls", 1, 4, "barrier-live-worker-2", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim post-barrier lease: claimed=%v err=%v", claimed, err)
	}

	blocking, oldest, err := barrier.CountPreBarrierLeases(ctx, []string{"code_calls"}, stamped)
	if err != nil {
		t.Fatalf("CountPreBarrierLeases: %v", err)
	}
	if blocking != 1 {
		t.Fatalf("blocking leases = %d, want 1 (only the pre-barrier lease)", blocking)
	}
	if oldest.Domain != "code_calls" || oldest.PartitionID != 0 || oldest.LeaseOwner != "barrier-live-worker" {
		t.Fatalf("oldest blocking lease = %+v, want the pre-barrier code_calls/0 lease", oldest)
	}

	// Releasing the pre-barrier lease unblocks the retraction.
	if err := intents.ReleasePartitionLease(ctx, "code_calls", 0, 4, "barrier-live-worker"); err != nil {
		t.Fatalf("release pre-barrier lease: %v", err)
	}
	blocking, _, err = barrier.CountPreBarrierLeases(ctx, []string{"code_calls"}, stamped)
	if err != nil {
		t.Fatalf("CountPreBarrierLeases after release: %v", err)
	}
	if blocking != 0 {
		t.Fatalf("blocking leases after release = %d, want 0", blocking)
	}
}

// TestRetirementRecheckPredicatesMatchFence pins the 2q recheck to the
// fence's status predicates: the fence quiesces a scope, the recheck
// re-asserts it minutes later, and the two must agree on what counts as
// live. A status-list change on either side fails here so the other side
// is updated deliberately, never by drift.
func TestRetirementRecheckPredicatesMatchFence(t *testing.T) {
	t.Parallel()

	pairs := []struct {
		name    string
		recheck string
		fence   string
	}{
		{"generations", retirementRecheckGenerationsSQL, fencePrecheckGenerationsQuery},
		{"reducer rows", retirementRecheckReducerRowsSQL, fencePrecheckReducerRowsQuery},
		{"live reducer leases", retirementRecheckLiveReducerLeasesSQL, fenceLiveReducerLeasesQuery},
		{"live projector leases", retirementRecheckLiveProjectorLeasesSQL, fenceLiveProjectorLeasesQuery},
	}
	fragments := []string{
		"status IN ('pending', 'active', 'failed')",
		"status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"status IN ('claimed', 'running') AND claim_until <= clock_timestamp()",
		"status IN ('claimed', 'running') AND claim_until > clock_timestamp()",
	}
	normalize := func(s string) string {
		joined := strings.Join(strings.Fields(s), " ")
		// The fence wraps the claim-expiry conjunct in parentheses the
		// recheck omits, and qualifies columns with table aliases the
		// recheck omits; the pin compares status predicates, neither
		// grouping nor qualification.
		stripped := strings.ReplaceAll(strings.ReplaceAll(joined, "(", ""), ")", "")
		for _, qualifier := range []string{"w.", "g.", "s."} {
			stripped = strings.ReplaceAll(stripped, qualifier, "")
		}
		return stripped
	}
	for _, pair := range pairs {
		recheck, fence := normalize(pair.recheck), normalize(pair.fence)
		matched := false
		for _, raw := range fragments {
			fragment := normalize(raw)
			inRecheck := strings.Contains(recheck, fragment)
			inFence := strings.Contains(fence, fragment)
			if inRecheck && inFence {
				matched = true
			}
			if inRecheck != inFence {
				t.Errorf("%s: fragment %q in recheck=%v but in fence=%v; update both sides deliberately", pair.name, fragment, inRecheck, inFence)
			}
		}
		if !matched {
			t.Errorf("%s: recheck and fence share no status fragment; the recheck no longer measures what the fence quiesced", pair.name)
		}
	}
}

// TestRetirementReassertScopesQuiescedLive proves the 2q recheck: it sees
// a live generation, an actionable reducer row, and live stage leases over
// the tombstone's scopes, and reports quiet only when every one is gone.
// The reopened-row half then simulates a maintenance pass recreating a
// reducer row after quiescence and asserts the recheck catches it: that
// detection is what fails the retirement closed instead of retracting
// under a live writer.
func TestRetirementReassertScopesQuiescedLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_reassert",
		"set ESHU_POSTGRES_DSN to run the retirement recheck Postgres proof")
	sqlDB := SQLDB{DB: database}

	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		 observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ('reassert-main', 'git', 'git', 'reassert-key', 'git', 'PARTITION:reassert',
		 NOW(), NOW(), 'active', 'reassert-gen-1', '{}'::jsonb)`); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		VALUES ('reassert-gen-1', 'reassert-main', 'manual', NOW(), NOW(), 'active')`); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items
		(work_item_id, scope_id, generation_id, stage, domain, status,
		 lease_owner, claim_until, payload, created_at, updated_at)
		VALUES ('reassert-reducer-1', 'reassert-main', 'reassert-gen-1', 'reducer',
			'semantic_entity_materialization', 'pending', NULL, NULL, '{}'::jsonb, NOW(), NOW()),
		       ('reassert-projector-1', 'reassert-main', 'reassert-gen-1', 'projector',
			'code_calls', 'claimed', 'reassert-worker', NOW() + INTERVAL '1 minute',
			'{}'::jsonb, NOW(), NOW())`); err != nil {
		t.Fatalf("seed work items: %v", err)
	}
	counts, err := ReassertScopesQuiesced(ctx, sqlDB, []string{"reassert-main"})
	if err != nil {
		t.Fatalf("ReassertScopesQuiesced: %v", err)
	}
	if counts.LiveGenerations != 1 || counts.ActionableReducerRows != 1 ||
		counts.LiveReducerLeases != 0 || counts.LiveProjectorLeases != 1 {
		t.Fatalf("counts = %+v, want generations=1 rows=1 reducer_leases=0 projector_leases=1", counts)
	}
	if counts.Quiet() {
		t.Fatal("Quiet with live rows present")
	}
	// Quiesce everything the fence would have quiesced.
	if _, err := database.ExecContext(ctx, `UPDATE scope_generations SET status = 'superseded'
		WHERE generation_id = 'reassert-gen-1'`); err != nil {
		t.Fatalf("supersede generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE fact_work_items SET status = 'completed', claim_until = NULL
		WHERE scope_id = 'reassert-main'`); err != nil {
		t.Fatalf("complete work items: %v", err)
	}
	counts, err = ReassertScopesQuiesced(ctx, sqlDB, []string{"reassert-main"})
	if err != nil {
		t.Fatalf("ReassertScopesQuiesced quiet: %v", err)
	}
	if !counts.Quiet() {
		t.Fatalf("counts = %+v, want quiet", counts)
	}
	// A maintenance pass reopens one reducer row after quiescence: the
	// recheck must catch it, not wave the retraction through.
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items
		(work_item_id, scope_id, generation_id, stage, domain, status,
		 lease_owner, claim_until, payload, created_at, updated_at)
		VALUES ('reassert-reducer-2', 'reassert-main', 'reassert-gen-1', 'reducer',
			'semantic_entity_materialization', 'retrying', NULL, NULL, '{}'::jsonb, NOW(), NOW())`); err != nil {
		t.Fatalf("reopen reducer row: %v", err)
	}
	counts, err = ReassertScopesQuiesced(ctx, sqlDB, []string{"reassert-main"})
	if err != nil {
		t.Fatalf("ReassertScopesQuiesced reopened: %v", err)
	}
	if counts.ActionableReducerRows != 1 || counts.Quiet() {
		t.Fatalf("counts = %+v, want the reopened row detected", counts)
	}
}

// TestRetirementBarrierHeartbeatKeepsEpochLive pins the claimed_at arm of
// the lease claim: a heartbeat (same owner re-claim) keeps the original
// claim epoch, so a pre-barrier lease still holds 2b' after heartbeats,
// while a new owner taking the lease starts a fresh post-barrier epoch
// and does not hold it.
func TestRetirementBarrierHeartbeatKeepsEpochLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_barrier_hb",
		"set ESHU_POSTGRES_DSN to run the retirement barrier heartbeat Postgres proof")

	store := NewRetirementStore(SQLDB{DB: database})
	marker, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_barrier_hb", RepoID: "PARTITION:barrier-hb",
		ScopeID:    "barrier-hb-main",
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "barrier-hb-key",
	})
	if err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	intents := NewSharedIntentStore(SQLDB{DB: database})
	claimed, err := intents.ClaimPartitionLease(ctx, "code_calls", 0, 4, "barrier-hb-worker", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim lease: claimed=%v err=%v", claimed, err)
	}
	barrier := NewRetirementBarrierStore(SQLDB{DB: database})
	stamped, err := barrier.StampIntentBarrier(ctx, marker.RetirementID)
	if err != nil {
		t.Fatalf("StampIntentBarrier: %v", err)
	}
	// Heartbeat after the barrier: the same owner keeps its epoch, so the
	// lease still holds 2b'. A reset-on-heartbeat bug would clear this to
	// zero here.
	claimed, err = intents.ClaimPartitionLease(ctx, "code_calls", 0, 4, "barrier-hb-worker", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("heartbeat: claimed=%v err=%v", claimed, err)
	}
	blocking, _, err := barrier.CountPreBarrierLeases(ctx, []string{"code_calls"}, stamped)
	if err != nil {
		t.Fatalf("CountPreBarrierLeases after heartbeat: %v", err)
	}
	if blocking != 1 {
		t.Fatalf("blocking leases after heartbeat = %d, want 1 (epoch kept)", blocking)
	}
	// Takeover: releasing and claiming as a new owner starts a fresh
	// post-barrier epoch, which must not hold 2b'.
	if err := intents.ReleasePartitionLease(ctx, "code_calls", 0, 4, "barrier-hb-worker"); err != nil {
		t.Fatalf("release: %v", err)
	}
	claimed, err = intents.ClaimPartitionLease(ctx, "code_calls", 0, 4, "barrier-hb-worker-2", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("takeover claim: claimed=%v err=%v", claimed, err)
	}
	blocking, _, err = barrier.CountPreBarrierLeases(ctx, []string{"code_calls"}, stamped)
	if err != nil {
		t.Fatalf("CountPreBarrierLeases after takeover: %v", err)
	}
	if blocking != 0 {
		t.Fatalf("blocking leases after takeover = %d, want 0 (fresh epoch)", blocking)
	}
	// Turnover: the same worker claiming again after its own release also
	// starts fresh (the release nulled the owner). Without this, a stable
	// worker would hold a pre-barrier epoch across cycles and 2b' would
	// never terminate on a healthy fleet.
	if err := intents.ReleasePartitionLease(ctx, "code_calls", 0, 4, "barrier-hb-worker-2"); err != nil {
		t.Fatalf("release takeover: %v", err)
	}
	claimed, err = intents.ClaimPartitionLease(ctx, "code_calls", 0, 4, "barrier-hb-worker-2", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("reclaim after release: claimed=%v err=%v", claimed, err)
	}
	blocking, _, err = barrier.CountPreBarrierLeases(ctx, []string{"code_calls"}, stamped)
	if err != nil {
		t.Fatalf("CountPreBarrierLeases after reclaim: %v", err)
	}
	if blocking != 0 {
		t.Fatalf("blocking leases after same-owner reclaim post-release = %d, want 0 (turnover)", blocking)
	}
}

// TestRetirementBarrierIgnoresExpiredLeaseLive proves an expired
// pre-barrier lease is outside the lease contract and does not hold 2b'.
func TestRetirementBarrierIgnoresExpiredLeaseLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_barrier_exp",
		"set ESHU_POSTGRES_DSN to run the retirement barrier expiry Postgres proof")

	store := NewRetirementStore(SQLDB{DB: database})
	marker, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_barrier_exp", RepoID: "PARTITION:barrier-exp",
		ScopeID:    "barrier-exp-main",
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "barrier-exp-key",
	})
	if err != nil {
		t.Fatalf("InsertPending: %v", err)
	}

	intents := NewSharedIntentStore(SQLDB{DB: database})
	// A negative TTL expires on commit: the row exists but holds no live
	// lease, so it must not block.
	claimed, err := intents.ClaimPartitionLease(ctx, "code_calls", 0, 4, "barrier-exp-worker", -time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim expired lease: claimed=%v err=%v", claimed, err)
	}

	barrier := NewRetirementBarrierStore(SQLDB{DB: database})
	stamped, err := barrier.StampIntentBarrier(ctx, marker.RetirementID)
	if err != nil {
		t.Fatalf("StampIntentBarrier: %v", err)
	}
	blocking, _, err := barrier.CountPreBarrierLeases(ctx, []string{"code_calls"}, stamped)
	if err != nil {
		t.Fatalf("CountPreBarrierLeases: %v", err)
	}
	if blocking != 0 {
		t.Fatalf("blocking leases with an expired lease = %d, want 0", blocking)
	}
}
