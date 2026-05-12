// Package telemetry owns Eshu's OpenTelemetry contract: metric instruments,
// span names, structured log keys, and shared runtime attributes.
//
// The frozen contract lives in contract.go (metric, span, scope, phase,
// and failure-class names) and the metric instruments themselves live in
// instruments.go. Metric names use the eshu_dp_ prefix; new dimensions and
// span names must be registered in contract.go before use, including
// documentation extraction counters, Terraform-state collector spans, and
// the safe_locator_hash and warning_kind dimensions used by the tfstate
// output, module, warning emission, correlation drift-match, drift-admission,
// and drift-intent enqueue counters. The reducer drift handler uses
// SpanReducerDriftEvidenceLoad to bracket the three-query join the
// PostgresDriftEvidenceLoader performs per config_state_drift intent. Pipeline
// stage, graph-backend, and failure-class names stay centralized here so
// runtime packages can report comparable events without inventing local label
// vocabularies. Callers must reuse existing log keys before adding new ones.
// High-cardinality values such as file paths and fact identifiers belong in
// spans or logs, never in metric labels, so dashboards and alerts stay bounded.
package telemetry
