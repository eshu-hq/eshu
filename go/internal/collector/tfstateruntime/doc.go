// Package tfstateruntime adapts Terraform-state source discovery and parsing
// to the workflow-claimed collector runtime.
//
// In addition to surfacing parsed facts to the workflow committer, this
// package records per-claim telemetry counters
// (eshu_dp_tfstate_outputs_emitted_total,
// eshu_dp_tfstate_modules_emitted_total,
// eshu_dp_tfstate_warnings_emitted_total) labeled with backend_kind,
// safe_locator_hash, and (for warnings) warning_kind so dashboards and
// alerts see per-locator emission rates without raw locators.
//
// ClaimedSource also wires a CompositeCaptureRecorder into ParseOptions
// (compositeCaptureLoggingRecorder in composite_capture_recorder.go) so the
// parser can increment eshu_dp_drift_schema_unknown_composite_total with a
// bounded reason label and emit a slog.Warn line whenever it drops a composite
// before capture or the streaming nested walker stops mid-capture. The log
// carries the high-cardinality attribute_key and source path that stay out of
// metric labels.
package tfstateruntime
