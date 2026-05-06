// Package telemetry owns Eshu's OpenTelemetry contract: metric instruments,
// span names, structured log keys, and shared runtime attributes.
//
// The frozen contract lives in contract.go (metric, span, scope, phase,
// and failure-class names) and the metric instruments themselves live in
// instruments.go. Metric names use the eshu_dp_ prefix; new dimensions and
// span names must be registered in contract.go before use, and callers
// must reuse existing log keys before adding new ones. High-cardinality
// values such as file paths and fact identifiers belong in spans or logs,
// never in metric labels, so dashboards and alerts stay bounded.
package telemetry
