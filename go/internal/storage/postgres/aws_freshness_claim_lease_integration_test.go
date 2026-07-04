// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
)

// ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN gates this suite against a real
// Postgres instance. It is skipped otherwise so the normal unit gate is
// unaffected, mirroring the sibling generation-liveness/reducer-queue
// integration proofs in this package.
const freshnessClaimLeaseProofDSNEnv = "ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN"

// TestAWSFreshnessStoreReapExpiredTriggerClaimsIntegration proves #4576's
// stuck-claim reclaim against a real Postgres instance, not just the SQL
// text: a trigger claimed with an already-expired lease is reclaimed back to
// 'queued' (recovering it from a mid-batch handoff abort or coordinator
// crash), while a trigger claimed with a lease that has NOT yet expired is
// left untouched — the reap must never race a still-live claim-holder within
// its lease window.
func TestAWSFreshnessStoreReapExpiredTriggerClaimsIntegration(t *testing.T) {
	dsn := os.Getenv(freshnessClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the AWS/GCP freshness claim-lease integration proof")
	}

	db := freshnessLeaseProofDB(t, dsn)
	store := NewAWSFreshnessStore(SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.July, 4, 12, 0, 0, 0, time.UTC)

	expiredTrigger := freshness.Trigger{
		EventID:      "evt-expired",
		Kind:         freshness.EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   "function-expired",
		ObservedAt:   now,
	}
	// liveTrigger uses a different region so its FreshnessKey
	// (account:region:service_kind) is distinct from expiredTrigger's —
	// StoreTrigger coalesces same-FreshnessKey triggers into one row, which
	// would otherwise collapse these two triggers into a single claimable row.
	liveTrigger := freshness.Trigger{
		EventID:      "evt-live",
		Kind:         freshness.EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-west-2",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   "function-live",
		ObservedAt:   now,
	}

	// received_at is staggered so ClaimQueuedTriggers's
	// "ORDER BY received_at ASC" claims expiredTrigger deterministically
	// before liveTrigger when each is claimed with limit=1 below.
	ctx := context.Background()
	if _, err := store.StoreTrigger(ctx, expiredTrigger, now.Add(-time.Second)); err != nil {
		t.Fatalf("StoreTrigger(expired) error = %v", err)
	}
	if _, err := store.StoreTrigger(ctx, liveTrigger, now); err != nil {
		t.Fatalf("StoreTrigger(live) error = %v", err)
	}

	// Claim each trigger separately with a lease duration chosen so, relative
	// to the asOf time used by ReapExpiredTriggerClaims below, one claim's
	// lease has already expired and the other's has not — mirroring two
	// claimants that claimed at the same wall-clock moment but with
	// different lease budgets (or, equivalently, one trigger claimed well
	// before the other within the same reconcile tick). claimedAt is far
	// enough in the past that claimedAt+leaseDuration is still before asOf
	// (now) for the "expired" claim, and still after asOf for the "live" one.
	if _, err := store.ClaimQueuedTriggers(ctx, "claimant-expired", now.Add(-10*time.Minute), 1, time.Minute); err != nil {
		t.Fatalf("ClaimQueuedTriggers(expired-lease) error = %v", err)
	}
	if _, err := store.ClaimQueuedTriggers(ctx, "claimant-live", now.Add(-30*time.Second), 1, 5*time.Minute); err != nil {
		t.Fatalf("ClaimQueuedTriggers(live-lease) error = %v", err)
	}

	reclaimed, err := store.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v", err)
	}
	if got, want := len(reclaimed), 1; got != want {
		t.Fatalf("len(reclaimed) = %d, want %d (only the expired-lease trigger)", got, want)
	}
	if reclaimed[0].ResourceID != "function-expired" {
		t.Fatalf("reclaimed trigger = %+v, want the expired-lease trigger", reclaimed[0])
	}
	if reclaimed[0].Status != freshness.TriggerStatusQueued {
		t.Fatalf("reclaimed Status = %q, want %q", reclaimed[0].Status, freshness.TriggerStatusQueued)
	}

	// Idempotency: a second reap pass immediately after finds nothing left to
	// reclaim (the reclaimed row is back at 'queued', not 'claimed'; the
	// live-lease row is still within its lease window).
	secondPass, err := store.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("second ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(secondPass) != 0 {
		t.Fatalf("second reap pass reclaimed = %+v, want none (idempotent no-op)", secondPass)
	}
}

// TestAWSFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety proves the
// reap query's FOR UPDATE SKIP LOCKED concurrency guarantee against a real
// Postgres instance: two reap passes racing against the same batch of
// expired claims never both reclaim the same trigger row. Every claimed
// trigger is reclaimed exactly once across both concurrent callers, proving
// the reap cannot double-process (and, by construction, cannot deadlock
// against itself).
func TestAWSFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety(t *testing.T) {
	dsn := os.Getenv(freshnessClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the AWS/GCP freshness claim-lease integration proof")
	}

	db := freshnessLeaseProofDB(t, dsn)
	store := NewAWSFreshnessStore(SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.July, 4, 13, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// Each trigger uses a distinct region so its FreshnessKey
	// (account:region:service_kind) is unique — StoreTrigger coalesces
	// same-FreshnessKey triggers into one row, which would otherwise leave
	// far fewer than triggerCount distinct claimable rows.
	const triggerCount = 20
	for i := 0; i < triggerCount; i++ {
		trigger := freshness.Trigger{
			EventID:      fmt.Sprintf("evt-concurrent-%d", i),
			Kind:         freshness.EventKindConfigChange,
			AccountID:    "123456789012",
			Region:       fmt.Sprintf("us-region-%02d", i),
			ServiceKind:  awscloud.ServiceLambda,
			ResourceType: awscloud.ResourceTypeLambdaFunction,
			ResourceID:   fmt.Sprintf("function-concurrent-%d", i),
			ObservedAt:   now,
		}
		if _, err := store.StoreTrigger(ctx, trigger, now); err != nil {
			t.Fatalf("StoreTrigger(%d) error = %v", i, err)
		}
	}
	if _, err := store.ClaimQueuedTriggers(ctx, "claimant-concurrent", now.Add(-10*time.Minute), triggerCount, time.Minute); err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v", err)
	}

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		reclaimedBy = make(map[string]int) // trigger_id -> number of reap calls that returned it
	)
	const concurrentReapers = 5
	wg.Add(concurrentReapers)
	for i := 0; i < concurrentReapers; i++ {
		go func() {
			defer wg.Done()
			reclaimed, err := store.ReapExpiredTriggerClaims(context.Background(), now, triggerCount)
			if err != nil {
				t.Errorf("concurrent ReapExpiredTriggerClaims() error = %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, trigger := range reclaimed {
				reclaimedBy[trigger.TriggerID]++
			}
		}()
	}
	wg.Wait()

	if len(reclaimedBy) != triggerCount {
		t.Fatalf("distinct triggers reclaimed = %d, want %d (every claimed row must be reclaimed exactly once)", len(reclaimedBy), triggerCount)
	}
	for triggerID, count := range reclaimedBy {
		if count != 1 {
			t.Fatalf("trigger %q reclaimed by %d concurrent reap passes, want exactly 1 (FOR UPDATE SKIP LOCKED must prevent double-reclaim)", triggerID, count)
		}
	}
}

// TestAWSFreshnessStoreStaleHolderCannotCompleteReapedClaimIntegration proves
// the #4576 claim_fencing_token fencing raised in PR #4682 review against a
// real Postgres instance: a trigger claimed by one owner, reaped back to
// 'queued' after its lease expires, and then re-claimed by a different
// owner, cannot be completed (via MarkTriggersHandedOff or MarkTriggersFailed)
// by the original stale holder using the fencing token from its now-expired
// claim. Only the new owner's completion, presenting the current token,
// succeeds.
func TestAWSFreshnessStoreStaleHolderCannotCompleteReapedClaimIntegration(t *testing.T) {
	dsn := os.Getenv(freshnessClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the AWS/GCP freshness claim-lease integration proof")
	}

	db := freshnessLeaseProofDB(t, dsn)
	store := NewAWSFreshnessStore(SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.July, 4, 14, 0, 0, 0, time.UTC)
	ctx := context.Background()

	trigger := freshness.Trigger{
		EventID:      "evt-stale-holder",
		Kind:         freshness.EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   "function-stale-holder",
		ObservedAt:   now,
	}
	if _, err := store.StoreTrigger(ctx, trigger, now); err != nil {
		t.Fatalf("StoreTrigger() error = %v", err)
	}

	// Original holder claims with a lease that is already expired relative to
	// `now`, mirroring a coordinator replica that claimed, then stalled past
	// its lease (e.g. GC pause, slow handoff) while the reaper moved on.
	staleClaims, err := store.ClaimQueuedTriggers(ctx, "claimant-stale", now.Add(-10*time.Minute), 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers(stale) error = %v", err)
	}
	if len(staleClaims) != 1 {
		t.Fatalf("len(staleClaims) = %d, want 1", len(staleClaims))
	}
	staleClaim := staleClaims[0]

	// The reaper reclaims the expired claim back to 'queued'.
	reclaimed, err := store.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("len(reclaimed) = %d, want 1", len(reclaimed))
	}

	// A different owner re-claims the reclaimed trigger, bumping
	// claim_fencing_token again.
	newClaims, err := store.ClaimQueuedTriggers(ctx, "claimant-new", now, 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers(new) error = %v", err)
	}
	if len(newClaims) != 1 {
		t.Fatalf("len(newClaims) = %d, want 1", len(newClaims))
	}
	newClaim := newClaims[0]
	if newClaim.ClaimFencingToken == staleClaim.ClaimFencingToken {
		t.Fatalf("newClaim.ClaimFencingToken = %d, want different from staleClaim.ClaimFencingToken = %d", newClaim.ClaimFencingToken, staleClaim.ClaimFencingToken)
	}

	// The original stale holder attempts to complete the claim using the
	// fencing token from its now-superseded claim. This must not affect the
	// row: the row's current token belongs to the new owner.
	if err := store.MarkTriggersHandedOff(ctx, []freshness.StoredTrigger{staleClaim}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff(stale) error = %v", err)
	}
	assertAWSFreshnessTriggerStatus(t, db, staleClaim.TriggerID, string(freshness.TriggerStatusClaimed))

	if err := store.MarkTriggersFailed(ctx, []freshness.StoredTrigger{staleClaim}, now, "stale-failure", "stale holder must not be able to fail this claim"); err != nil {
		t.Fatalf("MarkTriggersFailed(stale) error = %v", err)
	}
	assertAWSFreshnessTriggerStatus(t, db, staleClaim.TriggerID, string(freshness.TriggerStatusClaimed))

	// The new owner, presenting the current fencing token, can complete the
	// claim.
	if err := store.MarkTriggersHandedOff(ctx, []freshness.StoredTrigger{newClaim}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff(new) error = %v", err)
	}
	assertAWSFreshnessTriggerStatus(t, db, newClaim.TriggerID, string(freshness.TriggerStatusHandedOff))
}

// assertAWSFreshnessTriggerStatus reads a trigger's current status directly
// (bypassing the store API, which has no read-by-id method) so the test can
// prove which write actually landed.
func assertAWSFreshnessTriggerStatus(t *testing.T, db *sql.DB, triggerID string, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(context.Background(), "SELECT status FROM aws_freshness_triggers WHERE trigger_id = $1", triggerID).Scan(&got); err != nil {
		t.Fatalf("read trigger status: %v", err)
	}
	if got != want {
		t.Fatalf("trigger %q status = %q, want %q", triggerID, got, want)
	}
}

// freshnessLeaseProofDB opens an isolated-schema connection against dsn so
// this suite's rows never collide with another integration test's fixtures
// sharing the same database. The pool is capped at one connection: SET
// search_path is session-scoped, and this suite's concurrent-safety test
// deliberately issues overlapping ReapExpiredTriggerClaims calls that must
// still land in the proof schema rather than a fresh, unconfigured pooled
// connection (mirroring provisionLivenessSchema's sweepDB.SetMaxOpenConns(1)
// pattern in generation_liveness_write_time_race_test.go).
func freshnessLeaseProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	schemaName := fmt.Sprintf("freshness_claim_lease_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	return db
}
