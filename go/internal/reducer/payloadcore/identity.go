// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

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

// CloudResourceUID computes the stable CloudResource node identity shared by
// every cloud-provider materialization and edge-projection family (AWS,
// Azure, GCP, security-group, and observability-coverage). The identity
// inputs match the aws_resource fact's StableFactKey inputs so a relationship
// or coverage fact's resolved target identity recomputes the same uid.
func CloudResourceUID(accountID, region, resourceType, resourceID string) string {
	return facts.StableID("CloudResource", map[string]any{
		"account_id":    accountID,
		"region":        region,
		"resource_id":   resourceID,
		"resource_type": resourceType,
	})
}

// NormalizedEntityKey reduces one reducer entity key to its bare identity: the
// value is lowercased and trimmed, and a prefixed key ("repo:acme/web",
// "platform:eks-prod") collapses to the segment after its last colon. A key
// with no colon, or one whose colon is the final character, is returned as-is
// after normalization. It is the shared spelling used to compare entity keys
// that different collectors prefix differently.
func NormalizedEntityKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return ""
	}
	if idx := strings.LastIndex(key, ":"); idx >= 0 && idx < len(key)-1 {
		return strings.TrimSpace(key[idx+1:])
	}
	return key
}
