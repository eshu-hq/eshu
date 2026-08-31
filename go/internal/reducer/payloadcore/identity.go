// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

import "strings"

// RepositoryIDFromReducerScope extracts the repository identity from a reducer
// scope ID, accepting an already-canonical "repository:" scope or a
// "git-repository-scope:" prefix. It returns "" for any other scope shape.
func RepositoryIDFromReducerScope(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if strings.HasPrefix(scopeID, "repository:") {
		return scopeID
	}
	if strings.HasPrefix(scopeID, "git-repository-scope:") {
		return strings.TrimSpace(strings.TrimPrefix(scopeID, "git-repository-scope:"))
	}
	return ""
}

// SupplyChainWorkloadIDsFromPayload collects the workload identities a payload
// names, from an explicit workload_id and from any "workload:" entity key. The
// result is deduplicated and sorted.
func SupplyChainWorkloadIDsFromPayload(payload map[string]any) []string {
	var workloadIDs []string
	if workloadID := PayloadStr(payload, "workload_id"); workloadID != "" {
		workloadIDs = append(workloadIDs, workloadID)
	}
	for _, entityKey := range PayloadOrderedStrings(payload, "entity_keys") {
		if strings.HasPrefix(entityKey, "workload:") {
			workloadIDs = append(workloadIDs, entityKey)
		}
	}
	return UniqueSortedStrings(workloadIDs)
}

// OCIRepositoryID derives an OCI repository identity from payload, preferring an
// explicit repository_id and otherwise composing one from registry and
// repository. It returns "" when neither is available.
func OCIRepositoryID(payload map[string]any) string {
	if repositoryID := PayloadStr(payload, "repository_id"); repositoryID != "" {
		return repositoryID
	}
	registry := PayloadStr(payload, "registry")
	repository := PayloadStr(payload, "repository")
	if registry == "" || repository == "" {
		return ""
	}
	return "oci-registry://" + registry + "/" + repository
}
