// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// impact_candidate_access_filter.go holds the grant-binding row filters whose
// signatures name the candidate structs this package defines
// (ChangeSurfaceTargetCandidate, ResourceInvestigationCandidate). They live
// here rather than in impacttrace/impact_access_filter.go because an
// impacttrace function cannot name an impact-defined type (package impact
// imports impacttrace, so the reverse import would be a cycle). The row-map
// and provisioning filters, whose element types live in impacttrace, stay
// there. See #6060.

// filterChangeSurfaceCandidatesForAccess drops resolved change-surface target
// candidates (impact_change_surface_resolvers.go) whose RepoID is outside the
// caller's grant. Every ChangeSurfaceTargetCandidate resolver query
// (Workload, WorkloadInstance, Repository, CloudResource, TerraformModule,
// DataAsset) already projects repo_id (or, for Repository, its own id) into
// the candidate, so this filter needs no additional graph read.
func filterChangeSurfaceCandidatesForAccess(
	candidates []ChangeSurfaceTargetCandidate,
	access querycontract.RepositoryAccessFilter,
) []ChangeSurfaceTargetCandidate {
	if !access.Scoped() {
		return candidates
	}
	filtered := make([]ChangeSurfaceTargetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if impacttrace.ImpactRepoIDAllowed(candidate.RepoID, access) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// filterResourceInvestigationCandidatesForAccess drops resolved resource
// investigation candidates (impact_resource_investigation_response.go) whose
// RepoID is outside the caller's grant.
func filterResourceInvestigationCandidatesForAccess(
	candidates []ResourceInvestigationCandidate,
	access querycontract.RepositoryAccessFilter,
) []ResourceInvestigationCandidate {
	if !access.Scoped() {
		return candidates
	}
	filtered := make([]ResourceInvestigationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if impacttrace.ImpactRepoIDAllowed(candidate.RepoID, access) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}
