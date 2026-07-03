// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// EventKind identifies the normalized GCP Cloud Asset Inventory (CAI) signal
// family.
type EventKind string

const (
	// EventKindAssetChange represents a CAI asset create or update signal.
	EventKindAssetChange EventKind = "asset_change"
	// EventKindAssetDeleted represents a CAI asset deletion signal.
	EventKindAssetDeleted EventKind = "asset_deleted"
)

// TriggerStatus describes durable GCP freshness trigger handoff state.
type TriggerStatus string

const (
	// TriggerStatusQueued means the trigger is waiting for workflow handoff.
	TriggerStatusQueued TriggerStatus = "queued"
	// TriggerStatusClaimed means a handoff actor owns the trigger row.
	TriggerStatusClaimed TriggerStatus = "claimed"
	// TriggerStatusHandedOff means workflow work items were created.
	TriggerStatusHandedOff TriggerStatus = "handed_off"
	// TriggerStatusFailed means handoff failed and needs operator attention.
	TriggerStatusFailed TriggerStatus = "failed"
)

// Trigger is one normalized GCP freshness event targeted at a scan tuple.
type Trigger struct {
	EventID         string
	Kind            EventKind
	ParentScopeKind gcpcloud.ParentScopeKind
	ParentScopeID   string
	AssetType       string
	Location        string
	ObservedAt      time.Time
}

// StoredTrigger is the durable coalesced form of a Trigger.
type StoredTrigger struct {
	Trigger
	TriggerID      string
	DeliveryKey    string
	FreshnessKey   string
	Status         TriggerStatus
	DuplicateCount int
	ReceivedAt     time.Time
	UpdatedAt      time.Time
}

// Target is the GCP collector claim target derived from a trigger.
type Target struct {
	ParentScopeKind gcpcloud.ParentScopeKind
	ParentScopeID   string
	AssetType       string
	Location        string
}

// Validate checks that a trigger is bounded to a supported GCP scan tuple.
func (t Trigger) Validate() error {
	t = t.normalized()
	if t.EventID == "" {
		return fmt.Errorf("gcp freshness trigger requires event_id")
	}
	switch t.Kind {
	case EventKindAssetChange, EventKindAssetDeleted:
	default:
		return fmt.Errorf("unsupported gcp freshness event kind %q", t.Kind)
	}
	if !t.ParentScopeKind.Valid() {
		return fmt.Errorf("unsupported parent_scope_kind %q", t.ParentScopeKind)
	}
	if t.ParentScopeID == "" {
		return fmt.Errorf("parent_scope_id is required")
	}
	if strings.Contains(t.ParentScopeID, "*") {
		return fmt.Errorf("parent_scope_id must not contain wildcard")
	}
	if t.AssetType == "" {
		return fmt.Errorf("asset_type is required")
	}
	if strings.Contains(t.AssetType, "*") {
		return fmt.Errorf("asset_type must not contain wildcard")
	}
	if strings.Contains(t.Location, "*") {
		return fmt.Errorf("location must not contain wildcard")
	}
	if t.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at must not be zero")
	}
	return nil
}

// Target returns the GCP collector claim target for this trigger.
func (t Trigger) Target() Target {
	t = t.normalized()
	return Target{
		ParentScopeKind: t.ParentScopeKind,
		ParentScopeID:   t.ParentScopeID,
		AssetType:       t.AssetType,
		Location:        t.Location,
	}
}

// NewStoredTrigger builds durable keys for a validated freshness trigger.
func NewStoredTrigger(trigger Trigger, receivedAt time.Time) (StoredTrigger, error) {
	if receivedAt.IsZero() {
		return StoredTrigger{}, fmt.Errorf("received_at must not be zero")
	}
	trigger = trigger.normalized()
	if err := trigger.Validate(); err != nil {
		return StoredTrigger{}, err
	}
	deliveryKey := trigger.deliveryKey()
	freshnessKey := trigger.Target().FreshnessKey()
	return StoredTrigger{
		Trigger:      trigger,
		TriggerID:    facts.StableID("GCPFreshnessTrigger", map[string]any{"freshness_key": freshnessKey}),
		DeliveryKey:  deliveryKey,
		FreshnessKey: freshnessKey,
		Status:       TriggerStatusQueued,
		ReceivedAt:   receivedAt.UTC(),
		UpdatedAt:    receivedAt.UTC(),
	}, nil
}

// FreshnessKey returns the coalescing identity for targeted GCP refresh.
func (t Target) FreshnessKey() string {
	return strings.Join([]string{
		string(t.ParentScopeKind),
		strings.TrimSpace(t.ParentScopeID),
		strings.TrimSpace(t.AssetType),
		strings.TrimSpace(t.Location),
	}, ":")
}

// ScopeID returns the GCP collector scope for the target tuple.
func (t Target) ScopeID() string {
	return "gcp:" + t.FreshnessKey()
}

func (t Trigger) normalized() Trigger {
	t.EventID = strings.TrimSpace(t.EventID)
	t.Kind = EventKind(strings.TrimSpace(string(t.Kind)))
	t.ParentScopeKind = gcpcloud.ParentScopeKind(strings.TrimSpace(string(t.ParentScopeKind)))
	t.ParentScopeID = strings.TrimSpace(t.ParentScopeID)
	t.AssetType = strings.TrimSpace(t.AssetType)
	t.Location = strings.TrimSpace(t.Location)
	t.ObservedAt = t.ObservedAt.UTC()
	return t
}

func (t Trigger) deliveryKey() string {
	return strings.Join([]string{
		string(t.Kind),
		t.EventID,
		string(t.ParentScopeKind),
		t.ParentScopeID,
	}, ":")
}
