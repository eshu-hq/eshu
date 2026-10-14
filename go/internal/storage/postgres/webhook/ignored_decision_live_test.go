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

// storeAcceptedRedelivery stores an accepted trigger carrying the same
// refresh key as suffix's seed (same provider, repository, branch, and
// target SHA) under a fresh delivery id, mirroring an accepted redelivery
// that flips a claimed+ignored row back to accepted under the live claim.
func storeAcceptedRedelivery(t *testing.T, ctx context.Context, store *webhookstore.WebhookTriggerStore, suffix string, receivedAt time.Time) webhook.StoredTrigger {
	t.Helper()
	stored, err := store.StoreTrigger(ctx, webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionAccepted,
		DeliveryID:           "delivery-accepted-" + suffix,
		RepositoryExternalID: "repo-" + suffix,
		RepositoryFullName:   "org/repo-" + suffix,
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "sha-" + suffix,
		Sender:               "octocat",
	}, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() accepted redelivery error = %v, want nil", err)
	}
	return stored
}

// ignoredSnapshotWithDecision rewrites a claim snapshot's decision the way
// the selector's claim snapshot looks after an ignored redelivery landed
// under the claim: the fencing token still matches, the decision reads
// ignored.
func ignoredSnapshotWithDecision(claimed webhook.StoredTrigger) webhook.StoredTrigger {
	claimed.Decision = webhook.DecisionIgnored
	claimed.Reason = webhook.ReasonTagRef
	return claimed
}

// TestWebhookTriggerStoreIgnoredDecisionFailIsDecisionGuarded is the #7795
// P1 statement-level regression: the same-tick ignored-decision fail must
// carry AND decision <> 'accepted' at UPDATE time and RETURN only the rows
// it actually terminated. A row flipped back to accepted by a redelivery
// between the claim snapshot and the fail stays claimed for the handoff,
// and a row presented with a stale fencing token stays untouched.
func TestWebhookTriggerStoreIgnoredDecisionFailIsDecisionGuarded(t *testing.T) {
	store, db := webhookLeaseProofStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)

	seedWebhookLeaseTrigger(t, ctx, store, "guard-stays", now.Add(-time.Hour))
	staysClaim := claimWebhookLeaseTrigger(t, ctx, store, "owner-a", now.Add(-time.Hour))
	storeIgnoredRedelivery(t, ctx, store, "guard-stays", now.Add(-30*time.Minute))

	seedWebhookLeaseTrigger(t, ctx, store, "guard-flip", now.Add(-time.Hour))
	flipClaim := claimWebhookLeaseTrigger(t, ctx, store, "owner-a", now.Add(-time.Hour))
	storeIgnoredRedelivery(t, ctx, store, "guard-flip", now.Add(-30*time.Minute))
	// The accepted redelivery lands between the claim snapshot and the
	// same-tick fail: the decision flips under the live claim without
	// bumping the fencing token.
	flipped := storeAcceptedRedelivery(t, ctx, store, "guard-flip", now.Add(-15*time.Minute))
	if flipped.Decision != webhook.DecisionAccepted {
		t.Fatalf("flipped decision = %q, want accepted", flipped.Decision)
	}
	if flipped.ClaimFencingToken != flipClaim.ClaimFencingToken {
		t.Fatalf("flipped fencing token = %d, want the live claim token %d", flipped.ClaimFencingToken, flipClaim.ClaimFencingToken)
	}

	seedWebhookLeaseTrigger(t, ctx, store, "guard-fence", now.Add(-time.Hour))
	fenceClaim := claimWebhookLeaseTrigger(t, ctx, store, "owner-a", now.Add(-time.Hour))
	storeIgnoredRedelivery(t, ctx, store, "guard-fence", now.Add(-30*time.Minute))
	staleFence := ignoredSnapshotWithDecision(fenceClaim)
	staleFence.ClaimFencingToken += 100

	failed, err := store.MarkIgnoredDecisionTriggersFailed(ctx,
		[]webhook.StoredTrigger{
			ignoredSnapshotWithDecision(staysClaim),
			ignoredSnapshotWithDecision(flipClaim),
			staleFence,
		},
		now, "ignored_decision",
		"webhook trigger decision is not accepted for the git collector handoff")
	if err != nil {
		t.Fatalf("MarkIgnoredDecisionTriggersFailed() error = %v, want nil", err)
	}
	if len(failed) != 1 || failed[0].TriggerID != staysClaim.TriggerID {
		t.Fatalf("failed = %v, want only the still-ignored trigger", triggerIDs(failed))
	}

	assertWebhookTriggerStatus(t, db, staysClaim.TriggerID, "failed")
	var failureClass string
	if err := db.QueryRowContext(ctx, "SELECT failure_class FROM webhook_refresh_triggers WHERE trigger_id = $1", staysClaim.TriggerID).Scan(&failureClass); err != nil {
		t.Fatalf("read stays failure_class: %v", err)
	}
	if failureClass != "ignored_decision" {
		t.Fatalf("stays failure_class = %q, want ignored_decision", failureClass)
	}

	// The flipped row stays claimed with the accepted decision: the
	// guarded UPDATE skipped it, so the handoff path still owns it.
	assertWebhookTriggerStatus(t, db, flipClaim.TriggerID, "claimed")
	var flipDecision string
	if err := db.QueryRowContext(ctx, "SELECT decision FROM webhook_refresh_triggers WHERE trigger_id = $1", flipClaim.TriggerID).Scan(&flipDecision); err != nil {
		t.Fatalf("read flip decision: %v", err)
	}
	if flipDecision != string(webhook.DecisionAccepted) {
		t.Fatalf("flip decision = %q, want accepted", flipDecision)
	}
	if err := store.MarkTriggersHandedOff(ctx, []webhook.StoredTrigger{flipClaim}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff(flip) error = %v, want nil: the flipped row must stay on the handoff path", err)
	}
	assertWebhookTriggerStatus(t, db, flipClaim.TriggerID, "handed_off")

	// The stale-fence row is untouched: the guard kept the
	// (trigger_id, fencing token, claimed) fence.
	assertWebhookTriggerStatus(t, db, fenceClaim.TriggerID, "claimed")
	var fenceDecision string
	if err := db.QueryRowContext(ctx, "SELECT decision FROM webhook_refresh_triggers WHERE trigger_id = $1", fenceClaim.TriggerID).Scan(&fenceDecision); err != nil {
		t.Fatalf("read fence decision: %v", err)
	}
	if fenceDecision != string(webhook.DecisionIgnored) {
		t.Fatalf("fence decision = %q, want ignored (row untouched)", fenceDecision)
	}
}
