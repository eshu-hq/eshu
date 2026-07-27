// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"
)

func TestVulnerabilitySuppressionMutationTelemetryContract(t *testing.T) {
	t.Parallel()

	if got, want := SpanAttrVulnerabilitySuppressionMutationOutcome, "eshu.mutation.outcome"; got != want {
		t.Fatalf("outcome attribute = %q, want %q", got, want)
	}
	got := VulnerabilitySuppressionMutationOutcomes()
	want := []string{"created", "unchanged", "rejected", "store_error"}
	if !slices.Equal(got, want) {
		t.Fatalf("mutation outcomes = %#v, want %#v", got, want)
	}
	got[0] = "mutated"
	if slices.Equal(got, VulnerabilitySuppressionMutationOutcomes()) {
		t.Fatal("VulnerabilitySuppressionMutationOutcomes returned shared storage")
	}
}
