// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
)

func normalizeResolvedEntities(entities []map[string]any, limit int) []map[string]any {
	if len(entities) == 0 {
		return nil
	}

	hasStableIdentity := false
	for _, entity := range entities {
		if hasIdentity(entity) {
			hasStableIdentity = true
			break
		}
	}

	deduped := make([]map[string]any, 0, len(entities))
	seen := make(map[string]struct{}, len(entities))
	for _, entity := range entities {
		if hasStableIdentity && isAnonymousContainer(entity) {
			continue
		}
		key := resolvedEntityDedupeKey(entity)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, entity)
	}

	sort.SliceStable(deduped, func(i, j int) bool {
		left := resolveRank(deduped[i])
		right := resolveRank(deduped[j])
		if left != right {
			return left > right
		}
		return resolvedEntityDedupeKey(deduped[i]) < resolvedEntityDedupeKey(deduped[j])
	})

	if limit > 0 && len(deduped) > limit {
		deduped = deduped[:limit]
	}
	return deduped
}

func resolveRank(entity map[string]any) int {
	score := 0
	for _, label := range labelStrings(entity["labels"]) {
		switch label {
		case "Repository":
			score += 1000
		case "Workload":
			score += 950
		case "WorkloadInstance":
			score += 900
		case "CloudResource":
			score += 850
		case "K8sResource", "HelmChart", "HelmValues", "ArgoCDApplication", "ArgoCDApplicationSet", "CloudFormationResource", "TerraformBlock":
			score += 700
		case "Directory":
			score += 100
		default:
			score += 500
		}
	}
	if stringField(entity, "id") != "" {
		score += 50
	}
	if stringField(entity, "repo_id") != "" {
		score += 20
	}
	if stringField(entity, "file_path") != "" {
		score += 10
	}
	return score
}

func hasIdentity(entity map[string]any) bool {
	return stringField(entity, "id") != "" ||
		stringField(entity, "repo_id") != "" ||
		stringField(entity, "file_path") != ""
}

func isAnonymousContainer(entity map[string]any) bool {
	if hasIdentity(entity) {
		return false
	}
	labels := labelStrings(entity["labels"])
	if len(labels) == 0 {
		return true
	}
	for _, label := range labels {
		if label != "Directory" {
			return false
		}
	}
	return true
}

func resolvedEntityDedupeKey(entity map[string]any) string {
	if id := stringField(entity, "id"); id != "" {
		return "id:" + id
	}
	return strings.Join([]string{
		strings.Join(labelStrings(entity["labels"]), ","),
		stringField(entity, "name"),
		stringField(entity, "repo_id"),
		stringField(entity, "file_path"),
	}, "|")
}

// stringField forwards to queryselector.EntityString. The implementation
// moved to queryselector for #6060; this wrapper keeps root callers
// unchanged.
func stringField(entity map[string]any, key string) string {
	return queryselector.EntityString(entity, key)
}

// labelStrings forwards to queryselector.EntityLabelStrings. The
// implementation moved to queryselector for #6060; this wrapper keeps root
// callers unchanged.
func labelStrings(raw any) []string {
	return queryselector.EntityLabelStrings(raw)
}
