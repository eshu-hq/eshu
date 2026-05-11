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
package tfstateruntime
