// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// claimPlaneRequestBuckets are second-scale boundaries for one claim-plane
// HTTP round trip (#8875). They resolve a healthy in-cluster call (a few
// milliseconds) and end at the 10 s client timeout, so a timeout lands in
// the last finite bucket instead of +Inf.
var claimPlaneRequestBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// registerClaimPlaneInstruments registers the #8875 collector claim-store
// instruments on inst. Request metrics carry only the closed protocol
// operation set and the closed workflow.ClaimPlaneOutcome set; the selection
// counter carries only backend_kind (postgres, claim_plane). No item, claim,
// scope, or instance id is a label.
func registerClaimPlaneInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.WorkflowClaimPlaneRequests, err = meter.Int64Counter(
		"eshu_dp_workflow_claim_plane_requests_total",
		metric.WithDescription("Collector requests to the workflow claim plane by protocol operation and outcome (ok, rejected, not_found, paced, invalid, server_error, timeout, canceled, transport_error, decode_error, request_error) (#8875)"),
	); err != nil {
		return fmt.Errorf("register WorkflowClaimPlaneRequests counter: %w", err)
	}
	if inst.WorkflowClaimPlaneRequestDuration, err = meter.Float64Histogram(
		"eshu_dp_workflow_claim_plane_request_duration_seconds",
		metric.WithDescription("Round-trip duration of one collector request to the workflow claim plane by protocol operation and outcome (#8875)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(claimPlaneRequestBuckets...),
	); err != nil {
		return fmt.Errorf("register WorkflowClaimPlaneRequestDuration histogram: %w", err)
	}
	if inst.WorkflowControlStoreSelections, err = meter.Int64Counter(
		"eshu_dp_workflow_control_store_selections_total",
		metric.WithDescription("Workflow claim store a collector selected at startup by backend_kind (postgres, claim_plane); one increment per process start (#8875)"),
	); err != nil {
		return fmt.Errorf("register WorkflowControlStoreSelections counter: %w", err)
	}
	return nil
}
