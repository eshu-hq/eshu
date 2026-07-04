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

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
)

// TestGCPFreshnessStoreReapExpiredTriggerClaimsIntegration mirrors
// TestAWSFreshnessStoreReapExpiredTriggerClaimsIntegration for the GCP store;
// see that test's doc comment for the #4576 rationale.
func TestGCPFreshnessStoreReapExpiredTriggerClaimsIntegration(t *testing.T) {
	dsn := os.Getenv(freshnessClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the AWS/GCP freshness claim-lease integration proof")
	}

	db := freshnessLeaseProofDB(t, dsn)
	store := NewGCPFreshnessStore(SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.July, 4, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	expiredTrigger := freshness.Trigger{
		EventID:         "evt-expired",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "demo-project-expired",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      now,
	}
	// liveTrigger uses a different ParentScopeID so its FreshnessKey is
	// distinct from expiredTrigger's — StoreTrigger coalesces same-key
	// triggers into one row, which would otherwise collapse these two
	// triggers into a single claimable row.
	liveTrigger := freshness.Trigger{
		EventID:         "evt-live",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "demo-project-live",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      now,
	}

	if _, err := store.StoreTrigger(ctx, expiredTrigger, now.Add(-time.Second)); err != nil {
		t.Fatalf("StoreTrigger(expired) error = %v", err)
	}
	if _, err := store.StoreTrigger(ctx, liveTrigger, now); err != nil {
		t.Fatalf("StoreTrigger(live) error = %v", err)
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
	if reclaimed[0].ParentScopeID != "demo-project-expired" {
		t.Fatalf("reclaimed trigger = %+v, want the expired-lease trigger", reclaimed[0])
	}
	if reclaimed[0].Status != freshness.TriggerStatusQueued {
		t.Fatalf("reclaimed Status = %q, want %q", reclaimed[0].Status, freshness.TriggerStatusQueued)
	}

	secondPass, err := store.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("second ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(secondPass) != 0 {
		t.Fatalf("second reap pass reclaimed = %+v, want none (idempotent no-op)", secondPass)
	}
}

// TestGCPFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety mirrors
// TestAWSFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety for the GCP
// store; see that test's doc comment for the concurrency rationale.
func TestGCPFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety(t *testing.T) {
	dsn := os.Getenv(freshnessClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the AWS/GCP freshness claim-lease integration proof")
	}

	db := freshnessLeaseProofDB(t, dsn)
	store := NewGCPFreshnessStore(SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.July, 4, 13, 0, 0, 0, time.UTC)
	ctx := context.Background()

	const triggerCount = 20
	for i := 0; i < triggerCount; i++ {
		trigger := freshness.Trigger{
			EventID:         fmt.Sprintf("evt-concurrent-%d", i),
			Kind:            freshness.EventKindAssetChange,
			ParentScopeKind: gcpcloud.ParentScopeProject,
			ParentScopeID:   fmt.Sprintf("demo-project-concurrent-%02d", i),
			AssetType:       "compute.googleapis.com/Instance",
			Location:        "us-central1-a",
			ObservedAt:      now,
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

// TestGCPFreshnessStoreStaleHolderCannotCompleteReapedClaimIntegration mirrors
// TestAWSFreshnessStoreStaleHolderCannotCompleteReapedClaimIntegration for the
// GCP store; see that test's doc comment for the claim_fencing_token
// rationale (#4576, raised in PR #4682 review).
func TestGCPFreshnessStoreStaleHolderCannotCompleteReapedClaimIntegration(t *testing.T) {
	dsn := os.Getenv(freshnessClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_FRESHNESS_CLAIM_LEASE_PROOF_DSN to run the AWS/GCP freshness claim-lease integration proof")
	}

	db := freshnessLeaseProofDB(t, dsn)
	store := NewGCPFreshnessStore(SQLDB{DB: db})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	now := time.Date(2026, time.July, 4, 14, 0, 0, 0, time.UTC)
	ctx := context.Background()

	trigger := freshness.Trigger{
		EventID:         "evt-stale-holder",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "demo-project-stale-holder",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      now,
	}
	if _, err := store.StoreTrigger(ctx, trigger, now); err != nil {
		t.Fatalf("StoreTrigger() error = %v", err)
	}

	staleClaims, err := store.ClaimQueuedTriggers(ctx, "claimant-stale", now.Add(-10*time.Minute), 1, time.Minute)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers(stale) error = %v", err)
	}
	if len(staleClaims) != 1 {
		t.Fatalf("len(staleClaims) = %d, want 1", len(staleClaims))
	}
	staleClaim := staleClaims[0]

	reclaimed, err := store.ReapExpiredTriggerClaims(ctx, now, 50)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("len(reclaimed) = %d, want 1", len(reclaimed))
	}

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

	if err := store.MarkTriggersHandedOff(ctx, []freshness.StoredTrigger{staleClaim}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff(stale) error = %v", err)
	}
	assertGCPFreshnessTriggerStatus(t, db, staleClaim.TriggerID, string(freshness.TriggerStatusClaimed))

	if err := store.MarkTriggersFailed(ctx, []freshness.StoredTrigger{staleClaim}, now, "stale-failure", "stale holder must not be able to fail this claim"); err != nil {
		t.Fatalf("MarkTriggersFailed(stale) error = %v", err)
	}
	assertGCPFreshnessTriggerStatus(t, db, staleClaim.TriggerID, string(freshness.TriggerStatusClaimed))

	if err := store.MarkTriggersHandedOff(ctx, []freshness.StoredTrigger{newClaim}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff(new) error = %v", err)
	}
	assertGCPFreshnessTriggerStatus(t, db, newClaim.TriggerID, string(freshness.TriggerStatusHandedOff))
}

// assertGCPFreshnessTriggerStatus mirrors assertAWSFreshnessTriggerStatus for
// the gcp_freshness_triggers table.
func assertGCPFreshnessTriggerStatus(t *testing.T, db *sql.DB, triggerID string, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(context.Background(), "SELECT status FROM gcp_freshness_triggers WHERE trigger_id = $1", triggerID).Scan(&got); err != nil {
		t.Fatalf("read trigger status: %v", err)
	}
	if got != want {
		t.Fatalf("trigger %q status = %q, want %q", triggerID, got, want)
	}
}
