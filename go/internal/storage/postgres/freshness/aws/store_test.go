// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsfreshnessstore

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
)

func TestAWSFreshnessSchemaDefinesCoalescingKeys(t *testing.T) {
	t.Parallel()

	schema := AWSFreshnessSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS aws_freshness_triggers",
		"delivery_key TEXT NOT NULL",
		"freshness_key TEXT NOT NULL",
		"claim_expires_at TIMESTAMPTZ NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS aws_freshness_triggers_freshness_key_idx",
		"CREATE INDEX IF NOT EXISTS aws_freshness_triggers_status_received_idx",
		"ON aws_freshness_triggers (status, received_at ASC, trigger_id ASC)",
		"CREATE INDEX IF NOT EXISTS aws_freshness_triggers_claimed_lease_idx",
		"ON aws_freshness_triggers (claim_expires_at)",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("AWSFreshnessSchemaSQL() missing %q:\n%s", want, schema)
		}
	}
}

func TestAWSFreshnessStoreStoreTriggerUpsertsByFreshnessKey(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	trigger := testAWSFreshnessTrigger(receivedAt)
	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{awsFreshnessTriggerRow(trigger, freshness.TriggerStatusQueued, receivedAt)},
	}
	store := NewAWSFreshnessStore(db)

	stored, err := store.StoreTrigger(context.Background(), trigger, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() error = %v, want nil", err)
	}
	if stored.Status != freshness.TriggerStatusQueued {
		t.Fatalf("Status = %q, want %q", stored.Status, freshness.TriggerStatusQueued)
	}
	if stored.TriggerID == "" || stored.DeliveryKey == "" || stored.FreshnessKey == "" {
		t.Fatalf("stored trigger missing durable keys: %#v", stored)
	}
	if got, want := len(db.Queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.Queries[0].Query, "INSERT INTO aws_freshness_triggers") {
		t.Fatalf("query missing insert: %s", db.Queries[0].Query)
	}
	if !strings.Contains(db.Queries[0].Query, "ON CONFLICT (freshness_key) DO UPDATE") {
		t.Fatalf("query missing freshness-key upsert: %s", db.Queries[0].Query)
	}
	for _, want := range []string{
		"trigger_id = CASE",
		"status = CASE",
		"WHEN aws_freshness_triggers.status = 'claimed'",
		"claimed_by = CASE",
		"claimed_at = CASE",
		"failed_at = CASE",
		"failure_class = CASE",
	} {
		if !strings.Contains(db.Queries[0].Query, want) {
			t.Fatalf("query missing claim-safe upsert fragment %q: %s", want, db.Queries[0].Query)
		}
	}
}

func TestAWSFreshnessStoreClaimQueuedTriggersUsesSkipLocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	trigger := testAWSFreshnessTrigger(now)
	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{awsFreshnessTriggerRow(trigger, freshness.TriggerStatusClaimed, now)},
	}
	store := NewAWSFreshnessStore(db)

	triggers, err := store.ClaimQueuedTriggers(context.Background(), "aws-freshness-handoff", now, 10, 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v, want nil", err)
	}
	if got, want := len(triggers), 1; got != want {
		t.Fatalf("len(triggers) = %d, want %d", got, want)
	}
	if triggers[0].Status != freshness.TriggerStatusClaimed {
		t.Fatalf("Status = %q, want %q", triggers[0].Status, freshness.TriggerStatusClaimed)
	}
	if !strings.Contains(db.Queries[0].Query, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("claim query missing SKIP LOCKED: %s", db.Queries[0].Query)
	}
	if !strings.Contains(db.Queries[0].Query, "status = 'queued'") {
		t.Fatalf("claim query missing queued filter: %s", db.Queries[0].Query)
	}
	if !strings.Contains(db.Queries[0].Query, "claim_expires_at = $4") {
		t.Fatalf("claim query missing claim_expires_at lease assignment: %s", db.Queries[0].Query)
	}
	wantLeaseExpiry := now.Add(5 * time.Minute)
	if got := db.Queries[0].Args[3]; got != wantLeaseExpiry {
		t.Fatalf("claim_expires_at arg = %v, want %v", got, wantLeaseExpiry)
	}
}

func TestAWSFreshnessStoreClaimQueuedTriggersRequiresPositiveLease(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	db := &fake.ExecQueryer{}
	store := NewAWSFreshnessStore(db)

	if _, err := store.ClaimQueuedTriggers(context.Background(), "aws-freshness-handoff", now, 10, 0); err == nil {
		t.Fatal("ClaimQueuedTriggers() error = nil, want non-nil for zero lease duration")
	}
	if len(db.Queries) != 0 {
		t.Fatalf("query count = %d, want 0 (should fail before issuing query)", len(db.Queries))
	}
}

func TestAWSFreshnessStoreReapExpiredTriggerClaimsUsesSkipLockedAndLeaseExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 10, 30, 0, 0, time.UTC)
	trigger := testAWSFreshnessTrigger(now)
	db := &fake.ExecQueryer{
		QueryResponses: []fake.Rows{awsFreshnessTriggerRow(trigger, freshness.TriggerStatusQueued, now)},
	}
	store := NewAWSFreshnessStore(db)

	reclaimed, err := store.ReapExpiredTriggerClaims(context.Background(), now, 50)
	if err != nil {
		t.Fatalf("ReapExpiredTriggerClaims() error = %v, want nil", err)
	}
	if got, want := len(reclaimed), 1; got != want {
		t.Fatalf("len(reclaimed) = %d, want %d", got, want)
	}
	if reclaimed[0].Status != freshness.TriggerStatusQueued {
		t.Fatalf("reclaimed Status = %q, want %q", reclaimed[0].Status, freshness.TriggerStatusQueued)
	}
	query := db.Queries[0].Query
	for _, want := range []string{
		"FOR UPDATE SKIP LOCKED",
		"status = 'claimed'",
		"claim_expires_at IS NOT NULL",
		"claim_expires_at < $1",
		"SET status = 'queued'",
		"claimed_by = NULL",
		"claimed_at = NULL",
		"claim_expires_at = NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reap query missing %q: %s", want, query)
		}
	}
}

func TestAWSFreshnessStoreReapExpiredTriggerClaimsRequiresPositiveLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 10, 30, 0, 0, time.UTC)
	db := &fake.ExecQueryer{}
	store := NewAWSFreshnessStore(db)

	if _, err := store.ReapExpiredTriggerClaims(context.Background(), now, 0); err == nil {
		t.Fatal("ReapExpiredTriggerClaims() error = nil, want non-nil for zero limit")
	}
	if len(db.Queries) != 0 {
		t.Fatalf("query count = %d, want 0 (should fail before issuing query)", len(db.Queries))
	}
}

func TestAWSFreshnessStoreMarkTriggersHandedOffUsesIndividualIDParameters(t *testing.T) {
	t.Parallel()

	db := &fake.ExecQueryer{}
	store := NewAWSFreshnessStore(db)
	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)

	err := store.MarkTriggersHandedOff(context.Background(), []freshness.StoredTrigger{
		{TriggerID: "trigger-2", ClaimFencingToken: 7},
		{TriggerID: "trigger-1", ClaimFencingToken: 3},
		{TriggerID: "trigger-2", ClaimFencingToken: 7},
	}, now)
	if err != nil {
		t.Fatalf("MarkTriggersHandedOff() error = %v, want nil", err)
	}
	if got, want := len(db.Execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if strings.Contains(db.Execs[0].Query, "ANY($1)") {
		t.Fatalf("query still uses array parameter: %s", db.Execs[0].Query)
	}
	if !strings.Contains(db.Execs[0].Query, "VALUES ($1, $2::bigint), ($3, $4::bigint)") {
		t.Fatalf("query missing fenced (trigger_id, fencing_token) VALUES pairs (dedup should drop the repeated trigger-2): %s", db.Execs[0].Query)
	}
	if !strings.Contains(db.Execs[0].Query, "trigger.claim_fencing_token = fenced.fencing_token") {
		t.Fatalf("query missing claim_fencing_token fencing predicate: %s", db.Execs[0].Query)
	}
	wantArgs := []any{"trigger-2", int64(7), "trigger-1", int64(3), now.UTC()}
	if got := db.Execs[0].Args; len(got) != len(wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", got, wantArgs)
	} else {
		for i := range wantArgs {
			if got[i] != wantArgs[i] {
				t.Fatalf("exec args[%d] = %#v, want %#v", i, got[i], wantArgs[i])
			}
		}
	}
}

func testAWSFreshnessTrigger(observedAt time.Time) freshness.Trigger {
	return freshness.Trigger{
		EventID:      "evt-123",
		Kind:         freshness.EventKindConfigChange,
		AccountID:    "123456789012",
		Region:       "us-east-1",
		ServiceKind:  awscloud.ServiceLambda,
		ResourceType: awscloud.ResourceTypeLambdaFunction,
		ResourceID:   "function-1",
		ObservedAt:   observedAt,
	}
}

func awsFreshnessTriggerRow(
	trigger freshness.Trigger,
	status freshness.TriggerStatus,
	now time.Time,
) fake.Rows {
	return awsFreshnessTriggerRowWithFencingToken(trigger, status, now, 0)
}

// awsFreshnessTriggerRowWithFencingToken is awsFreshnessTriggerRow with an
// explicit trailing claim_fencing_token value, for tests that need to prove
// ClaimQueuedTriggers surfaces the token a caller must fence completion on
// (#4576).
func awsFreshnessTriggerRowWithFencingToken(
	trigger freshness.Trigger,
	status freshness.TriggerStatus,
	now time.Time,
	fencingToken int64,
) fake.Rows {
	stored, err := freshness.NewStoredTrigger(trigger, now)
	if err != nil {
		panic(err)
	}
	return fake.Rows{Data: [][]any{{
		stored.TriggerID,
		stored.DeliveryKey,
		stored.FreshnessKey,
		string(trigger.Kind),
		trigger.EventID,
		trigger.AccountID,
		trigger.Region,
		trigger.ServiceKind,
		trigger.ResourceType,
		trigger.ResourceID,
		string(status),
		0,
		trigger.ObservedAt,
		now,
		now,
		fencingToken,
	}}}
}
