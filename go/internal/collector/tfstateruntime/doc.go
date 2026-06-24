// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
// bounded reason label for every skipped composite and emit one slog.Warn line
// per resource_type, attribute_key, and reason shape. The log carries the
// high-cardinality attribute_key and source path that stay out of metric
// labels.
// Warning-only generations for missing or oversized state sources emit stable
// reason codes and classified warning facts so status/API readbacks can mark
// those rows as blocking evidence without exposing raw state locators.
package tfstateruntime
