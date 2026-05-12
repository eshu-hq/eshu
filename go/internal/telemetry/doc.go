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
// vocabularies. The drift loader's module-aware join (issue #169) registers
// the DriftUnresolvedModuleCalls counter and the
// MetricDimensionDriftUnresolvedModuleReason dimension here so the closed
// enum of unresolvable-module reasons (external_registry, external_git,
// external_archive, cross_repo_local, cycle_detected, depth_exceeded) stays
// anchored to the contract surface. The streaming-nested-walker work in
// ADR 2026-05-12-tfstate-parser-composite-capture-for-schema-known-paths
// registers the DriftSchemaUnknownComposite counter and the
// MetricDimensionResourceType dimension here so operators can detect
// provider-schema drift; the high-cardinality companions (attribute_key,
// source path, error) stay in the LogKeyDriftComposite* log attrs and out
// of metric labels. Callers must reuse existing log keys before adding new
// ones.
// High-cardinality values such as file paths and fact identifiers belong in
// spans or logs, never in metric labels, so dashboards and alerts stay bounded.
package telemetry
