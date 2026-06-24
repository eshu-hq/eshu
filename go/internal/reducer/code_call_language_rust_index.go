// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func addRustTraitMethodCandidate(
	candidates map[string]map[string]map[string]struct{},
	repositoryID string,
	item map[string]any,
	entityID string,
) {
	traitName := strings.TrimSpace(anyToString(item["trait_context"]))
	methodName := strings.TrimSpace(anyToString(item["name"]))
	if repositoryID == "" ||
		traitName == "" ||
		methodName == "" ||
		entityID == "" ||
		strings.TrimSpace(anyToString(item["impl_context"])) != "" {
		return
	}
	if _, ok := candidates[repositoryID]; !ok {
		candidates[repositoryID] = make(map[string]map[string]struct{})
	}
	key := traitName + "::" + methodName
	if _, ok := candidates[repositoryID][key]; !ok {
		candidates[repositoryID][key] = make(map[string]struct{})
	}
	candidates[repositoryID][key][entityID] = struct{}{}
}

func uniqueRustTraitMethodCandidates(
	candidates map[string]map[string]map[string]struct{},
) map[string]map[string]string {
	out := make(map[string]map[string]string)
	for repositoryID, methods := range candidates {
		for methodKey, entityIDs := range methods {
			if len(entityIDs) != 1 {
				continue
			}
			if _, ok := out[repositoryID]; !ok {
				out[repositoryID] = make(map[string]string)
			}
			for entityID := range entityIDs {
				out[repositoryID][methodKey] = entityID
			}
		}
	}
	return out
}
