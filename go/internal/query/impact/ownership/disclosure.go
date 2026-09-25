// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

// Route patterns the impact ownership metrics are labelled with.
const (
	RouteTraceResourceToCode   = "POST /api/v0/impact/trace-resource-to-code"
	RouteExplainDependencyPath = "POST /api/v0/impact/explain-dependency-path"
	RouteTraceExposurePath     = "POST /api/v0/impact/trace-exposure-path"
)

// Withheld section names a scoped response discloses. They are static per
// route and returned on every scoped response, so their presence says only
// that the caller is scoped, never whether anything was withheld.
const (
	// WithheldPathsThroughUngrantedNodes: paths crossing a node the grant does
	// not own (or one past the ownership budget) are dropped whole.
	WithheldPathsThroughUngrantedNodes = "paths_through_ungranted_nodes"
	// WithheldUnownedSinkClasses: exposure sinks whose class has no
	// repository owner (SecretsIAMSecretMetadataPath, CidrBlock).
	WithheldUnownedSinkClasses = "unowned_sink_classes"
)

// WithheldSinkLabels are the exposure sink labels no grant can bind: they
// carry no repo_id and have no owning edge the ownership statements walk.
var WithheldSinkLabels = []string{"SecretsIAMSecretMetadataPath", "CidrBlock"}

// WithheldSinkReason is the coverage.unresolved_reason text a scoped
// exposure-path response carries, naming the withheld sink classes.
const WithheldSinkReason = "scoped caller: sinks of class SecretsIAMSecretMetadataPath and CidrBlock are withheld because no repository owns them, and paths crossing a node outside the grant are dropped"

// Disclose adds the scoped disclosure fields to a response map: scoped=true
// and the route's static withheld_sections list.
func Disclose(resp map[string]any, sections ...string) {
	resp["scoped"] = true
	resp["withheld_sections"] = append([]string(nil), sections...)
}
