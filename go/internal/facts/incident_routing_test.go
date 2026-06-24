// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

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
