// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incidentfreshnessstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// TestIncidentFreshnessClaimAssignsLeaseAndBumpsFencingToken is the #9379
// RED test: claiming queued incident freshness triggers must stamp a
// claim_expires_at lease (claimedAt+leaseDuration) and bump
// claim_fencing_token, mirroring the AWS/GCP freshness lease pattern, so a
// crashed claimant's rows can be reaped and a stale holder fenced.
func TestIncidentFreshnessClaimAssignsLeaseAndBumpsFencingToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	trigger := testIncidentFreshnessTrigger(now)
	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{incidentFreshnessClaimRow(trigger, now, 7)},
	}
	store := NewIncidentFreshnessStore(db)

	claimed, err := store.ClaimQueuedTriggers(context.Background(), "incident-freshness-handoff", now, 10, 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v, want nil", err)
	}
	if got, want := len(claimed), 1; got != want {
		t.Fatalf("len(claimed) = %d, want %d", got, want)
	}
	if got, want := claimed[0].ClaimFencingToken, int64(7); got != want {
		t.Fatalf("ClaimFencingToken = %d, want %d", got, want)
	}
	if got, want := len(db.Queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.Queries[0].Query
	for _, want := range []string{
		"claim_expires_at",
		"claim_fencing_token",
		"claim_fencing_token + 1",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing %q: %s", want, query)
		}
	}
	// Args are (limit, owner, claimed_at, claim_expires_at): the lease must
	// be claimedAt+leaseDuration.
	if got, want := len(db.Queries[0].Args), 4; got != want {
		t.Fatalf("claim arg count = %d, want %d", got, want)
	}
	expiresAt, ok := db.Queries[0].Args[3].(time.Time)
	if !ok {
		t.Fatalf("claim arg[3] = %#v, want time.Time lease expiry", db.Queries[0].Args[3])
	}
	if want := now.Add(5 * time.Minute); !expiresAt.Equal(want) {
		t.Fatalf("claim lease expiry = %v, want %v", expiresAt, want)
	}
}

// TestIncidentFreshnessReapExpiredTriggerClaims is the #9379 RED test: the
// store must reclaim 'claimed' rows whose lease expired back to 'queued'
// while leaving live-lease rows untouched.
func TestIncidentFreshnessReapExpiredTriggerClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	trigger := testIncidentFreshnessTrigger(now)
	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{incidentFreshnessClaimRow(trigger, now, 3)},
	}
	store := NewIncidentFreshnessStore(db)

	reclaimed, err := store.ReapExpiredTriggerClaims(context.Background(), now, 50)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v, want nil", err)
	}
	if got, want := len(reclaimed), 1; got != want {
		t.Fatalf("len(reclaimed) = %d, want %d", got, want)
	}
	if got, want := len(db.Queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.Queries[0].Query
	for _, want := range []string{
		"status = 'claimed'",
		"claim_expires_at",
		"status = 'queued'",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reap query missing %q: %s", want, query)
		}
	}
}

// TestIncidentFreshnessCompletionIsFencedByToken is the #9379 RED test:
// MarkTriggersHandedOff/MarkTriggersFailed must present each trigger's
// fencing token and only complete rows whose current token still matches,
// so a stale holder whose lease was reaped and re-claimed cannot complete a
// claim it no longer holds.
func TestIncidentFreshnessCompletionIsFencedByToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	trigger := testIncidentFreshnessTrigger(now)
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(trigger, now)
	if err != nil {
		t.Fatalf("NewStoredIncidentFreshnessTrigger() error = %v", err)
	}
	stored.Status = webhook.TriggerStatusClaimed
	stored.ClaimFencingToken = 4

	db := &fake.ExecQueryer{}
	store := NewIncidentFreshnessStore(db)

	if err := store.MarkTriggersHandedOff(context.Background(), []webhook.StoredIncidentFreshnessTrigger{stored}, now); err != nil {
		t.Fatalf("MarkTriggersHandedOff() error = %v, want nil", err)
	}
	if err := store.MarkTriggersFailed(context.Background(), []webhook.StoredIncidentFreshnessTrigger{stored}, now, "handoff_failed", "boom"); err != nil {
		t.Fatalf("MarkTriggersFailed() error = %v, want nil", err)
	}
	if got, want := len(db.Execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	for i, exec := range db.Execs {
		for _, want := range []string{
			"claim_fencing_token",
			"fenced(trigger_id, fencing_token)",
		} {
			if !strings.Contains(exec.Query, want) {
				t.Fatalf("completion query %d missing %q: %s", i, want, exec.Query)
			}
		}
	}
	// The fenced pair must carry the presented token.
	found := false
	for _, arg := range db.Execs[0].Args {
		if token, ok := arg.(int64); ok && token == 4 {
			found = true
		}
	}
	if !found {
		t.Fatalf("handed-off args missing fencing token 4: %#v", db.Execs[0].Args)
	}
}

// incidentFreshnessClaimRow stages one claimed incident freshness row whose
// shape ends in claim_fencing_token, matching the post-#9379 claim/reap
// RETURNING lists.
func incidentFreshnessClaimRow(
	trigger webhook.IncidentFreshnessTrigger,
	now time.Time,
	token int64,
) fake.Rows {
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(trigger, now)
	if err != nil {
		panic(err)
	}
	return fake.Rows{Data: [][]any{{
		stored.TriggerID,
		stored.DeliveryKey,
		stored.FreshnessKey,
		string(trigger.Provider),
		trigger.EventKind,
		trigger.EventID,
		trigger.ScopeID,
		trigger.ResourceID,
		string(webhook.TriggerStatusClaimed),
		0,
		stored.ObservedAt,
		now,
		now,
		token,
	}}}
}
