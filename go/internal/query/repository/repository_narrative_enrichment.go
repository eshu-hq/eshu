// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

type repositoryFrameworkAggregate struct {
	signalCount   int
	evidenceKinds map[string]struct{}
	paths         map[string]struct{}
}

func hydrateRepositoryNarrativeFiles(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	files []querycontract.FileContent,
) ([]querycontract.FileContent, error) {
	if reader == nil || repoID == "" || len(files) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(files))
	hydrated := make([]querycontract.FileContent, 0, len(files))
	for _, file := range files {
		relativePath := artifacts.CleanRepositoryRelativePath(file.RelativePath)
		if relativePath == "" || !isRepositoryNarrativeCandidate(relativePath) {
			continue
		}
		if _, ok := seen[relativePath]; ok {
			continue
		}
		seen[relativePath] = struct{}{}

		if strings.TrimSpace(file.Content) != "" {
			hydrated = append(hydrated, file)
			continue
		}

		fileContent, err := reader.GetFileContent(ctx, repoID, relativePath)
		if err != nil {
			return nil, fmt.Errorf("get repository narrative file %q: %w", relativePath, err)
		}
		if fileContent == nil {
			continue
		}
		hydrated = append(hydrated, *fileContent)
	}

	sort.Slice(hydrated, func(i, j int) bool {
		return hydrated[i].RelativePath < hydrated[j].RelativePath
	})
	return hydrated, nil
}

func EnrichRepositoryStoryResponseWithEvidence(
	response map[string]any,
	semanticOverview map[string]any,
	files []querycontract.FileContent,
) {
	if len(response) == 0 {
		return
	}

	if frameworkSummary := buildRepositoryFrameworkSummary(semanticOverview, files); len(frameworkSummary) > 0 {
		response["framework_summary"] = frameworkSummary
		appendRepositoryStoryFragment(response, querycontract.StringVal(frameworkSummary, "story"))
	}

	if documentationOverview := buildRepositoryDocumentationOverview(querycontract.MapValue(response, "documentation_overview"), files); len(documentationOverview) > 0 {
		response["documentation_overview"] = documentationOverview
		appendRepositoryStoryFragment(response, querycontract.StringVal(documentationOverview, "story"))
	}

	deploymentOverview := querycontract.MapValue(response, "deployment_overview")
	if len(deploymentOverview) > 0 {
		if topologySummary := buildRepositoryTopologySummary(deploymentOverview); topologySummary != "" {
			deploymentOverview["topology_summary"] = topologySummary
			appendRepositoryStoryFragment(response, topologySummary)
		}
		response["deployment_overview"] = deploymentOverview
	}

	if supportOverview := buildRepositorySupportOverview(querycontract.MapValue(response, "support_overview"), response); len(supportOverview) > 0 {
		response["support_overview"] = supportOverview
	}
}

func buildRepositoryFrameworkSummary(
	semanticOverview map[string]any,
	files []querycontract.FileContent,
) map[string]any {
	frameworks := map[string]*repositoryFrameworkAggregate{}
	for framework, count := range stringIntMapValue(semanticOverview, "framework_counts") {
		noteRepositoryFrameworkSignal(frameworks, framework, "semantic_entity", "", count)
	}
	for _, file := range files {
		collectRepositoryFrameworkSignals(file, frameworks)
	}
	if len(frameworks) == 0 {
		return nil
	}

	names := make([]string, 0, len(frameworks))
	for framework := range frameworks {
		names = append(names, framework)
	}
	sort.Strings(names)

	rows := make([]map[string]any, 0, len(names))
	storyParts := make([]string, 0, len(names))
	for _, framework := range names {
		aggregate := frameworks[framework]
		if aggregate == nil || aggregate.signalCount <= 0 {
			continue
		}
		evidenceKinds := querycontract.SortedSetKeys(aggregate.evidenceKinds)
		row := map[string]any{
			"framework":      framework,
			"confidence":     repositoryFrameworkConfidence(aggregate),
			"evidence_kinds": evidenceKinds,
			"signal_count":   aggregate.signalCount,
		}
		if paths := querycontract.SortedSetKeys(aggregate.paths); len(paths) > 0 {
			row["paths"] = paths
		}
		rows = append(rows, row)
		storyParts = append(
			storyParts,
			fmt.Sprintf(
				"%s (%s via %s)",
				framework,
				querycontract.StringVal(row, "confidence"),
				strings.Join(evidenceKinds, ", "),
			),
		)
	}
	if len(rows) == 0 {
		return nil
	}

	return map[string]any{
		"framework_count": len(rows),
		"frameworks":      rows,
		"story":           "Framework signals suggest " + querycontract.JoinSentenceFragments(storyParts) + ".",
	}
}

func buildRepositoryDocumentationOverview(
	base map[string]any,
	files []querycontract.FileContent,
) map[string]any {
	overview := cloneStringAnyMap(base)
	if overview == nil {
		overview = map[string]any{}
	}

	docFiles := make([]string, 0)
	catalogPaths := make([]string, 0)
	docRoutes := make([]string, 0)
	specPaths := make([]string, 0)
	seenDocFiles := map[string]struct{}{}
	seenCatalog := map[string]struct{}{}
	seenRoutes := map[string]struct{}{}
	seenSpecs := map[string]struct{}{}

	for _, file := range files {
		relativePath := artifacts.CleanRepositoryRelativePath(file.RelativePath)
		if relativePath == "" {
			continue
		}
		if isRepositoryDocumentationFile(relativePath) {
			if _, ok := seenDocFiles[relativePath]; !ok {
				seenDocFiles[relativePath] = struct{}{}
				docFiles = append(docFiles, relativePath)
			}
		}
		if isCatalogDescriptorPath(relativePath) {
			if _, ok := seenCatalog[relativePath]; !ok {
				seenCatalog[relativePath] = struct{}{}
				catalogPaths = append(catalogPaths, relativePath)
			}
		}
		for _, route := range querycontract.ExtractDocsRoutes(file.Content) {
			if _, ok := seenRoutes[route]; ok {
				continue
			}
			seenRoutes[route] = struct{}{}
			docRoutes = append(docRoutes, route)
		}
		if spec, ok := querycontract.ExtractAPISpecEvidenceWithoutRefs(file); ok {
			if isRepositoryAPISpecEvidence(spec) {
				if _, ok := seenSpecs[spec.RelativePath]; !ok {
					seenSpecs[spec.RelativePath] = struct{}{}
					specPaths = append(specPaths, spec.RelativePath)
				}
			}
			for _, route := range spec.DocsRoutes {
				if _, ok := seenRoutes[route]; ok {
					continue
				}
				seenRoutes[route] = struct{}{}
				docRoutes = append(docRoutes, route)
			}
		}
	}

	sort.Strings(docFiles)
	sort.Strings(catalogPaths)
	sort.Strings(docRoutes)
	sort.Strings(specPaths)

	if len(docFiles) > 0 {
		overview["documentation_file_count"] = len(docFiles)
		overview["documentation_files"] = docFiles
	}
	if len(catalogPaths) > 0 {
		overview["catalog_descriptor_paths"] = catalogPaths
	}
	if len(docRoutes) > 0 {
		overview["docs_route_count"] = len(docRoutes)
		overview["docs_routes"] = docRoutes
	}
	if len(specPaths) > 0 {
		overview["api_spec_count"] = len(specPaths)
		overview["api_spec_paths"] = specPaths
	}

	storyParts := make([]string, 0, 3)
	if len(docFiles) > 0 {
		storyParts = append(storyParts, "files "+querycontract.JoinSentenceFragments(docFiles))
	}
	if len(specPaths) > 0 {
		storyParts = append(storyParts, "API specs "+querycontract.JoinSentenceFragments(specPaths))
	}
	if len(docRoutes) > 0 {
		storyParts = append(storyParts, "docs routes "+querycontract.JoinSentenceFragments(docRoutes))
	}
	if len(storyParts) > 0 {
		overview["story"] = "Documentation signals include " + querycontract.JoinSentenceFragments(storyParts) + "."
	}
	if len(overview) == 0 {
		return nil
	}
	return overview
}

func isRepositoryAPISpecEvidence(spec querycontract.ServiceAPISpecEvidence) bool {
	if spec.Parsed {
		return true
	}
	switch strings.ToLower(filepath.Ext(spec.RelativePath)) {
	case ".yaml", ".yml", ".json":
		return strings.Contains(strings.ToLower(spec.RelativePath), "spec")
	default:
		return false
	}
}

func buildRepositorySupportOverview(
	base map[string]any,
	response map[string]any,
) map[string]any {
	overview := cloneStringAnyMap(base)
	if overview == nil {
		overview = map[string]any{}
	}

	frameworkCount := querycontract.IntVal(querycontract.MapValue(response, "framework_summary"), "framework_count")
	documentationOverview := querycontract.MapValue(response, "documentation_overview")
	deploymentOverview := querycontract.MapValue(response, "deployment_overview")
	topologySignals := querycontract.StringSliceValue(deploymentOverview, "direct_story")
	if len(topologySignals) == 0 {
		topologySignals = querycontract.StringSliceValue(deploymentOverview, "topology_story")
	}

	overview["framework_count"] = frameworkCount
	overview["documentation_file_count"] = querycontract.IntVal(documentationOverview, "documentation_file_count")
	overview["api_spec_count"] = querycontract.IntVal(documentationOverview, "api_spec_count")
	overview["docs_route_count"] = querycontract.IntVal(documentationOverview, "docs_route_count")
	overview["topology_signal_count"] = len(topologySignals)
	overview["has_framework_summary"] = frameworkCount > 0
	overview["has_topology_summary"] = querycontract.StringVal(deploymentOverview, "topology_summary") != ""

	storyParts := make([]string, 0, 5)
	if dependencyCount := querycontract.IntVal(overview, "dependency_count"); dependencyCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d dependency link(s)", dependencyCount))
	}
	if languageCount := querycontract.IntVal(overview, "language_count"); languageCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d language family(ies)", languageCount))
	}
	if frameworkCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d framework signal(s)", frameworkCount))
	}
	if documentationFileCount := querycontract.IntVal(overview, "documentation_file_count"); documentationFileCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d documentation file(s)", documentationFileCount))
	}
	if topologySignalCount := querycontract.IntVal(overview, "topology_signal_count"); topologySignalCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d topology signal(s)", topologySignalCount))
	}
	if len(storyParts) > 0 {
		overview["story"] = "Support surface spans " + querycontract.JoinSentenceFragments(storyParts) + "."
	}
	return overview
}

func buildRepositoryTopologySummary(deploymentOverview map[string]any) string {
	directStory := querycontract.StringSliceValue(deploymentOverview, "direct_story")
	if len(directStory) == 0 {
		directStory = querycontract.StringSliceValue(deploymentOverview, "topology_story")
	}
	switch len(directStory) {
	case 0:
		return ""
	case 1:
		return directStory[0]
	case 2:
		return directStory[0] + " " + directStory[1]
	default:
		return fmt.Sprintf("%s %s %d additional deployment signal(s).", directStory[0], directStory[1], len(directStory)-2)
	}
}

func appendRepositoryStoryFragment(response map[string]any, fragment string) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return
	}

	story := strings.TrimSpace(querycontract.StringVal(response, "story"))
	if story == "" {
		response["story"] = fragment
		return
	}
	if strings.Contains(story, fragment) {
		return
	}
	response["story"] = story + " " + fragment
}

// enrichRepositoryStoryResponseWithEvidence keeps the in-package spelling
// after the #6060 export; service stayers name
// EnrichRepositoryStoryResponseWithEvidence.
func enrichRepositoryStoryResponseWithEvidence(
	response map[string]any,
	semanticOverview map[string]any,
	files []querycontract.FileContent,
) {
	EnrichRepositoryStoryResponseWithEvidence(response, semanticOverview, files)
}
