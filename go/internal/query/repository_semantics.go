package query

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

const repositorySemanticEntityLimit = 5000

func buildRepositorySemanticOverview(entities []EntityContent) map[string]any {
	return buildRepositorySemanticOverviewWithFiles(entities, nil)
}

func buildRepositorySemanticOverviewWithFiles(
	entities []EntityContent,
	files []FileContent,
) map[string]any {
	if len(entities) == 0 {
		return buildRepositoryInfrastructureOverview(nil, files)
	}

	languageCounts := map[string]int{}
	signalCounts := map[string]int{}
	surfaceKindCounts := map[string]int{}
	entityTypeCounts := map[string]int{}
	infraFamilyCounts := map[string]int{}
	frameworkCounts := map[string]int{}
	entityCount := 0

	for _, entity := range entities {
		entityTypeCounts[entity.EntityType]++
		if family := infraFamilyForEntityType(entity.EntityType); family != "" {
			infraFamilyCounts[family]++
		}
		result := map[string]any{
			"labels":   []string{entity.EntityType},
			"language": entity.Language,
			"metadata": entity.Metadata,
			"name":     entity.EntityName,
		}
		profile := buildEntitySemanticProfile(result)
		if len(profile) == 0 {
			continue
		}

		entityCount++
		if entity.Language != "" {
			languageCounts[entity.Language]++
		}
		if surfaceKind, ok := profile["surface_kind"].(string); ok && surfaceKind != "" {
			surfaceKindCounts[surfaceKind]++
		}
		if framework, ok := profile["framework"].(string); ok && framework != "" {
			frameworkCounts[framework]++
		}
		if signals, ok := profile["signals"].([]string); ok {
			for _, signal := range signals {
				if signal != "" {
					signalCounts[signal]++
				}
			}
		}
	}

	if entityCount == 0 {
		return buildRepositoryInfrastructureOverview(nil, files)
	}

	overview := map[string]any{
		"entity_count":        entityCount,
		"language_counts":     languageCounts,
		"framework_counts":    frameworkCounts,
		"signal_counts":       signalCounts,
		"surface_kind_counts": surfaceKindCounts,
		"entity_type_counts":  entityTypeCounts,
		"infra_family_counts": infraFamilyCounts,
	}
	if infraOverview := buildRepositoryInfrastructureOverview(nil, files); infraOverview != nil {
		overview["artifact_family_counts"] = infraOverview["artifact_family_counts"]
		overview["infrastructure_families"] = infraOverview["families"]
	}
	return overview
}

func loadRepositorySemanticOverview(
	ctx context.Context,
	reader ContentStore,
	repoID string,
) (map[string]any, error) {
	if reader == nil || repoID == "" {
		return nil, nil
	}

	entities, err := reader.ListRepoEntities(ctx, repoID, repositorySemanticEntityLimit)
	if err != nil {
		return nil, fmt.Errorf("list repository semantic entities: %w", err)
	}
	files, err := reader.ListRepoFiles(ctx, repoID, repositorySemanticEntityLimit)
	if err != nil {
		return nil, fmt.Errorf("list repository semantic files: %w", err)
	}
	return buildRepositorySemanticOverviewWithFiles(entities, files), nil
}

func buildRepositorySemanticStory(overview map[string]any) string {
	if len(overview) == 0 {
		return ""
	}

	entityCount, _ := overview["entity_count"].(int)
	languageCounts, _ := overview["language_counts"].(map[string]int)
	signalCounts, _ := overview["signal_counts"].(map[string]int)
	surfaceKindCounts, _ := overview["surface_kind_counts"].(map[string]int)
	infraFamilyCounts, _ := overview["infra_family_counts"].(map[string]int)
	artifactFamilyCounts, _ := overview["artifact_family_counts"].(map[string]int)
	if entityCount == 0 && len(infraFamilyCounts) == 0 && len(artifactFamilyCounts) == 0 {
		return ""
	}
	if entityCount == 0 {
		fragments := make([]string, 0, len(infraFamilyCounts)+len(artifactFamilyCounts))
		if infraValues := renderCountFragments(infraFamilyCounts); len(infraValues) > 0 {
			fragments = append(fragments, "infrastructure="+joinSentenceFragments(infraValues))
		}
		if artifactValues := renderCountFragments(artifactFamilyCounts); len(artifactValues) > 0 {
			fragments = append(fragments, "artifacts="+joinSentenceFragments(artifactValues))
		}
		if len(fragments) == 0 {
			return ""
		}
		return "Infrastructure coverage: " + joinSentenceFragments(fragments) + "."
	}

	fragments := make([]string, 0, len(signalCounts)+len(surfaceKindCounts))
	for _, key := range sortedIntMapKeys(signalCounts) {
		fragments = append(fragments, fmt.Sprintf("%s=%d", key, signalCounts[key]))
	}
	for _, key := range sortedIntMapKeys(surfaceKindCounts) {
		fragments = append(fragments, fmt.Sprintf("%s=%d", key, surfaceKindCounts[key]))
	}

	story := fmt.Sprintf(
		"Semantic signals cover %d entity(ies) across %d language(s): %s.",
		entityCount,
		len(languageCounts),
		joinSentenceFragments(fragments),
	)
	if infraFragments := renderCountFragments(infraFamilyCounts); len(infraFragments) > 0 {
		story += " Infrastructure families: " + joinSentenceFragments(infraFragments) + "."
	}
	if artifactFragments := renderCountFragments(artifactFamilyCounts); len(artifactFragments) > 0 {
		story += " Artifact families: " + joinSentenceFragments(artifactFragments) + "."
	}
	return story
}

func sortedIntMapKeys(values map[string]int) []string {
	if len(values) == 0 {
		return nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right string) int {
		return cmp.Compare(left, right)
	})
	return keys
}

func renderCountFragments(values map[string]int) []string {
	keys := sortedIntMapKeys(values)
	if len(keys) == 0 {
		return nil
	}
	fragments := make([]string, 0, len(keys))
	for _, key := range keys {
		fragments = append(fragments, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return fragments
}
