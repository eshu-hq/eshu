// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"
)

func TestExtensionOutcomePartialHealthClearsOnCleanCompletion(t *testing.T) {
	instance := InstanceSummary{
		InstanceID: "aws-extension", CollectorKind: "aws", Enabled: true,
		ClaimsEnabled: true, ExtensionHosted: true,
	}
	outcome := ExtensionOutcome{
		CollectorKind: "aws", InstanceID: instance.InstanceID,
		ScopeCount: 2, PartialScopes: 1, WarningCount: 1,
		UpdatedAt:       time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC),
		LastCommittedAt: time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC),
	}
	rows := RuntimeStatuses(Evidence{Instances: []InstanceSummary{instance}, ExtensionOutcomes: []ExtensionOutcome{outcome}})
	if len(rows) != 1 || rows[0].Health != "partial" {
		t.Fatalf("partial outcome health = %#v, want one partial row", rows)
	}
	outcome.PartialScopes = 0
	outcome.WarningCount = 0
	rows = RuntimeStatuses(Evidence{Instances: []InstanceSummary{instance}, ExtensionOutcomes: []ExtensionOutcome{outcome}})
	if len(rows) != 1 || rows[0].Health != "observed" {
		t.Fatalf("recovered outcome health = %#v, want one observed row", rows)
	}
	outcome.InFlightScopes = 1
	rows = RuntimeStatuses(Evidence{Instances: []InstanceSummary{instance}, ExtensionOutcomes: []ExtensionOutcome{outcome}})
	if len(rows) != 1 || rows[0].Health != "partial" {
		t.Fatalf("in-flight outcome health = %#v, want one partial row", rows)
	}
	outcome.InFlightScopes = 0
	instance.Enabled = false
	rows = RuntimeStatuses(Evidence{Instances: []InstanceSummary{instance}, ExtensionOutcomes: []ExtensionOutcome{outcome}})
	if len(rows) != 1 || rows[0].Health != "disabled" {
		t.Fatalf("disabled instance health = %#v, want disabled", rows)
	}
	instance.Enabled = true
	instance.DeactivatedAt = outcome.UpdatedAt
	rows = RuntimeStatuses(Evidence{Instances: []InstanceSummary{instance}, ExtensionOutcomes: []ExtensionOutcome{outcome}})
	if len(rows) != 1 || rows[0].Health != "disabled" {
		t.Fatalf("deactivated instance health = %#v, want disabled", rows)
	}
}

func TestWithheldExtensionOutcomeDoesNotAdvanceObservationTime(t *testing.T) {
	committed := time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC)
	attempted := committed.Add(time.Hour)
	instance := InstanceSummary{
		InstanceID: "aws-extension", CollectorKind: "aws", Enabled: true,
		ClaimsEnabled: true, ExtensionHosted: true,
	}
	outcome := ExtensionOutcome{
		CollectorKind: "aws", InstanceID: instance.InstanceID,
		ScopeCount: 1, PartialScopes: 1, WarningCount: 3,
		LastCommittedAt: committed, UpdatedAt: attempted,
	}
	rows := RuntimeStatuses(Evidence{Instances: []InstanceSummary{instance}, ExtensionOutcomes: []ExtensionOutcome{outcome}})
	if len(rows) != 1 || rows[0].Health != "partial" || !rows[0].LastObservedAt.Equal(committed) ||
		!rows[0].UpdatedAt.Equal(attempted) {
		t.Fatalf("withheld status = %+v, want prior observation and later partial update", rows)
	}
	outcome.LastCommittedAt = time.Time{}
	rows = RuntimeStatuses(Evidence{Instances: []InstanceSummary{instance}, ExtensionOutcomes: []ExtensionOutcome{outcome}})
	if len(rows) != 1 || !rows[0].LastObservedAt.IsZero() || !rows[0].UpdatedAt.Equal(attempted) {
		t.Fatalf("first withheld status = %+v, want no successful observation", rows)
	}
}
