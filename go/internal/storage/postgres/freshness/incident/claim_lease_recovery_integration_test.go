// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incidentfreshnessstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	incidentfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/incident"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ESHU_INCIDENT_FRESHNESS_CLAIM_LEASE_PROOF_DSN gates this suite against a
// real Postgres instance. It is skipped otherwise so the normal unit gate
// is unaffected, mirroring the AWS/GCP freshness claim-lease proof.
const incidentClaimLeaseProofDSNEnv = "ESHU_INCIDENT_FRESHNESS_CLAIM_LEASE_PROOF_DSN"

// TestIncidentFreshnessStoreReapExpiredTriggerClaimsLive proves #9379's
// stuck-claim reclaim against a real Postgres instance: a trigger claimed
// with an already-expired lease is reclaimed back to 'queued' (recovering
// it from a mid-batch handoff abort or coordinator crash), while a trigger
// claimed with a lease that has NOT yet expired is left untouched — the
// reap must never race a still-live claim-holder within its lease window.
func TestIncidentFreshnessStoreReapExpiredTriggerClaimsLive(t *testing.T) {
	dsn := os.Getenv(incidentClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_INCIDENT_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the incident freshness claim-lease recovery proof")
	}

	db := incidentLeaseProofDB(t, dsn)
	store := incidentfreshnessstore.NewIncidentFreshnessStore(postgres.SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if _, err := store.StoreIncidentFreshnessTrigger(ctx, incidentLeaseTrigger("evt-expired", "pagerduty:account:expired", now), now.Add(-time.Second)); err != nil {
		t.Fatalf("StoreIncidentFreshnessTrigger(expired) error = %v", err)
	}
	if _, err := store.StoreIncidentFreshnessTrigger(ctx, incidentLeaseTrigger("evt-live", "pagerduty:account:live", now), now); err != nil {
		t.Fatalf("StoreIncidentFreshnessTrigger(live) error = %v", err)
	}

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
	if reclaimed[0].EventID != "evt-expired" {
		t.Fatalf("reclaimed trigger = %+v, want the expired-lease trigger", reclaimed[0])
	}
	if reclaimed[0].Status != webhook.TriggerStatusQueued {
		t.Fatalf("reclaimed Status = %q, want %q", reclaimed[0].Status, webhook.TriggerStatusQueued)
	}

	secondPass, err := store.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("second ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(secondPass) != 0 {
		t.Fatalf("second reap pass reclaimed = %+v, want none (idempotent no-op)", secondPass)
	}
}

// TestIncidentFreshnessStoreStaleHolderCannotCompleteReapedClaimLive proves
// the #9379 claim_fencing_token fencing against a real Postgres instance:
// a trigger claimed by one owner, reaped back to 'queued' after its lease
// expires, and then re-claimed by a different owner, cannot be completed by
// the original stale holder using the fencing token from its now-expired
// claim (no dual authority). A duplicate provider delivery landing while
// the row is claimed coalesces without losing the trigger (no lost facts),
// and only the new owner's completion, presenting the current token,
// succeeds.
func TestIncidentFreshnessStoreStaleHolderCannotCompleteReapedClaimLive(t *testing.T) {
	dsn := os.Getenv(incidentClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_INCIDENT_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the incident freshness claim-lease recovery proof")
	}

	db := incidentLeaseProofDB(t, dsn)
	store := incidentfreshnessstore.NewIncidentFreshnessStore(postgres.SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.October, 28, 13, 0, 0, 0, time.UTC)
	ctx := context.Background()

	if _, err := store.StoreIncidentFreshnessTrigger(ctx, incidentLeaseTrigger("evt-stale-holder", "pagerduty:account:stale", now), now); err != nil {
		t.Fatalf("StoreIncidentFreshnessTrigger() error = %v", err)
	}

	// Original holder claims with a lease that is already expired relative
	// to `now`, mirroring a coordinator replica that claimed, then stalled
	// past its lease while the reaper moved on.
	staleClaims, err := store.ClaimQueuedTriggers(ctx, "claimant-stale", now.Add(-10*time.Minute), 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers(stale) error = %v", err)
	}
	if len(staleClaims) != 1 {
		t.Fatalf("len(staleClaims) = %d, want 1", len(staleClaims))
	}
	staleClaim := staleClaims[0]

	// A duplicate provider delivery landing while the row is claimed must
	// coalesce (duplicate_count+1) without disturbing the live claim.
	redelivered, err := store.StoreIncidentFreshnessTrigger(ctx, incidentLeaseTrigger("evt-stale-holder", "pagerduty:account:stale", now), now)
	if err != nil {
		t.Fatalf("StoreIncidentFreshnessTrigger(redelivery) error = %v", err)
	}
	if redelivered.Status != webhook.TriggerStatusClaimed {
		t.Fatalf("redelivered Status = %q, want %q (claimed row stays claimed)", redelivered.Status, webhook.TriggerStatusClaimed)
	}
	if redelivered.DuplicateCount != 1 {
		t.Fatalf("redelivered DuplicateCount = %d, want 1", redelivered.DuplicateCount)
	}

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
	if err := store.MarkTriggersHandedOff(ctx, []webhook.StoredIncidentFreshnessTrigger{staleClaim}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff(stale) error = %v", err)
	}
	assertIncidentFreshnessTriggerStatus(t, db, staleClaim.TriggerID, string(webhook.TriggerStatusClaimed))

	if err := store.MarkTriggersFailed(ctx, []webhook.StoredIncidentFreshnessTrigger{staleClaim}, now, "stale-failure", "stale holder must not be able to fail this claim"); err != nil {
		t.Fatalf("MarkTriggersFailed(stale) error = %v", err)
	}
	assertIncidentFreshnessTriggerStatus(t, db, staleClaim.TriggerID, string(webhook.TriggerStatusClaimed))

	// The new owner, presenting the current fencing token, can complete the
	// claim.
	if err := store.MarkTriggersHandedOff(ctx, []webhook.StoredIncidentFreshnessTrigger{newClaim}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff(new) error = %v", err)
	}
	assertIncidentFreshnessTriggerStatus(t, db, newClaim.TriggerID, string(webhook.TriggerStatusHandedOff))
}

// TestIncidentFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety proves
// the reap query's FOR UPDATE SKIP LOCKED concurrency guarantee against a
// real Postgres instance: concurrent reap passes racing the same batch of
// expired claims never reclaim the same trigger row twice.
func TestIncidentFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety(t *testing.T) {
	dsn := os.Getenv(incidentClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_INCIDENT_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the incident freshness claim-lease recovery proof")
	}

	db := incidentLeaseProofDB(t, dsn)
	store := incidentfreshnessstore.NewIncidentFreshnessStore(postgres.SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.October, 28, 14, 0, 0, 0, time.UTC)
	ctx := context.Background()

	const triggerCount = 10
	for i := 0; i < triggerCount; i++ {
		trigger := incidentLeaseTrigger(fmt.Sprintf("evt-concurrent-%d", i), fmt.Sprintf("pagerduty:account:concurrent-%02d", i), now)
		if _, err := store.StoreIncidentFreshnessTrigger(ctx, trigger, now); err != nil {
			t.Fatalf("StoreIncidentFreshnessTrigger(%d) error = %v", i, err)
		}
	}
	if _, err := store.ClaimQueuedTriggers(ctx, "claimant-concurrent", now.Add(-10*time.Minute), triggerCount, time.Minute); err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v", err)
	}

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		reclaimedBy = make(map[string]int)
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

func incidentLeaseTrigger(eventID, scopeID string, observedAt time.Time) webhook.IncidentFreshnessTrigger {
	return webhook.IncidentFreshnessTrigger{
		Provider:   webhook.ProviderPagerDuty,
		EventKind:  "incident.triggered",
		EventID:    eventID,
		ScopeID:    scopeID,
		ResourceID: "PINC123",
		ObservedAt: observedAt,
	}
}

// assertIncidentFreshnessTriggerStatus reads a trigger's current status
// directly (bypassing the store API, which has no read-by-id method) so the
// test can prove which write actually landed.
func assertIncidentFreshnessTriggerStatus(t *testing.T, db *sql.DB, triggerID string, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(context.Background(), "SELECT status FROM incident_freshness_triggers WHERE trigger_id = $1", triggerID).Scan(&got); err != nil {
		t.Fatalf("read trigger status: %v", err)
	}
	if got != want {
		t.Fatalf("trigger %q status = %q, want %q", triggerID, got, want)
	}
}

// incidentLeaseProofDB opens an isolated-schema connection against dsn so
// this suite's rows never collide with another integration test's fixtures
// sharing the same database. The pool is capped at one connection: SET
// search_path is session-scoped, and this suite's concurrent-safety test
// deliberately issues overlapping ReapExpiredTriggerClaims calls that must
// still land in the proof schema rather than a fresh, unconfigured pooled
// connection.
func incidentLeaseProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	schemaName := fmt.Sprintf("incident_claim_lease_proof_%d", time.Now().UnixNano())
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
