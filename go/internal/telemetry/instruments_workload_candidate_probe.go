// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// Workload candidate-probe outcomes (#9986), recorded on the outcome
// dimension of eshu_dp_reducer_workload_candidate_probe_total. The value set
// is closed.
const (
	// WorkloadCandidateProbeOutcomeMayHold means the probe found a repository
	// identity joined to a file fact whose parsed payload holds a value the
	// workload extraction reads, so the bulk-load pre-gate evaluates the
	// own-generation gate before the load and may shed it. The corpus-wide
	// fence does not shed on the fused production loader (#9986).
	WorkloadCandidateProbeOutcomeMayHold = "may_hold"
	// WorkloadCandidateProbeOutcomeNoCandidates means the probe found no such
	// pair; the loader takes the normal load, then the post-load gates.
	WorkloadCandidateProbeOutcomeNoCandidates = "no_candidates"
	// WorkloadCandidateProbeOutcomeError means the probe statement failed and
	// the loader fell back to the full bulk load. A steady error rate means
	// the bulk-load shed is not firing at all.
	WorkloadCandidateProbeOutcomeError = "error"
)

// Workload candidate-probe error cause classes (#9986), recorded on the
// cause_class dimension. The value set is closed; none accompanies every
// non-error outcome.
const (
	// WorkloadCandidateProbeCauseNone accompanies may_hold and no_candidates.
	WorkloadCandidateProbeCauseNone = "none"
	// WorkloadCandidateProbeCauseCanceled means the caller's context was
	// canceled mid-probe (shutdown or lease loss).
	WorkloadCandidateProbeCauseCanceled = "canceled"
	// WorkloadCandidateProbeCauseDeadline means the probe exceeded a deadline.
	WorkloadCandidateProbeCauseDeadline = "deadline"
	// WorkloadCandidateProbeCauseSQLInvalid means PostgreSQL rejected the
	// statement itself (SQLSTATE class 42: syntax error or access rule).
	// This is the #9986 failure shape and never self-heals.
	WorkloadCandidateProbeCauseSQLInvalid = "sql_invalid"
	// WorkloadCandidateProbeCauseConnection means a connection-class
	// failure (SQLSTATE class 08) or a network error.
	WorkloadCandidateProbeCauseConnection = "connection"
	// WorkloadCandidateProbeCauseResource means a server resource or
	// operator-intervention failure (SQLSTATE classes 53 and 57).
	WorkloadCandidateProbeCauseResource = "resource"
	// WorkloadCandidateProbeCauseOther is every other probe failure.
	WorkloadCandidateProbeCauseOther = "other"
)

// registerWorkloadCandidateProbeInstruments registers the #9986 probe-outcome
// counter on inst. One increment per probe call, labeled only by the closed
// outcome and cause_class sets, with no scope or generation label.
func registerWorkloadCandidateProbeInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReducerWorkloadCandidateProbe, err = meter.Int64Counter(
		"eshu_dp_reducer_workload_candidate_probe_total",
		metric.WithDescription("Workload bulk-load pre-gate candidate-precondition probe calls by outcome (may_hold, no_candidates, error) and error cause_class (#9986)"),
	); err != nil {
		return fmt.Errorf("register ReducerWorkloadCandidateProbe counter: %w", err)
	}
	return nil
}
