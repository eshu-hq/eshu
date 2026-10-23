// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

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
func serviceWorkloadCandidateMap(candidate ServiceWorkloadCandidate) map[string]any {
	return map[string]any{
		"service_id":    candidate.ID,
		"service_name":  candidate.Name,
		"kind":          candidate.Kind,
		"repo_id":       candidate.RepoID,
		"repo_name":     candidate.RepoName,
		"environment":   candidate.Environment,
		"match_basis":   candidate.MatchBasis,
		"next_selector": map[string]any{"argument": "service_id", "value": candidate.ID},
	}
}
