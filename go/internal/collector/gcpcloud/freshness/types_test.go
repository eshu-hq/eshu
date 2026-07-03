// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

func TestTriggerValidateAcceptsBoundedTarget(t *testing.T) {
	t.Parallel()

	trigger := Trigger{
		EventID:         "evt-123",
		Kind:            EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "demo-project",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
	}

	if err := trigger.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if got, want := trigger.Target().ScopeID(), "gcp:project:demo-project:compute.googleapis.com/Instance:us-central1-a"; got != want {
		t.Fatalf("ScopeID() = %q, want %q", got, want)
	}
}

func TestTriggerValidateRejectsWildcardAndUnknownScope(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		trigger Trigger
		want    string
	}{
		{
			name: "wildcard parent scope id",
			trigger: Trigger{
				EventID:         "evt-1",
				Kind:            EventKindAssetChange,
				ParentScopeKind: gcpcloud.ParentScopeProject,
				ParentScopeID:   "*",
				AssetType:       "compute.googleapis.com/Instance",
				ObservedAt:      observedAt,
			},
			want: "parent_scope_id must not contain wildcard",
		},
		{
			name: "unknown parent scope kind",
			trigger: Trigger{
				EventID:         "evt-2",
				Kind:            EventKindAssetChange,
				ParentScopeKind: gcpcloud.ParentScopeKind("unknown"),
				ParentScopeID:   "demo-project",
				AssetType:       "compute.googleapis.com/Instance",
				ObservedAt:      observedAt,
			},
			want: "unsupported parent_scope_kind",
		},
		{
			name: "missing event id",
			trigger: Trigger{
				Kind:            EventKindAssetChange,
				ParentScopeKind: gcpcloud.ParentScopeProject,
				ParentScopeID:   "demo-project",
				AssetType:       "compute.googleapis.com/Instance",
				ObservedAt:      observedAt,
			},
			want: "requires event_id",
		},
		{
			name: "missing asset type",
			trigger: Trigger{
				EventID:         "evt-3",
				Kind:            EventKindAssetChange,
				ParentScopeKind: gcpcloud.ParentScopeProject,
				ParentScopeID:   "demo-project",
				ObservedAt:      observedAt,
			},
			want: "asset_type is required",
		},
		{
			name: "zero observed at",
			trigger: Trigger{
				EventID:         "evt-4",
				Kind:            EventKindAssetChange,
				ParentScopeKind: gcpcloud.ParentScopeProject,
				ParentScopeID:   "demo-project",
				AssetType:       "compute.googleapis.com/Instance",
			},
			want: "observed_at must not be zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.trigger.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("Validate() error = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestStoredTriggerKeysCoalesceByTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	first := Trigger{
		EventID:         "evt-1",
		Kind:            EventKindAssetChange,
		ParentScopeKind: gcpcloud.ParentScopeProject,
		ParentScopeID:   "demo-project",
		AssetType:       "compute.googleapis.com/Instance",
		Location:        "us-central1-a",
		ObservedAt:      observedAt,
	}
	second := first
	second.EventID = "evt-2"

	firstStored, err := NewStoredTrigger(first, observedAt)
	if err != nil {
		t.Fatalf("NewStoredTrigger(first) error = %v, want nil", err)
	}
	secondStored, err := NewStoredTrigger(second, observedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("NewStoredTrigger(second) error = %v, want nil", err)
	}
	if firstStored.FreshnessKey != secondStored.FreshnessKey {
		t.Fatalf("FreshnessKey differs for same target: %q vs %q", firstStored.FreshnessKey, secondStored.FreshnessKey)
	}
	if firstStored.DeliveryKey == secondStored.DeliveryKey {
		t.Fatalf("DeliveryKey = %q, want per-event delivery keys to differ", firstStored.DeliveryKey)
	}
}
