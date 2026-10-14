// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhookstore_test

import (
	"context"
	"testing"
	"time"

	webhookstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// storeIgnoredRedelivery stores an ignored trigger carrying the same
// refresh key as suffix's accepted seed (same provider, repository,
// branch, and target SHA) under a fresh delivery id, mirroring a tag or
// non-default-branch redelivery for already-queued refresh work.
func storeIgnoredRedelivery(t *testing.T, ctx context.Context, store *webhookstore.WebhookTriggerStore, suffix string, receivedAt time.Time) webhook.StoredTrigger {
	t.Helper()
	stored, err := store.StoreTrigger(ctx, webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionIgnored,
		Reason:               webhook.ReasonTagRef,
		DeliveryID:           "delivery-ignored-" + suffix,
		RepositoryExternalID: "repo-" + suffix,
		RepositoryFullName:   "org/repo-" + suffix,
		DefaultBranch:        "main",
		Ref:                  "refs/tags/v1.0.0",
		TargetSHA:            "sha-" + suffix,
		Sender:               "octocat",
	}, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() ignored redelivery error = %v, want nil", err)
	}
	return stored
}

// TestWebhookTriggerStoreUpsertDemotesQueuedToIgnored is the #7795
// upsert-half regression: an ignored redelivery for the same refresh key
// must move the row back to ignored (status tracks the latest decision),
// instead of leaving a queued row with an ignored decision behind for
// the claim to pick up.
func TestWebhookTriggerStoreUpsertDemotesQueuedToIgnored(t *testing.T) {
	store, _ := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)

	seeded := seedWebhookLeaseTrigger(t, ctx, store, "demote", now)
	if seeded.Status != webhook.TriggerStatusQueued {
		t.Fatalf("seeded status = %q, want queued", seeded.Status)
	}
	redelivered := storeIgnoredRedelivery(t, ctx, store, "demote", now.Add(time.Minute))
	if redelivered.Decision != webhook.DecisionIgnored {
		t.Fatalf("redelivered decision = %q, want ignored", redelivered.Decision)
	}
	if redelivered.Status != webhook.TriggerStatusIgnored {
		t.Fatalf("redelivered status = %q, want ignored: the row must not stay queued with an ignored decision", redelivered.Status)
	}
}

// TestWebhookTriggerStoreReapFailsIgnoredDecisionClaims is the #7795
// reap-half regression: a stale claimed row whose decision is ignored
// must leave the requeue out of it and land terminal with the
// ignored_decision class — never requeued, and never failed with the
// accepted-row claim_lease_exhausted class.
func TestWebhookTriggerStoreReapFailsIgnoredDecisionClaims(t *testing.T) {
	store, db := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	staleBefore := now.Add(-5 * time.Minute)

	seeded := seedWebhookLeaseTrigger(t, ctx, store, "reap-ignored", now.Add(-time.Hour))
	claimWebhookLeaseTrigger(t, ctx, store, "owner-a", now.Add(-time.Hour))
	// The ignored redelivery lands while the accepted claim is stranded:
	// the decision flips under the claimed status.
	storeIgnoredRedelivery(t, ctx, store, "reap-ignored", now.Add(-30*time.Minute))

	requeued, exhausted, ignored, err := store.ReapExpiredTriggerClaims(ctx, staleBefore, 3, 100, now)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v, want nil", err)
	}
	if len(requeued) != 0 {
		t.Fatalf("requeued = %v, want none: ignored-decision rows stay out of the requeue", triggerIDs(requeued))
	}
	if len(exhausted) != 0 {
		t.Fatalf("exhausted = %v, want none: ignored-decision rows never take the lease-exhaust path", triggerIDs(exhausted))
	}
	if len(ignored) != 1 || ignored[0].TriggerID != seeded.TriggerID {
		t.Fatalf("ignored = %v, want only the ignored-decision trigger", triggerIDs(ignored))
	}
	assertWebhookTriggerStatus(t, db, seeded.TriggerID, "failed")
	var failureClass string
	if err := db.QueryRowContext(ctx, "SELECT failure_class FROM webhook_refresh_triggers WHERE trigger_id = $1", seeded.TriggerID).Scan(&failureClass); err != nil {
		t.Fatalf("read failure_class: %v", err)
	}
	if failureClass != "ignored_decision" {
		t.Fatalf("failure_class = %q, want ignored_decision", failureClass)
	}
	// A further reap is stable: the terminal row is never touched again.
	requeued, exhausted, ignored, err = store.ReapExpiredTriggerClaims(ctx, staleBefore, 3, 100, now)
	if err != nil {
		t.Fatalf("final ReapExpiredTriggerClaims() error = %v, want nil", err)
	}
	if len(requeued) != 0 || len(exhausted) != 0 || len(ignored) != 0 {
		t.Fatalf("final requeued/exhausted/ignored = %d/%d/%d, want 0/0/0", len(requeued), len(exhausted), len(ignored))
	}
}
