// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerComponentExtensionInstruments registers the #8908 component
// extension status family on inst. Record it through
// ComponentExtensionStatusRecorder, which folds every label into its bounded
// set (collector_kind, state, status_class, failure_class) and never labels a
// scope, instance, work item, URL, or provider value.
func registerComponentExtensionInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ComponentExtensionResults, err = meter.Int64Counter(
		"eshu_dp_component_extension_results_total",
		metric.WithDescription("Component-extension claim results by collector_kind, state (complete, unchanged, partial, retryable, terminal), and failure_class, including host verdicts (invalid_claim, launch_failure, invalid_result, identity_mismatch, status_record) (#8908)"),
	); err != nil {
		return fmt.Errorf("register ComponentExtensionResults counter: %w", err)
	}
	if inst.ComponentExtensionStatusRecords, err = meter.Int64Counter(
		"eshu_dp_component_extension_status_records_total",
		metric.WithDescription("Validated component-extension SDK status rows by collector_kind, state, status_class, and failure_class (#8908)"),
	); err != nil {
		return fmt.Errorf("register ComponentExtensionStatusRecords counter: %w", err)
	}
	if inst.ComponentExtensionReportedFacts, err = meter.Int64Counter(
		"eshu_dp_component_extension_reported_facts_total",
		metric.WithDescription("Fact counts component extensions reported in SDK status rows, by collector_kind and state; committed facts are eshu_dp_workflow_claim_facts_emitted_total (#8908)"),
	); err != nil {
		return fmt.Errorf("register ComponentExtensionReportedFacts counter: %w", err)
	}
	if inst.ComponentExtensionReportedWarnings, err = meter.Int64Counter(
		"eshu_dp_component_extension_reported_warnings_total",
		metric.WithDescription("Warning counts component extensions reported in SDK status rows, by collector_kind and state (#8908)"),
	); err != nil {
		return fmt.Errorf("register ComponentExtensionReportedWarnings counter: %w", err)
	}
	if inst.ComponentExtensionSourceLatency, err = meter.Float64Histogram(
		"eshu_dp_component_extension_source_latency_seconds",
		metric.WithDescription("Source latency component extensions reported in SDK status rows (source_latency_ms), by collector_kind and state (#8908)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300),
	); err != nil {
		return fmt.Errorf("register ComponentExtensionSourceLatency histogram: %w", err)
	}
	if inst.ComponentExtensionRetryAfter, err = meter.Float64Gauge(
		"eshu_dp_component_extension_retry_after_seconds",
		metric.WithDescription("Retry delay the extension requested on the latest claim result for the collector_kind, after the one-hour cap; 0 when the latest result was not retryable. Recorded before ClaimedService decides: it applies max(poll interval, this), and none once the attempt budget is spent (#8908)"),
		metric.WithUnit("s"),
	); err != nil {
		return fmt.Errorf("register ComponentExtensionRetryAfter gauge: %w", err)
	}
	return nil
}
