package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

func TestWebhookTriggerSchemaDefinesDurableDedupeKeys(t *testing.T) {
	t.Parallel()

	schema := WebhookTriggerSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS webhook_refresh_triggers",
		"delivery_key TEXT NOT NULL",
		"refresh_key TEXT NOT NULL",
		"PRIMARY KEY (trigger_id)",
		"failed_at TIMESTAMPTZ NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS webhook_refresh_triggers_refresh_key_idx",
		"CREATE INDEX IF NOT EXISTS webhook_refresh_triggers_status_received_idx",
		"ON webhook_refresh_triggers (status, received_at ASC, trigger_id ASC)",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("WebhookTriggerSchemaSQL() missing %q:\n%s", want, schema)
		}
	}
}

func TestWebhookTriggerStoreStoreTriggerUpsertsAcceptedTrigger(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
	trigger := webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionAccepted,
		DeliveryID:           "delivery-1",
		RepositoryExternalID: "42",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		BeforeSHA:            "1111111111111111111111111111111111111111",
		TargetSHA:            "2222222222222222222222222222222222222222",
		Sender:               "linuxdynasty",
	}
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{webhookTriggerRow(trigger, webhook.TriggerStatusQueued, receivedAt)},
	}
	store := NewWebhookTriggerStore(db)

	stored, err := store.StoreTrigger(context.Background(), trigger, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() error = %v, want nil", err)
	}
	if stored.Status != webhook.TriggerStatusQueued {
		t.Fatalf("Status = %q, want %q", stored.Status, webhook.TriggerStatusQueued)
	}
	if stored.TriggerID == "" || stored.DeliveryKey == "" || stored.RefreshKey == "" {
		t.Fatalf("stored trigger missing durable keys: %#v", stored)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "INSERT INTO webhook_refresh_triggers") {
		t.Fatalf("query missing insert: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "ON CONFLICT (refresh_key) DO UPDATE") {
		t.Fatalf("query missing refresh-key upsert: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "status = CASE") {
		t.Fatalf("query missing ignored-to-queued status transition: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "RETURNING") {
		t.Fatalf("query missing persisted row return: %s", db.queries[0].query)
	}
}

func TestWebhookTriggerStoreStoreTriggerPersistsIgnoredDecision(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
	trigger := webhook.Trigger{
		Provider:             webhook.ProviderGitLab,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionIgnored,
		Reason:               webhook.ReasonTagRef,
		DeliveryID:           "delivery-2",
		RepositoryExternalID: "77",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/tags/v1.0.0",
		TargetSHA:            "3333333333333333333333333333333333333333",
	}
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{webhookTriggerRow(trigger, webhook.TriggerStatusIgnored, receivedAt)},
	}
	store := NewWebhookTriggerStore(db)

	stored, err := store.StoreTrigger(context.Background(), trigger, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() error = %v, want nil", err)
	}
	if stored.Status != webhook.TriggerStatusIgnored {
		t.Fatalf("Status = %q, want %q", stored.Status, webhook.TriggerStatusIgnored)
	}
	if got := db.queries[0].args[16]; got != string(webhook.TriggerStatusIgnored) {
		t.Fatalf("status arg = %v, want ignored", got)
	}
}

func TestWebhookTriggerStoreStoreTriggerReturnsPersistedStatusOnDuplicate(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
	trigger := webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionAccepted,
		DeliveryID:           "delivery-1",
		RepositoryExternalID: "42",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		Ref:                  "refs/heads/main",
		TargetSHA:            "2222222222222222222222222222222222222222",
	}
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{webhookTriggerRow(trigger, webhook.TriggerStatusHandedOff, receivedAt)},
	}
	store := NewWebhookTriggerStore(db)

	stored, err := store.StoreTrigger(context.Background(), trigger, receivedAt)
	if err != nil {
		t.Fatalf("StoreTrigger() error = %v, want nil", err)
	}
	if stored.Status != webhook.TriggerStatusHandedOff {
		t.Fatalf("Status = %q, want persisted handed_off status", stored.Status)
	}
}

func TestWebhookTriggerStoreClaimQueuedTriggersUsesSkipLocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"trigger-1", "delivery-key", "refresh-key",
				string(webhook.ProviderGitHub), string(webhook.EventKindPush),
				string(webhook.DecisionAccepted), "",
				"delivery-1", "42", "eshu-hq/eshu", "main", "refs/heads/main",
				"before", "after", "", "linuxdynasty",
				string(webhook.TriggerStatusClaimed), 0, now, now,
			}}},
		},
	}
	store := NewWebhookTriggerStore(db)

	triggers, err := store.ClaimQueuedTriggers(context.Background(), "collector-git", now, 10)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v, want nil", err)
	}
	if got, want := len(triggers), 1; got != want {
		t.Fatalf("len(triggers) = %d, want %d", got, want)
	}
	if triggers[0].Status != webhook.TriggerStatusClaimed {
		t.Fatalf("Status = %q, want %q", triggers[0].Status, webhook.TriggerStatusClaimed)
	}
	if !strings.Contains(db.queries[0].query, "FOR UPDATE SKIP LOCKED") {
		t.Fatalf("claim query missing SKIP LOCKED: %s", db.queries[0].query)
	}
	if !strings.Contains(WebhookTriggerSchemaSQL(), "ON webhook_refresh_triggers (status, received_at ASC, trigger_id ASC)") {
		t.Fatalf("schema missing claim order index:\n%s", WebhookTriggerSchemaSQL())
	}
	if !strings.Contains(db.queries[0].query, "status = 'queued'") {
		t.Fatalf("claim query missing queued filter: %s", db.queries[0].query)
	}
}

func TestWebhookTriggerStoreMarkTriggersHandedOffRequiresIDs(t *testing.T) {
	t.Parallel()

	store := NewWebhookTriggerStore(&fakeExecQueryer{})
	if err := store.MarkTriggersHandedOff(context.Background(), nil, time.Now()); err == nil {
		t.Fatal("MarkTriggersHandedOff() error = nil, want missing ids error")
	}
}

func TestWebhookTriggerStoreMarkTriggersHandedOffUsesIndividualIDParameters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWebhookTriggerStore(db)
	now := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)

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
	if got, want := db.execs[0].args[0], "trigger-2"; got != want {
		t.Fatalf("arg 0 = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], "trigger-1"; got != want {
		t.Fatalf("arg 1 = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[2], now; got != want {
		t.Fatalf("arg 2 = %v, want %v", got, want)
	}
}

func TestWebhookTriggerStoreMarkTriggersFailedPersistsFailureDetails(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWebhookTriggerStore(db)
	now := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)

	err := store.MarkTriggersFailed(context.Background(), []string{"trigger-1"}, now, "sync_git_failed", "git unavailable")
	if err != nil {
		t.Fatalf("MarkTriggersFailed() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "status = 'failed'") {
		t.Fatalf("query missing failed status: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "failed_at = $4") {
		t.Fatalf("query missing failed_at timestamp: %s", db.execs[0].query)
	}
	if strings.Contains(db.execs[0].query, "ANY($1)") {
		t.Fatalf("query still uses array parameter: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "trigger_id IN ($1)") {
		t.Fatalf("query missing individual id placeholder: %s", db.execs[0].query)
	}
	if got := db.execs[0].args[1]; got != "sync_git_failed" {
		t.Fatalf("failure class arg = %v, want sync_git_failed", got)
	}
}

func webhookTriggerRow(trigger webhook.Trigger, status webhook.TriggerStatus, now time.Time) queueFakeRows {
	return queueFakeRows{rows: [][]any{{
		"trigger-1", "delivery-key", "refresh-key",
		string(trigger.Provider), string(trigger.EventKind),
		string(trigger.Decision), string(trigger.Reason),
		trigger.DeliveryID, trigger.RepositoryExternalID, trigger.RepositoryFullName,
		trigger.DefaultBranch, trigger.Ref, trigger.BeforeSHA, trigger.TargetSHA,
		trigger.Action, trigger.Sender, string(status), 0, now, now,
	}}}
}
