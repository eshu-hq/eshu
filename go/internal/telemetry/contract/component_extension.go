// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Component-extension status telemetry (#8908). An out-of-tree collector
// reports bounded SDK status rows (class, failure_class, fact_count,
// source_latency_ms, retry_after_seconds, warning_count) with every claim
// result, and the extension host turns them into these operator signals. The
// extension emits no telemetry of its own, so this family is how an operator
// sees an extracted collector at all.
//
// Instruments driven by this contract:
//
//   - eshu_dp_component_extension_results_total (collector_kind, state,
//     failure_class): one increment per claim result, including results the
//     host rejected (invalid claim, launch failure, invalid result, identity
//     mismatch), which carry the host failure class.
//   - eshu_dp_component_extension_status_records_total (collector_kind, state,
//     status_class, failure_class): one increment per validated SDK status row.
//   - eshu_dp_component_extension_reported_facts_total and
//     eshu_dp_component_extension_reported_warnings_total (collector_kind,
//     state): the fact_count and warning_count the extension reported.
//   - eshu_dp_component_extension_source_latency_seconds (collector_kind,
//     state): source_latency_ms from each status row that reports one.
//   - eshu_dp_component_extension_retry_after_seconds (collector_kind): the
//     retry delay the extension requested on the latest result for the kind
//     (after the one-hour cap), 0 when the latest result was not retryable.
//     The host records it before ClaimedService decides: ClaimedService
//     applies max(poll interval, this), or none once the attempt budget is
//     spent, so it is the request, not the applied delay. state is likewise
//     the extension's result state, not the durable claim outcome.
//
// collector_kind is bounded by the component manifest. state and status_class
// are closed sets. failure_class is pattern-bounded by the host and capped per
// process (ComponentExtensionFailureClassCap); past the cap new values fold to
// "other". No label carries a scope id, instance id, work item id, URL,
// provider value, credential, or payload content.

// Metric names for the component-extension status family.
const (
	// MetricComponentExtensionResults counts claim results.
	MetricComponentExtensionResults = "eshu_dp_component_extension_results_total"
	// MetricComponentExtensionStatusRecords counts validated SDK status rows.
	MetricComponentExtensionStatusRecords = "eshu_dp_component_extension_status_records_total"
	// MetricComponentExtensionReportedFacts sums reported status fact_count.
	MetricComponentExtensionReportedFacts = "eshu_dp_component_extension_reported_facts_total"
	// MetricComponentExtensionReportedWarnings sums reported status
	// warning_count.
	MetricComponentExtensionReportedWarnings = "eshu_dp_component_extension_reported_warnings_total"
	// MetricComponentExtensionSourceLatency observes reported source latency.
	MetricComponentExtensionSourceLatency = "eshu_dp_component_extension_source_latency_seconds"
	// MetricComponentExtensionRetryAfter is the gauge of the retry delay the
	// extension requested, after the one-hour cap (not the applied delay).
	MetricComponentExtensionRetryAfter = "eshu_dp_component_extension_retry_after_seconds"
)

// Log keys on the one structured line the host writes per claim result. The
// line also carries the root collector_kind and failure_class keys. It never
// carries a scope id, instance id, URL, provider value, or payload.
const (
	// LogKeyExtensionStatusOrigin is sdk when the extension's result was
	// validated, or host when the host rejected the claim or result.
	LogKeyExtensionStatusOrigin = "extension_status.origin"
	// LogKeyExtensionStatusState is the claim result state.
	LogKeyExtensionStatusState = "extension_status.state"
	// LogKeyExtensionStatusCount is the number of SDK status rows recorded.
	LogKeyExtensionStatusCount = "extension_status.status_count"
	// LogKeyExtensionStatusFactCount is the summed reported fact_count.
	LogKeyExtensionStatusFactCount = "extension_status.fact_count"
	// LogKeyExtensionStatusWarningCount is the summed reported warning_count.
	LogKeyExtensionStatusWarningCount = "extension_status.warning_count"
	// LogKeyExtensionStatusSourceLatencyMS is the largest reported
	// source_latency_ms.
	LogKeyExtensionStatusSourceLatencyMS = "extension_status.source_latency_ms"
	// LogKeyExtensionStatusPartial is true when any status row was partial.
	LogKeyExtensionStatusPartial = "extension_status.partial"
	// LogKeyExtensionStatusRetryAfterSeconds is the retry delay the extension
	// requested, after the one-hour cap. ClaimedService applies
	// max(poll interval, this), or none once the attempt budget is spent.
	LogKeyExtensionStatusRetryAfterSeconds = "extension_status.retry_after_seconds"
	// LogKeyExtensionStatusRetryAfterRequestedSeconds is the largest
	// retry_after_seconds the extension requested, before the cap.
	LogKeyExtensionStatusRetryAfterRequestedSeconds = "extension_status.retry_after_requested_seconds"
)

// Closed values of the origin log field.
const (
	// ExtensionStatusOriginSDK marks a result the SDK validator accepted.
	ExtensionStatusOriginSDK = "sdk"
	// ExtensionStatusOriginHost marks a host-side verdict on the claim.
	ExtensionStatusOriginHost = "host"
)

// Closed values of the state label (the SDK result states).
const (
	ExtensionResultStateComplete  = "complete"
	ExtensionResultStateUnchanged = "unchanged"
	ExtensionResultStatePartial   = "partial"
	ExtensionResultStateRetryable = "retryable"
	ExtensionResultStateTerminal  = "terminal"
)

// Closed values of the status_class label (the SDK status classes).
const (
	ExtensionStatusClassProgress = "progress"
	ExtensionStatusClassWarning  = "warning"
	ExtensionStatusClassFailure  = "failure"
	ExtensionStatusClassComplete = "complete"
)

// Fold values shared by the family's labels.
const (
	// ExtensionLabelUnknown replaces a state, status_class, or collector_kind
	// value outside its closed set.
	ExtensionLabelUnknown = "unknown"
	// ExtensionFailureClassNone labels a result or row with no failure class.
	ExtensionFailureClassNone = "none"
	// ExtensionFailureClassOther replaces a failure class that is not
	// pattern-bounded or arrives after the per-process cap is reached.
	ExtensionFailureClassOther = "other"
)

// ComponentExtensionFailureClassCap bounds the distinct failure_class label
// values one process admits. Host classes and typical provider classes
// (rate_limited, auth_failed, ...) fit well inside it; it is the backstop
// against an extension minting a new class per result.
const ComponentExtensionFailureClassCap = 64
