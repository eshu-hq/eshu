// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"

// ServiceWorkloadCandidate is one service-workload candidate row. Exported as the result element of QueryServiceWorkloadCandidates; see #6060.
type ServiceWorkloadCandidate struct {
	ID          string
	Name        string
	Kind        string
	RepoID      string
	RepoName    string
	Environment string
	MatchBasis  string
}

// serviceWorkloadCandidateMaps renders ambiguity candidates for the 409
// details, one map per candidate in input order.
func serviceWorkloadCandidateMaps(candidates []ServiceWorkloadCandidate) []map[string]any {
	rows := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, serviceWorkloadCandidateMap(candidate))
	}
	return rows
}

// serviceWorkloadCandidateMap is the single place a candidate's wire shape is
// built. Fields are additive: existing keys keep their names, and
// next_selector names the exact argument and value to pass on the next call.
// The service_id value is the candidate's id (a full scoped id, or a handle
// for a legacy node), which the resolver matches exactly, so the follow-up
// call resolves one workload. A new
// per-candidate field, or a change to next_selector for one kind of
// candidate, belongs here as one branch.
//
// legacy_id is true only when the id is the name-only handle of the
// candidate's own name (workload:<name>, compared exactly), the shape the
// #5385 identity change replaced. legacyworkload.IsLegacyID applies the same
// exact comparison as the retirement's detection and delete Cypher, so a node
// the marker flags is one the retirement examines. A scoped candidate carries no legacy_id key. The marker
// is information only: the resolver still refuses to pick a candidate (#8795).
func serviceWorkloadCandidateMap(candidate ServiceWorkloadCandidate) map[string]any {
	row := map[string]any{
		"service_id":    candidate.ID,
		"service_name":  candidate.Name,
		"kind":          candidate.Kind,
		"repo_id":       candidate.RepoID,
		"repo_name":     candidate.RepoName,
		"environment":   candidate.Environment,
		"match_basis":   candidate.MatchBasis,
		"next_selector": map[string]any{"argument": "service_id", "value": candidate.ID},
	}
	if legacyworkload.IsLegacyID(candidate.ID, candidate.Name) {
		row["legacy_id"] = true
	}
	return row
}
