// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"testing"
)

func TestParseGroupedInventoryDimensionClosedSet(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]GroupedInventoryDimension{
		"service_id":  GroupedInventoryByService,
		"workload_id": GroupedInventoryByWorkload,
		"environment": GroupedInventoryByEnvironment,
	} {
		got, ok := ParseGroupedInventoryDimension(raw)
		if !ok || got != want {
			t.Errorf("ParseGroupedInventoryDimension(%q) = %q, %v; want %q, true", raw, got, ok, want)
		}
	}
	// The five single-valued dimensions stay on AggregateStore; the grouped
	// port must not claim them.
	for _, raw := range []string{"", "impact_status", "priority_bucket", "severity", "repository_id", "ecosystem", "Service_ID", "bogus"} {
		if got, ok := ParseGroupedInventoryDimension(raw); ok {
			t.Errorf("ParseGroupedInventoryDimension(%q) = %q, true; want rejected", raw, got)
		}
	}
}

func TestGroupCompletenessClosedSet(t *testing.T) {
	t.Parallel()

	for _, c := range []GroupCompleteness{
		GroupCompletenessComplete,
		GroupCompletenessPartial,
		GroupCompletenessUnattributed,
		GroupCompletenessReadModelUnavailable,
	} {
		if !c.Valid() {
			t.Errorf("%q.Valid() = false, want true", c)
		}
	}
	for _, c := range []GroupCompleteness{"", "ready", "COMPLETE", "readiness_state"} {
		if c.Valid() {
			t.Errorf("%q.Valid() = true, want false", c)
		}
	}
	if GroupCompletenessComplete != "complete" ||
		GroupCompletenessPartial != "partial" ||
		GroupCompletenessUnattributed != "unattributed" ||
		GroupCompletenessReadModelUnavailable != "read_model_unavailable" {
		t.Fatal("completeness wire values drifted from the #9873 ruling")
	}
}

func TestGroupedInventoryRowWireShape(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(GroupedInventoryRow{
		Dimension:        GroupedInventoryByService,
		Value:            "svc-a",
		Count:            3,
		ByImpactStatus:   map[string]int{"affected_exact": 3},
		BySeverity:       map[string]int{"high": 3},
		MaxPriorityScore: 80,
		Completeness:     GroupCompletenessComplete,
		MissingEvidence:  []string{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{
		"dimension", "value", "unattributed", "count", "by_impact_status",
		"by_severity", "max_priority_score", "completeness", "missing_evidence",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("row JSON lacks %q: %s", key, raw)
		}
	}
}

func TestUnattributedMissingEvidenceNamesTheDimensionGap(t *testing.T) {
	t.Parallel()

	for dimension, want := range map[GroupedInventoryDimension]string{
		GroupedInventoryByService:     "service_mapping",
		GroupedInventoryByWorkload:    "workload_mapping",
		GroupedInventoryByEnvironment: "current_deploy_evidence",
	} {
		if got := UnattributedMissingEvidence(dimension); got != want {
			t.Errorf("UnattributedMissingEvidence(%q) = %q, want %q", dimension, got, want)
		}
	}
}
