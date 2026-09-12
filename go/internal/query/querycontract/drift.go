// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// DriftedAttributeView is one declared/observed value pair for an
// image_version_drift finding's comparable attribute (ami, image_uri,
// version, or the synthetic "image" key for the ECS container-image
// comparison).
//
// Its home moved here (#6642 Part A) from root package query
// (cloud_runtime_drift.go) so both the staying multi-cloud runtime-drift
// surface and the iac/ leaf's AWS-specific runtime-drift surface can share
// one type without an import cycle: iac/ needs the type for
// ManagementFindingRow.DriftedAttributes and AWSRuntimeDriftFindingRow, and
// root's cloud_runtime_drift.go (which iac/ cannot import) already needs the
// same type for its own MultiCloudRuntimeDriftFindingRow. Root keeps a type
// alias (cloud_runtime_drift.go) so every existing caller keeps spelling
// query.DriftedAttributeView unchanged.
type DriftedAttributeView struct {
	// Attribute is the allowlisted comparable attribute name.
	Attribute string `json:"attribute"`
	// Declared is the Terraform-state value.
	Declared string `json:"declared_value"`
	// Observed is the AWS-observed cloud value.
	Observed string `json:"observed_value"`
}
