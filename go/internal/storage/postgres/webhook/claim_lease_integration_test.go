// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhookstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	webhookstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// ESHU_WEBHOOK_CLAIM_LEASE_PROOF_DSN gates this suite against a real
// Postgres instance, mirroring the AWS/GCP freshness claim-lease proofs.
const webhookClaimLeaseProofDSNEnv = "ESHU_WEBHOOK_CLAIM_LEASE_PROOF_DSN"

func webhookLeaseProofStore(t *testing.T) (*webhookstore.WebhookTriggerStore, *sql.DB) {
	t.Helper()
	dsn := os.Getenv(webhookClaimLeaseProofDSNEnv)
	if dsn == "" {
		t.Skip("set ESHU_WEBHOOK_CLAIM_LEASE_PROOF_DSN to run the webhook claim-lease integration proof")
	}
	bootstrap, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	// LIFO cleanups: drop the schema before the bootstrap pool closes.
	// Closing here via defer would run at helper return, leaving the
	// later DROP to fail silently on a closed pool and leak the schema.
	t.Cleanup(func() { _ = bootstrap.Close() })
	ctx := context.Background()
	schemaName := fmt.Sprintf("webhook_claim_lease_proof_%d", time.Now().UnixNano())
	if _, err := bootstrap.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = bootstrap.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	// Pin search_path per connection through the DSN: a bare SET would
	// only cover the pooled connection it ran on, and the concurrent
	// reapers open their own.
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	schemaDSN := dsn + separator + "options=" + url.QueryEscape("-c search_path="+schemaName)
	db, err := sql.Open("pgx", schemaDSN)
	if err != nil {
		t.Fatalf("open proof connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)
	store := webhookstore.NewWebhookTriggerStore(postgres.SQLDB{DB: db})
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	return store, db
}

func seedWebhookLeaseTrigger(t *testing.T, ctx context.Context, store *webhookstore.WebhookTriggerStore, suffix string, receivedAt time.Time) webhook.StoredTrigger {
	t.Helper()
	stored, err := store.StoreTrigger(ctx, webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionAccepted,
		Reason:               "",
		DeliveryID:           "delivery-" + suffix,
		RepositoryExternalID: "repo-" + suffix,
		RepositoryFullName:   "org/repo-" + suffix,
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "sha-" + suffix,
		Action:               "synchronize",
		Sender:               "octocat",
	}, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() error = %v", err)
	}
	return stored
}

func claimWebhookLeaseTrigger(t *testing.T, ctx context.Context, store *webhookstore.WebhookTriggerStore, owner string, claimedAt time.Time) webhook.StoredTrigger {
	t.Helper()
	claimed, err := store.ClaimQueuedTriggers(ctx, owner, claimedAt, 1)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimQueuedTriggers() returned %d triggers, want 1", len(claimed))
	}
	return claimed[0]
}

// TestWebhookTriggerStoreReapExpiredClaimsIntegration proves #7661's lease
// reclaim: a claimed row older than the lease window goes back to queued
// and a fresh claimed row is untouched. Attempt exhaustion is proven by
// TestWebhookTriggerStoreExhaustedClaimsFailWithReason.
func TestWebhookTriggerStoreReapExpiredClaimsIntegration(t *testing.T) {
	store, db := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	leaseWindow := 5 * time.Minute
	staleBefore := now.Add(-leaseWindow)

	stale := seedWebhookLeaseTrigger(t, ctx, store, "stale", now.Add(-time.Hour))
	staleClaim := claimWebhookLeaseTrigger(t, ctx, store, "owner-a", now.Add(-time.Hour))
	fresh := seedWebhookLeaseTrigger(t, ctx, store, "fresh", now.Add(-time.Minute))
	freshClaim := claimWebhookLeaseTrigger(t, ctx, store, "owner-a", now.Add(-time.Minute))
	_ = freshClaim
	_ = stale
	_ = staleClaim

	requeued, exhausted, err := store.ReapExpiredTriggerClaims(ctx, staleBefore, 3, 100, now)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(requeued) != 1 || requeued[0].TriggerID != stale.TriggerID {
		t.Fatalf("requeued = %v, want only the stale trigger", triggerIDs(requeued))
	}
	if len(exhausted) != 0 {
		t.Fatalf("exhausted = %v, want none", triggerIDs(exhausted))
	}
	assertWebhookTriggerStatus(t, db, stale.TriggerID, "queued")
	assertWebhookTriggerStatus(t, db, fresh.TriggerID, "claimed")

	if got := webhookStuckCount(t, ctx, store, staleBefore); got != 0 {
		t.Fatalf("stuck claimed rows after reap = %d, want 0", got)
	}
}

func triggerIDs(triggers []webhook.StoredTrigger) []string {
	ids := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		ids = append(ids, trigger.TriggerID)
	}
	return ids
}

func assertWebhookTriggerStatus(t *testing.T, db *sql.DB, triggerID string, want string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(context.Background(), "SELECT status FROM webhook_refresh_triggers WHERE trigger_id = $1", triggerID).Scan(&got); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if got != want {
		t.Fatalf("status of %s = %q, want %q", triggerID, got, want)
	}
}

func webhookStuckCount(t *testing.T, ctx context.Context, store *webhookstore.WebhookTriggerStore, staleBefore time.Time) int64 {
	t.Helper()
	n, err := store.CountStaleClaims(ctx, staleBefore)
	if err != nil {
		t.Fatalf("CountStaleClaims() error = %v", err)
	}
	return n
}

// TestWebhookTriggerStoreReapConcurrentSafety proves two concurrent
// reclaimers never double-reap a row: FOR UPDATE SKIP LOCKED partitions
// the stale set between them.
func TestWebhookTriggerStoreReapConcurrentSafety(t *testing.T) {
	store, db := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)
	const rows = 20
	for i := 0; i < rows; i++ {
		seedWebhookLeaseTrigger(t, ctx, store, fmt.Sprintf("race-%d", i), now.Add(-time.Hour))
	}
	if _, err := store.ClaimQueuedTriggers(ctx, "owner-a", now.Add(-time.Hour), rows); err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v", err)
	}

	var wg sync.WaitGroup
	results := make([][]webhook.StoredTrigger, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			requeued, _, err := store.ReapExpiredTriggerClaims(ctx, staleBefore, 3, rows, now)
			results[i] = requeued
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("reaper %d error = %v", i, err)
		}
	}
	seen := map[string]int{}
	for _, list := range results {
		for _, trigger := range list {
			seen[trigger.TriggerID]++
		}
	}
	if len(seen) != rows {
		t.Fatalf("reaped %d distinct rows, want %d", len(seen), rows)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %s reaped %d times, want 1", id, n)
		}
	}
	var queued int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM webhook_refresh_triggers WHERE status = 'queued'").Scan(&queued); err != nil {
		t.Fatalf("count queued: %v", err)
	}
	if queued != rows {
		t.Fatalf("queued rows = %d, want %d", queued, rows)
	}
}

// TestWebhookTriggerStoreStaleHolderCannotCompleteReapedClaim proves the
// fencing token: after a reap and a re-claim by another owner, the
// original holder's handoff affects zero rows while the new holder's
// completes.
func TestWebhookTriggerStoreStaleHolderCannotCompleteReapedClaim(t *testing.T) {
	store, db := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)

	seeded := seedWebhookLeaseTrigger(t, ctx, store, "fence", now.Add(-time.Hour))
	first := claimWebhookLeaseTrigger(t, ctx, store, "owner-a", now.Add(-time.Hour))
	if _, _, err := store.ReapExpiredTriggerClaims(ctx, staleBefore, 3, 100, now); err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v", err)
	}
	second := claimWebhookLeaseTrigger(t, ctx, store, "owner-b", now)
	if second.ClaimFencingToken == first.ClaimFencingToken {
		t.Fatalf("re-claim token = %d, want a bump over %d", second.ClaimFencingToken, first.ClaimFencingToken)
	}
	// Stale holder finishes late: must not complete owner-b's claim.
	if err := store.MarkTriggersHandedOff(ctx, []webhook.StoredTrigger{first}, now); err != nil {
		t.Fatalf("stale MarkTriggersHandedOff() error = %v", err)
	}
	assertWebhookTriggerStatus(t, db, seeded.TriggerID, "claimed")
	// Live holder completes normally.
	if err := store.MarkTriggersHandedOff(ctx, []webhook.StoredTrigger{second}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff() error = %v", err)
	}
	assertWebhookTriggerStatus(t, db, seeded.TriggerID, "handed_off")
}

// TestWebhookTriggerStoreExhaustedClaimsFailWithReason proves the #7661
// poison-row bound: the fencing token doubles as the attempt counter, so
// after maxAttempts claim/reap cycles the row lands in failed with the
// claim_lease_exhausted reason instead of looping through the lease
// forever.
func TestWebhookTriggerStoreExhaustedClaimsFailWithReason(t *testing.T) {
	store, db := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)
	const maxAttempts = 3

	seeded := seedWebhookLeaseTrigger(t, ctx, store, "poison", now.Add(-2*time.Hour))
	staleClaimAt := now.Add(-time.Hour)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		claimed := claimWebhookLeaseTrigger(t, ctx, store, fmt.Sprintf("owner-%d", attempt), staleClaimAt)
		if claimed.ClaimFencingToken != int64(attempt) {
			t.Fatalf("attempt %d token = %d, want %d", attempt, claimed.ClaimFencingToken, attempt)
		}
		requeued, exhausted, err := store.ReapExpiredTriggerClaims(ctx, staleBefore, maxAttempts, 100, now)
		if err != nil {
			t.Fatalf("attempt %d ReapExpiredTriggerClaims() error = %v", attempt, err)
		}
		if attempt < maxAttempts {
			if len(requeued) != 1 || len(exhausted) != 0 {
				t.Fatalf("attempt %d requeued/exhausted = %d/%d, want 1/0", attempt, len(requeued), len(exhausted))
			}
			continue
		}
		if len(requeued) != 0 || len(exhausted) != 1 {
			t.Fatalf("attempt %d requeued/exhausted = %d/%d, want 0/1", attempt, len(requeued), len(exhausted))
		}
	}
	assertWebhookTriggerStatus(t, db, seeded.TriggerID, "failed")
	var failureClass, failureMessage string
	if err := db.QueryRowContext(ctx, "SELECT failure_class, failure_message FROM webhook_refresh_triggers WHERE trigger_id = $1", seeded.TriggerID).Scan(&failureClass, &failureMessage); err != nil {
		t.Fatalf("read failure reason: %v", err)
	}
	if failureClass != "claim_lease_exhausted" {
		t.Fatalf("failure_class = %q, want claim_lease_exhausted", failureClass)
	}
	if failureMessage == "" {
		t.Fatal("failure_message is empty, want a poison-row reason")
	}
	// A further reap is stable: the failed row is never requeued.
	requeued, exhausted, err := store.ReapExpiredTriggerClaims(ctx, staleBefore, maxAttempts, 100, now)
	if err != nil {
		t.Fatalf("final ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(requeued) != 0 || len(exhausted) != 0 {
		t.Fatalf("final requeued/exhausted = %d/%d, want 0/0", len(requeued), len(exhausted))
	}
}

// TestWebhookTriggerStoreReapRequeuesTokenZeroDeployRow proves the #7661
// deploy shape: every row stuck at rollout carries claim_fencing_token 0
// (the pre-migration default), so the first reap must requeue it, never
// exhaust it.
func TestWebhookTriggerStoreReapRequeuesTokenZeroDeployRow(t *testing.T) {
	store, db := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)

	if _, err := db.ExecContext(ctx, `INSERT INTO webhook_refresh_triggers (
    trigger_id, delivery_key, refresh_key, provider, event_kind, decision,
    delivery_id, repository_external_id, repository_full_name, default_branch,
    ref, target_sha, status, received_at, updated_at, claimed_by, claimed_at,
    claim_fencing_token
) VALUES (
    'deploy-stuck', 'delivery-deploy-stuck', 'refresh-deploy-stuck',
    'github', 'push', 'accepted', 'delivery-deploy-stuck', 'repo',
    'org/repo', 'main', 'refs/heads/main', 'sha', 'claimed',
    $1, $1, 'owner-dead', $1, 0
)`, now.Add(-time.Hour)); err != nil {
		t.Fatalf("seed token-0 stuck row: %v", err)
	}

	requeued, exhausted, err := store.ReapExpiredTriggerClaims(ctx, staleBefore, 3, 100, now)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v", err)
	}
	if len(requeued) != 1 || requeued[0].TriggerID != "deploy-stuck" {
		t.Fatalf("requeued = %v, want the token-0 row", triggerIDs(requeued))
	}
	if len(exhausted) != 0 {
		t.Fatalf("exhausted = %v, want none", triggerIDs(exhausted))
	}
	assertWebhookTriggerStatus(t, db, "deploy-stuck", "queued")
}

// TestWebhookTriggerStoreConcurrentClaimersSplitQueued proves two
// concurrent claimers never double-claim a row: FOR UPDATE SKIP LOCKED
// partitions the queued set between them and the union is complete.
func TestWebhookTriggerStoreConcurrentClaimersSplitQueued(t *testing.T) {
	store, _ := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	const rows = 40
	for i := 0; i < rows; i++ {
		seedWebhookLeaseTrigger(t, ctx, store, fmt.Sprintf("claim-race-%d", i), now.Add(-time.Hour))
	}

	var wg sync.WaitGroup
	results := make([][]webhook.StoredTrigger, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimed, err := store.ClaimQueuedTriggers(ctx, fmt.Sprintf("owner-%c", 'a'+i), now, rows)
			results[i] = claimed
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("claimer %d error = %v", i, err)
		}
	}
	seen := map[string]int{}
	for _, list := range results {
		for _, trigger := range list {
			seen[trigger.TriggerID]++
		}
	}
	if len(seen) != rows {
		t.Fatalf("claimed %d distinct rows, want %d", len(seen), rows)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %s claimed %d times, want 1", id, n)
		}
	}
}
