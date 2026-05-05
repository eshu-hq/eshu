package query

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type repositoryFrameworkAggregate struct {
	signalCount   int
	evidenceKinds map[string]struct{}
	paths         map[string]struct{}
}

func hydrateRepositoryNarrativeFiles(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	files []FileContent,
) ([]FileContent, error) {
	if reader == nil || repoID == "" || len(files) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(files))
	hydrated := make([]FileContent, 0, len(files))
	for _, file := range files {
		relativePath := cleanRepositoryRelativePath(file.RelativePath)
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

func enrichRepositoryStoryResponseWithEvidence(
	response map[string]any,
	semanticOverview map[string]any,
	files []FileContent,
) {
	if len(response) == 0 {
		return
	}

	if frameworkSummary := buildRepositoryFrameworkSummary(semanticOverview, files); len(frameworkSummary) > 0 {
		response["framework_summary"] = frameworkSummary
		appendRepositoryStoryFragment(response, StringVal(frameworkSummary, "story"))
	}

	if documentationOverview := buildRepositoryDocumentationOverview(mapValue(response, "documentation_overview"), files); len(documentationOverview) > 0 {
		response["documentation_overview"] = documentationOverview
		appendRepositoryStoryFragment(response, StringVal(documentationOverview, "story"))
	}

	deploymentOverview := mapValue(response, "deployment_overview")
	if len(deploymentOverview) > 0 {
		if topologySummary := buildRepositoryTopologySummary(deploymentOverview); topologySummary != "" {
			deploymentOverview["topology_summary"] = topologySummary
			appendRepositoryStoryFragment(response, topologySummary)
		}
		response["deployment_overview"] = deploymentOverview
	}

	if supportOverview := buildRepositorySupportOverview(mapValue(response, "support_overview"), response); len(supportOverview) > 0 {
		response["support_overview"] = supportOverview
	}
}

func buildRepositoryFrameworkSummary(
	semanticOverview map[string]any,
	files []FileContent,
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
		evidenceKinds := sortedSetKeys(aggregate.evidenceKinds)
		row := map[string]any{
			"framework":      framework,
			"confidence":     repositoryFrameworkConfidence(aggregate),
			"evidence_kinds": evidenceKinds,
			"signal_count":   aggregate.signalCount,
		}
		if paths := sortedSetKeys(aggregate.paths); len(paths) > 0 {
			row["paths"] = paths
		}
		rows = append(rows, row)
		storyParts = append(
			storyParts,
			fmt.Sprintf(
				"%s (%s via %s)",
				framework,
				StringVal(row, "confidence"),
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
		"story":           "Framework signals suggest " + joinSentenceFragments(storyParts) + ".",
	}
}

func buildRepositoryDocumentationOverview(
	base map[string]any,
	files []FileContent,
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
		relativePath := cleanRepositoryRelativePath(file.RelativePath)
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
		for _, route := range extractDocsRoutes(file.Content) {
			if _, ok := seenRoutes[route]; ok {
				continue
			}
			seenRoutes[route] = struct{}{}
			docRoutes = append(docRoutes, route)
		}
		if spec, ok := extractAPISpecEvidence(file, nil); ok {
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
		storyParts = append(storyParts, "files "+joinSentenceFragments(docFiles))
	}
	if len(specPaths) > 0 {
		storyParts = append(storyParts, "API specs "+joinSentenceFragments(specPaths))
	}
	if len(docRoutes) > 0 {
		storyParts = append(storyParts, "docs routes "+joinSentenceFragments(docRoutes))
	}
	if len(storyParts) > 0 {
		overview["story"] = "Documentation signals include " + joinSentenceFragments(storyParts) + "."
	}
	if len(overview) == 0 {
		return nil
	}
	return overview
}

func isRepositoryAPISpecEvidence(spec ServiceAPISpecEvidence) bool {
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

	frameworkCount := IntVal(mapValue(response, "framework_summary"), "framework_count")
	documentationOverview := mapValue(response, "documentation_overview")
	deploymentOverview := mapValue(response, "deployment_overview")
	topologySignals := stringSliceValue(deploymentOverview, "direct_story")
	if len(topologySignals) == 0 {
		topologySignals = stringSliceValue(deploymentOverview, "topology_story")
	}

	overview["framework_count"] = frameworkCount
	overview["documentation_file_count"] = IntVal(documentationOverview, "documentation_file_count")
	overview["api_spec_count"] = IntVal(documentationOverview, "api_spec_count")
	overview["docs_route_count"] = IntVal(documentationOverview, "docs_route_count")
	overview["topology_signal_count"] = len(topologySignals)
	overview["has_framework_summary"] = frameworkCount > 0
	overview["has_topology_summary"] = StringVal(deploymentOverview, "topology_summary") != ""

	storyParts := make([]string, 0, 5)
	if dependencyCount := IntVal(overview, "dependency_count"); dependencyCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d dependency link(s)", dependencyCount))
	}
	if languageCount := IntVal(overview, "language_count"); languageCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d language family(ies)", languageCount))
	}
	if frameworkCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d framework signal(s)", frameworkCount))
	}
	if documentationFileCount := IntVal(overview, "documentation_file_count"); documentationFileCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d documentation file(s)", documentationFileCount))
	}
	if topologySignalCount := IntVal(overview, "topology_signal_count"); topologySignalCount > 0 {
		storyParts = append(storyParts, fmt.Sprintf("%d topology signal(s)", topologySignalCount))
	}
	if len(storyParts) > 0 {
		overview["story"] = "Support surface spans " + joinSentenceFragments(storyParts) + "."
	}
	return overview
}

func buildRepositoryTopologySummary(deploymentOverview map[string]any) string {
	directStory := stringSliceValue(deploymentOverview, "direct_story")
	if len(directStory) == 0 {
		directStory = stringSliceValue(deploymentOverview, "topology_story")
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

	story := strings.TrimSpace(StringVal(response, "story"))
	if story == "" {
		response["story"] = fragment
		return
	}
	if strings.Contains(story, fragment) {
		return
	}
	response["story"] = story + " " + fragment
}

func collectRepositoryFrameworkSignals(
	file FileContent,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	relativePath := cleanRepositoryRelativePath(file.RelativePath)
	if relativePath == "" {
		return
	}

	base := strings.ToLower(filepath.Base(relativePath))
	content := file.Content
	lowerContent := strings.ToLower(content)

	switch {
	case base == "package.json":
		collectPackageJSONFrameworkSignals(relativePath, content, frameworks)
	case base == "pyproject.toml", base == "pipfile", base == "requirements.txt",
		strings.HasPrefix(base, "requirements-"), strings.HasPrefix(base, "requirements_"), base == "setup.py":
		collectPythonFrameworkSignals(relativePath, lowerContent, "package_dependency", frameworks)
	case strings.HasPrefix(base, "next.config."):
		noteRepositoryFrameworkSignal(frameworks, "nextjs", "config_file", relativePath, 1)
	}

	switch strings.ToLower(filepath.Ext(relativePath)) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		collectJavaScriptFrameworkSignals(relativePath, lowerContent, frameworks)
	case ".py":
		collectPythonFrameworkSignals(relativePath, lowerContent, "source_import", frameworks)
	}
}

func collectPackageJSONFrameworkSignals(
	relativePath string,
	content string,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	type packageManifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	var manifest packageManifest
	if err := json.Unmarshal([]byte(content), &manifest); err == nil {
		for dependency := range manifest.Dependencies {
			noteRepositoryFrameworkFromPackage(frameworks, dependency, relativePath)
		}
		for dependency := range manifest.DevDependencies {
			noteRepositoryFrameworkFromPackage(frameworks, dependency, relativePath)
		}
		return
	}

	lowerContent := strings.ToLower(content)
	for framework, dependency := range map[string]string{
		"express": "express",
		"fastify": "fastify",
		"hapi":    "@hapi/hapi",
		"nextjs":  `"next"`,
		"nestjs":  "@nestjs/core",
		"react":   "react",
		"svelte":  "svelte",
		"vue":     "vue",
	} {
		if strings.Contains(lowerContent, strings.ToLower(dependency)) {
			noteRepositoryFrameworkSignal(frameworks, framework, "package_dependency", relativePath, 1)
		}
	}
}

func noteRepositoryFrameworkFromPackage(
	frameworks map[string]*repositoryFrameworkAggregate,
	dependency string,
	relativePath string,
) {
	switch strings.ToLower(strings.TrimSpace(dependency)) {
	case "react":
		noteRepositoryFrameworkSignal(frameworks, "react", "package_dependency", relativePath, 1)
	case "express":
		noteRepositoryFrameworkSignal(frameworks, "express", "package_dependency", relativePath, 1)
	case "fastify":
		noteRepositoryFrameworkSignal(frameworks, "fastify", "package_dependency", relativePath, 1)
	case "@hapi/hapi", "hapi":
		noteRepositoryFrameworkSignal(frameworks, "hapi", "package_dependency", relativePath, 1)
	case "next":
		noteRepositoryFrameworkSignal(frameworks, "nextjs", "package_dependency", relativePath, 1)
	case "@nestjs/core":
		noteRepositoryFrameworkSignal(frameworks, "nestjs", "package_dependency", relativePath, 1)
	case "vue":
		noteRepositoryFrameworkSignal(frameworks, "vue", "package_dependency", relativePath, 1)
	case "svelte":
		noteRepositoryFrameworkSignal(frameworks, "svelte", "package_dependency", relativePath, 1)
	}
}

func collectJavaScriptFrameworkSignals(
	relativePath string,
	lowerContent string,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	for framework, needle := range map[string]string{
		"express": "express",
		"fastify": "fastify",
		"hapi":    "@hapi/hapi",
		"nestjs":  "@nestjs/core",
		"react":   "react",
		"vue":     "vue",
		"svelte":  "svelte",
	} {
		if strings.Contains(lowerContent, "'"+needle+"'") ||
			strings.Contains(lowerContent, `"`+needle+`"`) ||
			strings.Contains(lowerContent, "require(\""+needle+"\")") ||
			strings.Contains(lowerContent, "require('"+needle+"')") {
			noteRepositoryFrameworkSignal(frameworks, framework, "source_import", relativePath, 1)
		}
	}
}

func collectPythonFrameworkSignals(
	relativePath string,
	lowerContent string,
	evidenceKind string,
	frameworks map[string]*repositoryFrameworkAggregate,
) {
	for framework, needles := range map[string][]string{
		"fastapi": {"fastapi", "from fastapi import", "import fastapi"},
		"flask":   {"flask", "from flask import", "import flask"},
		"django":  {"django", "from django", "import django"},
	} {
		for _, needle := range needles {
			if strings.Contains(lowerContent, needle) {
				noteRepositoryFrameworkSignal(frameworks, framework, evidenceKind, relativePath, 1)
				break
			}
		}
	}
}

func noteRepositoryFrameworkSignal(
	frameworks map[string]*repositoryFrameworkAggregate,
	framework string,
	evidenceKind string,
	relativePath string,
	count int,
) {
	framework = strings.TrimSpace(strings.ToLower(framework))
	evidenceKind = strings.TrimSpace(evidenceKind)
	relativePath = cleanRepositoryRelativePath(relativePath)
	if framework == "" || evidenceKind == "" || count <= 0 {
		return
	}

	aggregate, ok := frameworks[framework]
	if !ok {
		aggregate = &repositoryFrameworkAggregate{
			evidenceKinds: map[string]struct{}{},
			paths:         map[string]struct{}{},
		}
		frameworks[framework] = aggregate
	}
	aggregate.signalCount += count
	aggregate.evidenceKinds[evidenceKind] = struct{}{}
	if relativePath != "" {
		aggregate.paths[relativePath] = struct{}{}
	}
}

func repositoryFrameworkConfidence(aggregate *repositoryFrameworkAggregate) string {
	if aggregate == nil {
		return "low"
	}
	if _, ok := aggregate.evidenceKinds["semantic_entity"]; ok {
		return "high"
	}
	if len(aggregate.evidenceKinds) >= 2 {
		return "high"
	}
	if _, ok := aggregate.evidenceKinds["package_dependency"]; ok {
		return "medium"
	}
	if _, ok := aggregate.evidenceKinds["source_import"]; ok {
		return "medium"
	}
	return "low"
}

func isRepositoryNarrativeCandidate(relativePath string) bool {
	lowerPath := strings.ToLower(cleanRepositoryRelativePath(relativePath))
	if lowerPath == "" {
		return false
	}
	if isRepositoryDocumentationFile(lowerPath) {
		return true
	}
	if isCatalogDescriptorPath(lowerPath) {
		return true
	}
	base := strings.ToLower(filepath.Base(lowerPath))
	switch {
	case base == "package.json",
		base == "pyproject.toml",
		base == "pipfile",
		base == "requirements.txt",
		strings.HasPrefix(base, "requirements-"),
		strings.HasPrefix(base, "requirements_"),
		base == "setup.py",
		strings.HasPrefix(base, "next.config."):
		return true
	}
	if isServiceEvidenceCandidate(FileContent{RelativePath: lowerPath}, "") {
		return true
	}
	switch filepath.Ext(lowerPath) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts", ".py":
		return strings.Contains(base, "app") ||
			strings.Contains(base, "server") ||
			strings.Contains(base, "main") ||
			strings.Contains(base, "index") ||
			strings.Contains(base, "api")
	default:
		return false
	}
}

func isRepositoryDocumentationFile(relativePath string) bool {
	lowerPath := strings.ToLower(cleanRepositoryRelativePath(relativePath))
	if lowerPath == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(lowerPath))
	if strings.HasPrefix(base, "readme.") {
		return true
	}
	if isCatalogDescriptorPath(lowerPath) {
		return true
	}
	if strings.HasPrefix(lowerPath, "docs/") && strings.HasSuffix(lowerPath, ".md") {
		return true
	}
	return false
}

func isCatalogDescriptorPath(relativePath string) bool {
	base := strings.ToLower(filepath.Base(relativePath))
	return base == "catalog-info.yaml" || base == "catalog-info.yml"
}

func stringIntMapValue(value map[string]any, key string) map[string]int {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	typed, ok := raw.(map[string]int)
	if ok {
		return typed
	}
	return nil
}

func cloneStringAnyMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
