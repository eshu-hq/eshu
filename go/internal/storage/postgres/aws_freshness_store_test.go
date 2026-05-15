package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
)

func TestAWSFreshnessSchemaDefinesCoalescingKeys(t *testing.T) {
	t.Parallel()

	schema := AWSFreshnessSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS aws_freshness_triggers",
		"delivery_key TEXT NOT NULL",
		"freshness_key TEXT NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS aws_freshness_triggers_freshness_key_idx",
		"CREATE INDEX IF NOT EXISTS aws_freshness_triggers_status_received_idx",
		"ON aws_freshness_triggers (status, received_at ASC, trigger_id ASC)",
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
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{awsFreshnessTriggerRow(trigger, freshness.TriggerStatusQueued, receivedAt)},
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
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "INSERT INTO aws_freshness_triggers") {
		t.Fatalf("query missing insert: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "ON CONFLICT (freshness_key) DO UPDATE") {
		t.Fatalf("query missing freshness-key upsert: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "status = EXCLUDED.status") {
		t.Fatalf("query must requeue a new event for an already handed-off target: %s", db.queries[0].query)
	}
}

func TestAWSFreshnessStoreClaimQueuedTriggersUsesSkipLocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	trigger := testAWSFreshnessTrigger(now)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{awsFreshnessTriggerRow(trigger, freshness.TriggerStatusClaimed, now)},
	}
	store := NewAWSFreshnessStore(db)

	triggers, err := store.ClaimQueuedTriggers(context.Background(), "aws-freshness-handoff", now, 10)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v, want nil", err)
	}
	if got, want := len(triggers), 1; got != want {
		t.Fatalf("len(triggers) = %d, want %d", got, want)
	}
	if triggers[0].Status != freshness.TriggerStatusClaimed {
		t.Fatalf("Status = %q, want %q", triggers[0].Status, freshness.TriggerStatusClaimed)
	}
	if !strings.Contains(db.queries[0].query, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("claim query missing SKIP LOCKED: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "status = 'queued'") {
		t.Fatalf("claim query missing queued filter: %s", db.queries[0].query)
	}
}

func TestAWSFreshnessStoreMarkTriggersHandedOffUsesIndividualIDParameters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewAWSFreshnessStore(db)
	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)

	err := store.MarkTriggersHandedOff(context.Background(), []string{"trigger-2", "trigger-1", "trigger-2"}, now)
	if err != nil {
		t.Fatalf("MarkTriggersHandedOff() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if strings.Contains(db.execs[0].query, "ANY($1)") {
		t.Fatalf("query still uses array parameter: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "trigger_id IN ($1, $2)") {
		t.Fatalf("query missing individual id placeholders: %s", db.execs[0].query)
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
) queueFakeRows {
	stored, err := freshness.NewStoredTrigger(trigger, now)
	if err != nil {
		panic(err)
	}
	return queueFakeRows{rows: [][]any{{
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
	}}}
}
