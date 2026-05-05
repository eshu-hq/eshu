package query

import (
	"sort"
	"strings"
)

func normalizeResolvedEntities(entities []map[string]any, limit int) []map[string]any {
	if len(entities) == 0 {
		return nil
	}

	hasStableIdentity := false
	for _, entity := range entities {
		if entityHasStableIdentity(entity) {
			hasStableIdentity = true
			break
		}
	}

	deduped := make([]map[string]any, 0, len(entities))
	seen := make(map[string]struct{}, len(entities))
	for _, entity := range entities {
		if hasStableIdentity && entityIsAnonymousContainer(entity) {
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
		left := entityResolveRank(deduped[i])
		right := entityResolveRank(deduped[j])
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

func entityResolveRank(entity map[string]any) int {
	score := 0
	for _, label := range entityLabelStrings(entity["labels"]) {
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
	if entityString(entity, "id") != "" {
		score += 50
	}
	if entityString(entity, "repo_id") != "" {
		score += 20
	}
	if entityString(entity, "file_path") != "" {
		score += 10
	}
	return score
}

func entityHasStableIdentity(entity map[string]any) bool {
	return entityString(entity, "id") != "" ||
		entityString(entity, "repo_id") != "" ||
		entityString(entity, "file_path") != ""
}

func entityIsAnonymousContainer(entity map[string]any) bool {
	if entityHasStableIdentity(entity) {
		return false
	}
	labels := entityLabelStrings(entity["labels"])
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
	if id := entityString(entity, "id"); id != "" {
		return "id:" + id
	}
	return strings.Join([]string{
		strings.Join(entityLabelStrings(entity["labels"]), ","),
		entityString(entity, "name"),
		entityString(entity, "repo_id"),
		entityString(entity, "file_path"),
	}, "|")
}

func entityString(entity map[string]any, key string) string {
	value, _ := entity[key].(string)
	return strings.TrimSpace(value)
}

func entityLabelStrings(raw any) []string {
	switch labels := raw.(type) {
	case []string:
		return labels
	case []any:
		result := make([]string, 0, len(labels))
		for _, label := range labels {
			value, ok := label.(string)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			result = append(result, value)
		}
		return result
	default:
		return nil
	}
}
