// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestWorkloadCandidateProbeContract pins the closed outcome and cause value
// sets of eshu_dp_reducer_workload_candidate_probe_total (#9986) and that
// NewInstruments registers the counter.
func TestWorkloadCandidateProbeContract(t *testing.T) {
	t.Parallel()

	if got, want := []string{
		WorkloadCandidateProbeOutcomeMayHold,
		WorkloadCandidateProbeOutcomeNoCandidates,
		WorkloadCandidateProbeOutcomeError,
	}, []string{"may_hold", "no_candidates", "error"}; !slices.Equal(got, want) {
		t.Fatalf("probe outcomes = %v, want the closed set %v", got, want)
	}
	if got, want := []string{
		WorkloadCandidateProbeCauseNone,
		WorkloadCandidateProbeCauseCanceled,
		WorkloadCandidateProbeCauseDeadline,
		WorkloadCandidateProbeCauseSQLInvalid,
		WorkloadCandidateProbeCauseConnection,
		WorkloadCandidateProbeCauseResource,
		WorkloadCandidateProbeCauseOther,
	}, []string{"none", "canceled", "deadline", "sql_invalid", "connection", "resource", "other"}; !slices.Equal(got, want) {
		t.Fatalf("probe causes = %v, want the closed set %v", got, want)
	}

	instruments, err := NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.ReducerWorkloadCandidateProbe == nil {
		t.Fatal("ReducerWorkloadCandidateProbe counter not registered")
	}
}
