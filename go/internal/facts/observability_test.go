// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestObservabilityFactKindsAndSchemaVersions(t *testing.T) {
	want := []string{
		ObservabilitySourceInstanceFactKind,
		ObservabilityDeclaredFolderFactKind,
		ObservabilityDeclaredDashboardFactKind,
		ObservabilityDeclaredDatasourceFactKind,
		ObservabilityDeclaredAlertRuleFactKind,
		ObservabilityDeclaredScrapeConfigFactKind,
		ObservabilityDeclaredMetricRuleFactKind,
		ObservabilityDeclaredMetricRouteFactKind,
		ObservabilityDeclaredLogRouteFactKind,
		ObservabilityDeclaredTraceRouteFactKind,
		ObservabilityAppliedResourceFactKind,
		ObservabilityAppliedSyncStateFactKind,
		ObservabilityObservedDashboardFactKind,
		ObservabilityObservedTargetFactKind,
		ObservabilityObservedRuleFactKind,
		ObservabilityObservedLogSignalFactKind,
		ObservabilityObservedTraceSignalFactKind,
		ObservabilityCoverageWarningFactKind,
	}

	got := ObservabilityFactKinds()
	if len(got) != len(want) {
		t.Fatalf("ObservabilityFactKinds len = %d, want %d: %#v", len(got), len(want), got)
	}
	for index, kind := range want {
		if got[index] != kind {
			t.Fatalf("ObservabilityFactKinds[%d] = %q, want %q", index, got[index], kind)
		}
		version, ok := ObservabilitySchemaVersion(kind)
		if !ok || version != ObservabilitySchemaVersionV1 {
			t.Fatalf("ObservabilitySchemaVersion(%q) = %q, %v; want %q, true", kind, version, ok, ObservabilitySchemaVersionV1)
		}
	}
	if version, ok := ObservabilitySchemaVersion("observability.unknown"); ok || version != "" {
		t.Fatalf("ObservabilitySchemaVersion(unknown) = %q, %v; want empty false", version, ok)
	}

	got[0] = "mutated"
	if ObservabilityFactKinds()[0] != ObservabilitySourceInstanceFactKind {
		t.Fatal("ObservabilityFactKinds returned mutable backing slice")
	}
}
