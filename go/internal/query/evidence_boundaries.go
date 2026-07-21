// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "sort"

// PostgresOnlyBoundary records a gap between a Postgres-only reducer domain
// and a graph-sourced story surface. It is a static, closed-vocabulary
// disclosure — never a per-request probe.
type PostgresOnlyBoundary struct {
	Domain      string `json:"domain"`
	ReadSurface string `json:"read_surface"`
	Reason      string `json:"reason"`
}

const boundaryReasonPostgresOnly = "postgres_only_read_model"

// attachEvidenceBoundaries adds a non-nil evidence_boundaries field to the
// response map when boundaries exist for the read surface. The field is
// nil/omitted when no boundaries apply. The slice is typed []PostgresOnlyBoundary
// so JSON encoding honours the struct tags (lowercase snake_case keys).
func attachEvidenceBoundaries(data map[string]any, readSurface string) {
	if data == nil {
		return
	}
	boundaries := evidenceBoundariesFor(readSurface)
	if len(boundaries) == 0 {
		return
	}
	data["evidence_boundaries"] = boundaries
}

// evidenceBoundariesFor returns the static Postgres-only boundaries for the
// named read surface. Entries are stable-ordered by domain. Returns nil when
// no boundaries apply.
func evidenceBoundariesFor(readSurface string) []PostgresOnlyBoundary {
	// get_service_story intentionally has no entries here: every Postgres-only
	// domain it touches (ci_cd_run_correlation via response["ci_cd_evidence"];
	// container_image_identity via response["code_to_runtime_trace"]'s
	// image_package segment, service_story_trace_path.go:94-121) is already
	// served through a sibling top-level field, so there is no boundary left to
	// disclose. See TestBuildServiceStoryResponseOmitsBoundaryForFieldAlreadyServed
	// and TestBuildServiceStoryResponseOmitsContainerImageIdentityBoundaryForFieldAlreadyServed.
	type pair struct{ domain, surface string }
	pairs := []pair{
		{domain: "ci_cd_run_correlation", surface: "get_workload_story"},
		{domain: "container_image_identity", surface: "get_workload_story"},
		{domain: "package_correlation", surface: "get_workload_story"},
		{domain: "container_image_identity", surface: "get_repo_story"},
		{domain: "package_correlation_ownership", surface: "get_repo_story"},
		{domain: "package_correlation_publication", surface: "get_repo_story"},
		{domain: "ci_cd_run_correlation", surface: "trace_deployment_chain"},
		{domain: "container_image_identity", surface: "trace_deployment_chain"},
	}
	var boundaries []PostgresOnlyBoundary
	for _, p := range pairs {
		if p.surface == readSurface {
			boundaries = append(boundaries, PostgresOnlyBoundary{
				Domain:      p.domain,
				ReadSurface: p.surface,
				Reason:      boundaryReasonPostgresOnly,
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
