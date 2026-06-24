// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"strings"
)

type pythonClassBaseCandidate struct {
	bases []string
	count int
}

func addPythonClassBaseCandidate(
	candidates map[string]map[string]map[string]pythonClassBaseCandidate,
	repositoryID string,
	item map[string]any,
) {
	className := strings.TrimSpace(anyToString(item["name"]))
	bases := codeCallMetadataStringSlice(item, "bases")
	if repositoryID == "" || className == "" || len(bases) == 0 {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]pythonClassBaseCandidate)
	}
	if _, ok := candidates[repositoryID][className]; !ok {
		candidates[repositoryID][className] = make(map[string]pythonClassBaseCandidate)
	}
	key := strings.Join(bases, "\x00")
	candidate := candidates[repositoryID][className][key]
	if candidate.count == 0 {
		candidate.bases = slices.Clone(bases)
	}
	candidate.count++
	candidates[repositoryID][className][key] = candidate
}

func uniquePythonClassBasesByRepo(
	candidates map[string]map[string]map[string]pythonClassBaseCandidate,
) map[string]map[string][]string {
	out := make(map[string]map[string][]string)
	for repositoryID, byClass := range candidates {
		for className, byBases := range byClass {
			if len(byBases) != 1 {
				continue
			}
			for _, candidate := range byBases {
				if candidate.count != 1 {
					continue
				}
				if _, ok := out[repositoryID]; !ok {
					out[repositoryID] = make(map[string][]string)
				}
				out[repositoryID][className] = slices.Clone(candidate.bases)
			}
		}
	}
	return out
}
