// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"path/filepath"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

func deployableUnitKeyFromPath(repoName, relativePath string) string {
	trimmedPath := strings.TrimSpace(relativePath)
	if trimmedPath == "" {
		return repoName
	}
	base := filepath.Base(trimmedPath)
	lowerBase := strings.ToLower(base)
	switch {
	case strings.EqualFold(base, "Dockerfile"):
		return dockerfileExactPathKey(repoName, trimmedPath)
	case strings.HasSuffix(lowerBase, ".dockerfile"):
		return strings.TrimSuffix(base, filepath.Ext(base))
	case strings.HasPrefix(lowerBase, "dockerfile."):
		return strings.TrimPrefix(base, "Dockerfile.")
	default:
		return repoName
	}
}

func boundedAmbiguousDeployableUnitConfidence(confidence float64) float64 {
	if confidence > 0.79 {
		return 0.79
	}
	return confidence
}

func deployableUnitMatchesPrimaryIdentity(candidate WorkloadCandidate, unitKey string) bool {
	unitKey = strings.ToLower(strings.TrimSpace(unitKey))
	if unitKey == "" {
		return false
	}
	if unitKey == strings.ToLower(strings.TrimSpace(candidate.RepoName)) {
		return true
	}
	if unitKey == strings.ToLower(strings.TrimSpace(candidate.WorkloadName)) {
		return true
	}
	return false
}

func deployableUnitKeys(candidate WorkloadCandidate) []string {
	keys := make(map[string]struct{})
	for _, provenance := range candidate.Provenance {
		if !strings.HasPrefix(provenance, "dockerfile_runtime:") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(provenance, "dockerfile_runtime:"))
		key := deployableUnitKeyFromPath(candidate.RepoName, path)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	if len(keys) == 0 {
		return []string{candidate.RepoName}
	}
	values := make([]string, 0, len(keys))
	for key := range keys {
		values = append(values, key)
	}
	return uniqueSortedStrings(values)
}

func deployableUnitCorrelationEntityKeys(intent reducercontract.Intent) (map[string]struct{}, error) {
	entityKeys := uniqueSortedStrings(intent.EntityKeys)
	if len(entityKeys) == 0 {
		return nil, fmt.Errorf(
			"deployable unit correlation intent %q must include at least one entity key",
			intent.IntentID,
		)
	}

	normalized := make(map[string]struct{}, len(entityKeys))
	for _, key := range entityKeys {
		raw := strings.ToLower(strings.TrimSpace(key))
		if raw != "" {
			normalized[raw] = struct{}{}
		}
		if alias := normalizedEntityKey(key); alias != "" {
			normalized[alias] = struct{}{}
		}
	}
	return normalized, nil
}

func candidateIdentityKeys(candidate WorkloadCandidate) []string {
	keys := make([]string, 0, 2)
	appendCandidateIdentityKey := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for _, existing := range keys {
			if existing == value {
				return
			}
		}
		keys = append(keys, value)
	}

	appendCandidateIdentityKey(candidate.RepoID)
	appendCandidateIdentityKey(normalizedEntityKey(candidate.RepoID))

	return keys
}

// normalizedEntityKey forwards to [payloadcore.NormalizedEntityKey]. The helper
// moved to the shared tier with the platform family (#6061), which compares the
// same entity keys when it picks a workload-replay anchor.
func normalizedEntityKey(key string) string {
	return payloadcore.NormalizedEntityKey(key)
}
