// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
)

func TestGCPFreshnessSchemaDefinesCoalescingKeys(t *testing.T) {
	t.Parallel()

	schema := GCPFreshnessSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS gcp_freshness_triggers",
		"delivery_key TEXT NOT NULL",
		"freshness_key TEXT NOT NULL",
		"parent_scope_kind TEXT NOT NULL",
		"parent_scope_id TEXT NOT NULL",
		"asset_type TEXT NOT NULL DEFAULT ''",
		"location TEXT NOT NULL DEFAULT ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS gcp_freshness_triggers_freshness_key_idx",
		"CREATE INDEX IF NOT EXISTS gcp_freshness_triggers_status_received_idx",
		"ON gcp_freshness_triggers (status, received_at ASC, trigger_id ASC)",
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("GCPFreshnessSchemaSQL() missing %q:\n%s", want, schema)
		}
	}
}

func TestGCPFreshnessStoreStoreTriggerUpsertsByFreshnessKey(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	trigger := testGCPFreshnessTrigger(receivedAt)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{gcpFreshnessTriggerRow(trigger, freshness.TriggerStatusQueued, receivedAt)},
	}
	store := NewGCPFreshnessStore(db)

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
	if !strings.Contains(db.queries[0].query, "INSERT INTO gcp_freshness_triggers") {
		t.Fatalf("query missing insert: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "ON CONFLICT (freshness_key) DO UPDATE") {
		t.Fatalf("query missing freshness-key upsert: %s", db.queries[0].query)
	}
	for _, want := range []string{
		"trigger_id = CASE",
		"status = CASE",
		"WHEN gcp_freshness_triggers.status = 'claimed'",
		"claimed_by = CASE",
		"claimed_at = CASE",
		"failed_at = CASE",
		"failure_class = CASE",
	} {
		if !strings.Contains(db.queries[0].query, want) {
			t.Fatalf("query missing claim-safe upsert fragment %q: %s", want, db.queries[0].query)
		}
	}
}

func TestGCPFreshnessStoreClaimQueuedTriggersUsesSkipLocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	trigger := testGCPFreshnessTrigger(now)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{gcpFreshnessTriggerRow(trigger, freshness.TriggerStatusClaimed, now)},
	}
	store := NewGCPFreshnessStore(db)

	triggers, err := store.ClaimQueuedTriggers(context.Background(), "gcp-freshness-handoff", now, 10)
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

func TestGCPFreshnessStoreMarkTriggersHandedOffUsesIndividualIDParameters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewGCPFreshnessStore(db)
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

func TestGCPFreshnessStoreMarkTriggersFailedRequiresFailureClass(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewGCPFreshnessStore(db)
	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)

	err := store.MarkTriggersFailed(context.Background(), []string{"trigger-1"}, now, "", "boom")
	if err == nil {
		t.Fatal("MarkTriggersFailed() error = nil, want non-nil for empty failure class")
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0", len(db.execs))
	}
}

func testGCPFreshnessTrigger(observedAt time.Time) freshness.Trigger {
	return freshness.Trigger{
		EventID:         "evt-123",
		Kind:            freshness.EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "demo-project",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      observedAt,
	}
}

func gcpFreshnessTriggerRow(
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
		string(trigger.ParentScopeKind),
		trigger.ParentScopeID,
		trigger.AssetType,
		trigger.Location,
		string(status),
		0,
		trigger.ObservedAt,
		now,
		now,
	}}}
}
