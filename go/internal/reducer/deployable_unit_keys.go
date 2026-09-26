// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path/filepath"
	"strings"
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
