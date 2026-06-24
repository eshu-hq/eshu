// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

func TestIncidentFreshnessSchemaDefinesCoalescingKeys(t *testing.T) {
	t.Parallel()

	schema := IncidentFreshnessSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS incident_freshness_triggers",
		"delivery_key TEXT NOT NULL",
		"freshness_key TEXT NOT NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS incident_freshness_triggers_freshness_key_idx",
		"CREATE INDEX IF NOT EXISTS incident_freshness_triggers_status_received_idx",
		"ON incident_freshness_triggers (status, received_at ASC, trigger_id ASC)",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("IncidentFreshnessSchemaSQL() missing %q:\n%s", want, schema)
		}
	}
}

func TestIncidentFreshnessStoreStoreTriggerUpsertsByFreshnessKey(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC)
	trigger := testIncidentFreshnessTrigger(receivedAt)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{incidentFreshnessTriggerRow(trigger, webhook.TriggerStatusQueued, receivedAt)},
	}
	store := NewIncidentFreshnessStore(db)

	stored, err := store.StoreIncidentFreshnessTrigger(context.Background(), trigger, receivedAt)
	if err != nil {
		t.Fatalf("StoreIncidentFreshnessTrigger() error = %v, want nil", err)
	}
	if stored.Status != webhook.TriggerStatusQueued {
		t.Fatalf("Status = %q, want %q", stored.Status, webhook.TriggerStatusQueued)
	}
	if stored.TriggerID == "" || stored.DeliveryKey == "" || stored.FreshnessKey == "" {
		t.Fatalf("stored trigger missing durable keys: %#v", stored)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	for _, want := range []string{
		"INSERT INTO incident_freshness_triggers",
		"ON CONFLICT (freshness_key) DO UPDATE",
		"WHEN incident_freshness_triggers.status = 'claimed'",
		"duplicate_count = incident_freshness_triggers.duplicate_count + 1",
	} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("query missing %q: %s", want, db.queries[0].query)
		}
	}
}

func TestIncidentFreshnessStoreClaimQueuedTriggersUsesSkipLocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC)
	trigger := testIncidentFreshnessTrigger(now)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{incidentFreshnessTriggerRow(trigger, webhook.TriggerStatusClaimed, now)},
	}
	store := NewIncidentFreshnessStore(db)

	triggers, err := store.ClaimQueuedTriggers(context.Background(), "incident-freshness-handoff", now, 10)
	if err != nil {
		t.Fatalf("ClaimQueuedTriggers() error = %v, want nil", err)
	}
	if got, want := len(triggers), 1; got != want {
		t.Fatalf("len(triggers) = %d, want %d", got, want)
	}
	if triggers[0].Status != webhook.TriggerStatusClaimed {
		t.Fatalf("Status = %q, want %q", triggers[0].Status, webhook.TriggerStatusClaimed)
	}
	for _, want := range []string{"FOR UPDATE SKIP LOCKED", "status = 'queued'"} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("claim query missing %q: %s", want, db.queries[0].query)
		}
	}
}

func testIncidentFreshnessTrigger(observedAt time.Time) webhook.IncidentFreshnessTrigger {
	return webhook.IncidentFreshnessTrigger{
		Provider:   webhook.ProviderPagerDuty,
		EventKind:  "incident.triggered",
		EventID:    "evt-123",
		ScopeID:    "pagerduty:account:example",
		ResourceID: "PINC123",
		ObservedAt: observedAt,
	}
}

func incidentFreshnessTriggerRow(
	trigger webhook.IncidentFreshnessTrigger,
	status webhook.TriggerStatus,
	now time.Time,
) queueFakeRows {
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(trigger, now)
	if err != nil {
		panic(err)
	}
	return queueFakeRows{rows: [][]any{{
		stored.TriggerID,
		stored.DeliveryKey,
		stored.FreshnessKey,
		string(trigger.Provider),
		trigger.EventKind,
		trigger.EventID,
		trigger.ScopeID,
		trigger.ResourceID,
		string(status),
		0,
		stored.ObservedAt,
		now,
		now,
	}}}
}
