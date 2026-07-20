// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

// benchScopedGrantAccess grants half of the benchmark repositories so the
// filters do real membership work (keep + drop), not a trivial all-pass or
// all-drop path.
func benchScopedGrantAccess(repoCount int) repositoryAccessFilter {
	granted := make([]string, 0, repoCount/2)
	allowed := make(map[string]struct{}, repoCount/2)
	for i := 0; i < repoCount; i += 2 {
		id := fmt.Sprintf("repo-%d", i)
		granted = append(granted, id)
		allowed[id] = struct{}{}
	}
	return repositoryAccessFilter{
		allowedRepositoryIDs: granted,
		allowed:              allowed,
	}
}

// benchRepoIDRows builds repoCount rows carrying a "repo_id" field, the shape
// filterRowsByRepoIDForAccess iterates.
func benchRepoIDRows(repoCount int) []map[string]any {
	rows := make([]map[string]any, repoCount)
	for i := range rows {
		rows[i] = map[string]any{
			"repo_id": fmt.Sprintf("repo-%d", i),
			"name":    fmt.Sprintf("service-%d", i),
		}
	}
	return rows
}

// benchProvisioningCandidates builds repoCount provisioningRepositoryCandidate
// values, the shape filterProvisioningRepositoryCandidatesForAccess iterates.
func benchProvisioningCandidates(repoCount int) []provisioningRepositoryCandidate {
	candidates := make([]provisioningRepositoryCandidate, repoCount)
	for i := range candidates {
		candidates[i] = provisioningRepositoryCandidate{
			RepoID:            fmt.Sprintf("repo-%d", i),
			RepoName:          fmt.Sprintf("service-%d", i),
			RelationshipTypes: []string{"PROVISIONS_DEPENDENCY_FOR"},
		}
	}
	return candidates
}

// benchRelationshipOverviewRows builds repoCount anchor-aware relationship
// overview rows (anchor repo-0 on one endpoint, a far repo on the other), the
// shape filterRepoRelationshipOverviewRowsForAccess iterates.
func benchRelationshipOverviewRows(repoCount int) []map[string]any {
	rows := make([]map[string]any, repoCount)
	for i := range rows {
		rows[i] = map[string]any{
			"direction": "outgoing",
			"type":      "DEPENDS_ON",
			"source_id": "repo-0",
			"target_id": fmt.Sprintf("repo-%d", i),
		}
	}
	return rows
}

// The provisioning/service/repository route result sets are bounded by existing
// LIMITs (repositoryDeploymentEvidenceArtifactLimit = 50,
// serviceStoryItemLimit, and the trace enrichment limit), so 50 is the
// representative worst-case row count for these filters.
const benchScopedGrantRowCount = 50

func BenchmarkFilterRowsByRepoIDForAccess(b *testing.B) {
	access := benchScopedGrantAccess(benchScopedGrantRowCount)
	rows := benchRepoIDRows(benchScopedGrantRowCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filterRowsByRepoIDForAccess(rows, access)
	}
}

func BenchmarkFilterProvisioningRepositoryCandidatesForAccess(b *testing.B) {
	access := benchScopedGrantAccess(benchScopedGrantRowCount)
	candidates := benchProvisioningCandidates(benchScopedGrantRowCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filterProvisioningRepositoryCandidatesForAccess(candidates, access)
	}
}

func BenchmarkFilterRepoRelationshipOverviewRowsForAccess(b *testing.B) {
	access := benchScopedGrantAccess(benchScopedGrantRowCount)
	rows := benchRelationshipOverviewRows(benchScopedGrantRowCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filterRepoRelationshipOverviewRowsForAccess(rows, "repo-0", access)
	}
}

func BenchmarkImpactRepoIDAllowed(b *testing.B) {
	access := benchScopedGrantAccess(benchScopedGrantRowCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = impactRepoIDAllowed("repo-10", access)
	}
}
