// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "sort"

// Evidence boundary disclosures shared by the query root and the
// handler-family subpackages (#6060 lane B2).
//
// A boundary records a gap between a Postgres-only reducer domain and a
// graph-sourced story surface. It is a static, closed-vocabulary disclosure —
// never a per-request probe. These live here rather than in package query
// because the impact family's trace handler attaches them and cannot import
// the root package back without an import cycle. Package query keeps
// forwarding wrappers under the original names.

// PostgresOnlyBoundary records a gap between a Postgres-only reducer domain
// and a graph-sourced story surface.
type PostgresOnlyBoundary struct {
	Domain      string `json:"domain"`
	ReadSurface string `json:"read_surface"`
	Reason      string `json:"reason"`
}

// BoundaryReasonPostgresOnly is the reason attached to every Postgres-only
// boundary entry.
const BoundaryReasonPostgresOnly = "postgres_only_read_model"

// AttachEvidenceBoundaries adds a non-nil evidence_boundaries field to the
// response map when boundaries exist for the read surface. The field is
// nil/omitted when no boundaries apply. The slice is typed
// []PostgresOnlyBoundary so JSON encoding honours the struct tags (lowercase
// snake_case keys).
func AttachEvidenceBoundaries(data map[string]any, readSurface string) {
	if data == nil {
		return
	}
	boundaries := EvidenceBoundariesFor(readSurface)
	if len(boundaries) == 0 {
		return
	}
	data["evidence_boundaries"] = boundaries
}

// EvidenceBoundariesFor returns the static Postgres-only boundaries for the
// named read surface. Entries are stable-ordered by domain. Returns nil when
// no boundaries apply.
func EvidenceBoundariesFor(readSurface string) []PostgresOnlyBoundary {
	// get_service_story intentionally has no entries here: every Postgres-only
	// domain it touches (ci_cd_run_correlation via response["ci_cd_evidence"];
	// container_image_identity via response["code_to_runtime_trace"]'s
	// image_package segment, story_trace_path.go:104-131) is already
	// served through a sibling top-level field, so there is no boundary left to
	// disclose. See TestBuildServiceStoryResponseOmitsBoundaryForFieldAlreadyServed
	// and TestBuildServiceStoryResponseOmitsContainerImageIdentityBoundaryForFieldAlreadyServed.
	//
	// get_repo_story has no entries here either (issue #5457): its three prior
	// boundary domains -- container_image_identity, package_correlation_ownership,
	// and package_correlation_publication -- all now project canonical graph
	// edges (BUILT_FROM, PUBLISHES) per
	// docs/internal/design/5472-graph-projection-policy.md, so there is no
	// longer a Postgres-only gap to disclose for this surface.
	//
	// get_workload_story drops its container_image_identity entry for the same
	// reason (#5457 BUILT_FROM projection), and narrows the former blanket
	// "package_correlation" entry to package_correlation_consumption: ownership
	// and publication now project PUBLISHES edges, but consumption correlation
	// deliberately STAYS Postgres-only (it overlaps the existing
	// DECLARES_DEPENDENCY/DEPENDS_ON graph lanes, #5472 policy), so that
	// narrower gap is still genuinely undisclosed.
	type pair struct{ domain, surface string }
	pairs := []pair{
		{domain: "ci_cd_run_correlation", surface: "get_workload_story"},
		{domain: "package_correlation_consumption", surface: "get_workload_story"},
		{domain: "ci_cd_run_correlation", surface: "trace_deployment_chain"},
		{domain: "container_image_identity", surface: "trace_deployment_chain"},
	}
	var boundaries []PostgresOnlyBoundary
	for _, p := range pairs {
		if p.surface == readSurface {
			boundaries = append(boundaries, PostgresOnlyBoundary{
				Domain:      p.domain,
				ReadSurface: p.surface,
				Reason:      BoundaryReasonPostgresOnly,
			})
		}
	}
	if len(boundaries) == 0 {
		return nil
	}
	sort.Slice(boundaries, func(i, j int) bool {
		return boundaries[i].Domain < boundaries[j].Domain
	})
	return boundaries
}
