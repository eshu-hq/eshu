// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// querycontract.RepositorySemanticEntityLimit caps semantic entity reads per repository.
// The value moved to querycontract for #6060 so the impact handler-family
// subpackages can share it without importing this package; this alias keeps
// root callers unchanged.

func buildRepositorySemanticOverview(entities []querycontract.EntityContent) map[string]any {
	return buildRepositorySemanticOverviewWithFiles(entities, nil)
}

func buildRepositorySemanticOverviewWithFiles(
	entities []querycontract.EntityContent,
	files []querycontract.FileContent,
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

// SemanticReadTruncatedReason is the shared limitations/partial_reasons value
// the repository story appends when its bounded semantic entity or file read
// found a row past querycontract.RepositorySemanticEntityLimit. The overview,
// the file list, and every stage that consumes them stay capped at the limit, so
// counts derived from them are lower bounds; this reason says so instead of
// letting the story present a capped list's length as the exact total (#7126).
// It is emitted only when the sentinel row beyond the limit exists, so a
// repository with exactly the limit's rows gets a byte-identical response.
const SemanticReadTruncatedReason = "repository_semantic_read_truncated_at_5000"

// loadRepositorySemanticOverview reads the repository's bounded entity and file
// lists and builds the semantic overview from them. It returns the file list it
// read so the caller can reuse it: the repository story consumes the same
// bounded file list for its infrastructure, deployment, narrative, and CI/CD
// stages, and reading it once instead of once per consumer removes the
// duplicate large-repository read (#7126). The returned slice must be treated
// as read-only.
//
// Each list is read with querycontract.RepositorySemanticEntityLimit+1 rows,
// the extra row being a sentinel: when it comes back the list is clipped to the
// limit, so downstream consumers see exactly the rows they always did, and
// truncated is true so the caller can disclose the cap.
func loadRepositorySemanticOverview(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
) (overview map[string]any, files []querycontract.FileContent, truncated bool, err error) {
	if reader == nil || repoID == "" {
		return nil, nil, false, nil
	}

	const limit = querycontract.RepositorySemanticEntityLimit
	entities, err := reader.ListRepoEntities(ctx, repoID, limit+1)
	if err != nil {
		return nil, nil, false, fmt.Errorf("list repository semantic entities: %w", err)
	}
	files, err = reader.ListRepoFiles(ctx, repoID, limit+1)
	if err != nil {
		return nil, nil, false, fmt.Errorf("list repository semantic files: %w", err)
	}
	if len(entities) > limit {
		entities, truncated = entities[:limit], true
	}
	if len(files) > limit {
		files, truncated = files[:limit], true
	}
	return buildRepositorySemanticOverviewWithFiles(entities, files), files, truncated, nil
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
			fragments = append(fragments, "infrastructure="+querycontract.JoinSentenceFragments(infraValues))
		}
		if artifactValues := renderCountFragments(artifactFamilyCounts); len(artifactValues) > 0 {
			fragments = append(fragments, "artifacts="+querycontract.JoinSentenceFragments(artifactValues))
		}
		if len(fragments) == 0 {
			return ""
		}
		return "Infrastructure coverage: " + querycontract.JoinSentenceFragments(fragments) + "."
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
		querycontract.JoinSentenceFragments(fragments),
	)
	if infraFragments := renderCountFragments(infraFamilyCounts); len(infraFragments) > 0 {
		story += " Infrastructure families: " + querycontract.JoinSentenceFragments(infraFragments) + "."
	}
	if artifactFragments := renderCountFragments(artifactFamilyCounts); len(artifactFragments) > 0 {
		story += " Artifact families: " + querycontract.JoinSentenceFragments(artifactFragments) + "."
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
