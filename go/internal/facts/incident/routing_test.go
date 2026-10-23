// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import "testing"

func TestIncidentRoutingFactKindsAndSchemaVersions(t *testing.T) {
	want := []string{
		IncidentRoutingAppliedPagerDutyResourceFactKind,
		IncidentRoutingAppliedAlertRouteFactKind,
		IncidentRoutingObservedPagerDutyServiceFactKind,
		IncidentRoutingObservedPagerDutyIntegrationFactKind,
		IncidentRoutingCoverageWarningFactKind,
	}

	got := IncidentRoutingFactKinds()
	if len(got) != len(want) {
		t.Fatalf("IncidentRoutingFactKinds len = %d, want %d: %#v", len(got), len(want), got)
	}
	for index, kind := range want {
		if got[index] != kind {
			t.Fatalf("IncidentRoutingFactKinds[%d] = %q, want %q", index, got[index], kind)
		}
		version, ok := IncidentRoutingSchemaVersion(kind)
		if !ok || version != IncidentRoutingSchemaVersionV1 {
			t.Fatalf("IncidentRoutingSchemaVersion(%q) = %q, %v; want %q, true", kind, version, ok, IncidentRoutingSchemaVersionV1)
		}
	}
	if version, ok := IncidentRoutingSchemaVersion("incident_routing.unknown"); ok || version != "" {
		t.Fatalf("IncidentRoutingSchemaVersion(unknown) = %q, %v; want empty false", version, ok)
	}

	got[0] = "mutated"
	if IncidentRoutingFactKinds()[0] != IncidentRoutingAppliedPagerDutyResourceFactKind {
		t.Fatal("IncidentRoutingFactKinds returned mutable backing slice")
	}
}

// TestIncidentRoutingCoverageWarningProducers pins the producer declaration to
// what the collectors emit: the PagerDuty collector emits config-validation
// coverage warnings and the Terraform-state collector emits applied-state
// coverage warnings, so an external producer grant for either must be able to
// cover the kind (#8911).
func TestIncidentRoutingCoverageWarningProducers(t *testing.T) {
	var got []string
	for _, producer := range IncidentRoutingFamilyDeclaration.Producers {
		for _, kind := range producer.Kinds {
			if kind == IncidentRoutingCoverageWarningFactKind {
				got = append(got, producer.ID)
			}
		}
	}
	want := []string{"collector-pagerduty", "collector-terraform-state"}
	if len(got) != len(want) {
		t.Fatalf("producers of %q = %v, want %v", IncidentRoutingCoverageWarningFactKind, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("producers of %q = %v, want %v", IncidentRoutingCoverageWarningFactKind, got, want)
		}
	}
}
