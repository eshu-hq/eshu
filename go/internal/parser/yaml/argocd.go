package yaml

import (
	"fmt"
	"sort"
	"strings"
)

type argoApplicationSource struct {
	repoURL        string
	path           string
	targetRevision string
	normalizedRoot string
}

func isArgoCDApplication(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, "argoproj.io/") && kind == "Application"
}

func parseArgoCDApplication(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	destination, _ := spec["destination"].(map[string]any)
	syncPolicy, syncOptions := collectArgoSyncPolicy(spec["syncPolicy"])
	row := map[string]any{
		"name":           strings.TrimSpace(fmt.Sprint(metadata["name"])),
		"line_number":    lineNumber,
		"namespace":      strings.TrimSpace(fmt.Sprint(metadata["namespace"])),
		"project":        strings.TrimSpace(fmt.Sprint(spec["project"])),
		"dest_name":      cleanYAMLString(destination["name"]),
		"dest_server":    cleanYAMLString(destination["server"]),
		"dest_namespace": cleanYAMLString(destination["namespace"]),
		"path":           path,
		"lang":           "yaml",
	}
	appendArgoApplicationSourceFields(row, extractArgoApplicationSources(spec))
	if labels := collectMetadataLabels(metadata); labels != "" {
		row["labels"] = labels
	}
	if syncPolicy != "" {
		row["sync_policy"] = syncPolicy
	}
	if syncOptions != "" {
		row["sync_policy_options"] = syncOptions
	}
	return row
}

func appendArgoApplicationSourceFields(row map[string]any, sources []argoApplicationSource) {
	if len(sources) == 0 {
		row["source_repo"] = ""
		row["source_path"] = ""
		row["source_revision"] = ""
		return
	}

	primary := sources[0]
	row["source_repo"] = primary.repoURL
	row["source_path"] = primary.path
	row["source_revision"] = primary.targetRevision
	if primary.normalizedRoot != "" {
		row["source_root"] = primary.normalizedRoot
	}

	sourceRepos := make([]string, 0, len(sources))
	sourcePaths := make([]string, 0, len(sources))
	sourceRevisions := make([]string, 0, len(sources))
	sourceRoots := make([]string, 0, len(sources))
	for _, source := range sources {
		sourceRepos = append(sourceRepos, source.repoURL)
		sourcePaths = append(sourcePaths, source.path)
		sourceRevisions = append(sourceRevisions, source.targetRevision)
		sourceRoots = append(sourceRoots, source.normalizedRoot)
	}
	if values := dedupeNonEmptyStrings(sourceRepos); len(values) > 0 {
		row["source_repos"] = strings.Join(values, ",")
	}
	if values := dedupeNonEmptyStrings(sourcePaths); len(values) > 0 {
		row["source_paths"] = strings.Join(values, ",")
	}
	if values := dedupeNonEmptyStrings(sourceRevisions); len(values) > 0 {
		row["source_revisions"] = strings.Join(values, ",")
	}
	if values := dedupeNonEmptyStrings(sourceRoots); len(values) > 0 {
		row["source_roots"] = strings.Join(values, ",")
	}
}

func extractArgoApplicationSources(spec map[string]any) []argoApplicationSource {
	sources := make([]argoApplicationSource, 0)
	if source, ok := spec["source"].(map[string]any); ok {
		if parsed := parseArgoApplicationSource(source); parsed.repoURL != "" || parsed.path != "" {
			sources = append(sources, parsed)
		}
	}
	if items, ok := spec["sources"].([]any); ok {
		for _, item := range items {
			source, ok := item.(map[string]any)
			if !ok {
				continue
			}
			parsed := parseArgoApplicationSource(source)
			if parsed.repoURL == "" && parsed.path == "" {
				continue
			}
			sources = append(sources, parsed)
		}
	}
	return sources
}

func parseArgoApplicationSource(source map[string]any) argoApplicationSource {
	sourcePath := cleanYAMLString(source["path"])
	return argoApplicationSource{
		repoURL:        cleanYAMLString(source["repoURL"]),
		path:           sourcePath,
		targetRevision: cleanYAMLString(source["targetRevision"]),
		normalizedRoot: normalizeArgoSourceRoot(sourcePath),
	}
}

func isArgoCDApplicationSet(apiVersion string, kind string) bool {
	return strings.HasPrefix(apiVersion, "argoproj.io/") && kind == "ApplicationSet"
}

func parseArgoCDApplicationSet(document map[string]any, metadata map[string]any, path string, lineNumber int) map[string]any {
	spec, _ := document["spec"].(map[string]any)
	template, _ := spec["template"].(map[string]any)
	templateSpec, _ := template["spec"].(map[string]any)
	generatorTypes := make([]string, 0)
	generatorSourceRepos := make([]string, 0)
	generatorSourcePaths := make([]string, 0)
	if generators, ok := spec["generators"].([]any); ok {
		for _, rawGenerator := range generators {
			generator, ok := rawGenerator.(map[string]any)
			if !ok {
				continue
			}
			collectArgoGeneratorKinds(generator, &generatorTypes)
			collectArgoGeneratorSources(generator, &generatorSourceRepos, &generatorSourcePaths)
		}
	}
	templateSourceRepos, templateSourcePaths := extractArgoTemplateSources(templateSpec)
	sourceRepos := append(append([]string(nil), generatorSourceRepos...), templateSourceRepos...)
	sourcePaths := append(append([]string(nil), generatorSourcePaths...), templateSourcePaths...)
	dedupedPaths := dedupeNonEmptyStrings(sourcePaths)
	generatorRoots := normalizeArgoSourceRoots(generatorSourcePaths)
	templateRoots := normalizeArgoSourceRoots(templateSourcePaths)
	sourceRoots := make([]string, 0, len(dedupedPaths))
	for _, sourcePath := range dedupedPaths {
		if root := normalizeArgoSourceRoot(sourcePath); root != "" {
			sourceRoots = append(sourceRoots, root)
		}
	}
	return map[string]any{
		"name":                   strings.TrimSpace(fmt.Sprint(metadata["name"])),
		"line_number":            lineNumber,
		"namespace":              strings.TrimSpace(fmt.Sprint(metadata["namespace"])),
		"generators":             strings.Join(dedupeAndSortStrings(generatorTypes), ","),
		"project":                strings.TrimSpace(fmt.Sprint(templateSpec["project"])),
		"dest_name":              strings.TrimSpace(fmt.Sprint(nestedMapValue(templateSpec, "destination", "name"))),
		"dest_server":            strings.TrimSpace(fmt.Sprint(nestedMapValue(templateSpec, "destination", "server"))),
		"dest_namespace":         strings.TrimSpace(fmt.Sprint(nestedMapValue(templateSpec, "destination", "namespace"))),
		"source_repos":           strings.Join(dedupeNonEmptyStrings(sourceRepos), ","),
		"source_paths":           strings.Join(dedupedPaths, ","),
		"generator_source_repos": strings.Join(dedupeNonEmptyStrings(generatorSourceRepos), ","),
		"generator_source_paths": strings.Join(dedupeNonEmptyStrings(generatorSourcePaths), ","),
		"generator_source_roots": strings.Join(generatorRoots, ","),
		"template_source_repos":  strings.Join(dedupeNonEmptyStrings(templateSourceRepos), ","),
		"template_source_paths":  strings.Join(dedupeNonEmptyStrings(templateSourcePaths), ","),
		"template_source_roots":  strings.Join(templateRoots, ","),
		"source_roots":           strings.Join(dedupeNonEmptyStrings(sourceRoots), ","),
		"path":                   path,
		"lang":                   "yaml",
	}
}

func normalizeArgoSourceRoots(paths []string) []string {
	roots := make([]string, 0, len(paths))
	for _, sourcePath := range dedupeNonEmptyStrings(paths) {
		if root := normalizeArgoSourceRoot(sourcePath); root != "" {
			roots = append(roots, root)
		}
	}
	return dedupeNonEmptyStrings(roots)
}

func collectArgoGeneratorKinds(generator map[string]any, kinds *[]string) {
	for _, key := range sortedMapKeysAny(generator) {
		value := generator[key]
		if isRecognizedArgoGeneratorKind(key) {
			*kinds = append(*kinds, key)
		}
		switch typed := value.(type) {
		case map[string]any:
			collectArgoGeneratorKinds(typed, kinds)
		case []any:
			for _, item := range typed {
				nested, ok := item.(map[string]any)
				if !ok {
					continue
				}
				collectArgoGeneratorKinds(nested, kinds)
			}
		}
	}
}

func isRecognizedArgoGeneratorKind(kind string) bool {
	switch kind {
	case "clusterDecisionResource", "clusters", "git", "list", "matrix", "merge", "plugin", "pullRequest", "scmProvider":
		return true
	default:
		return false
	}
}

func collectArgoGeneratorSources(generator map[string]any, sourceRepos *[]string, sourcePaths *[]string) {
	gitGenerator, _ := generator["git"].(map[string]any)
	if gitGenerator != nil {
		repoURL := strings.TrimSpace(fmt.Sprint(gitGenerator["repoURL"]))
		if repoURL != "" && !isTemplateOnlyArgoValue(repoURL) {
			*sourceRepos = append(*sourceRepos, repoURL)
		}
		collectArgoGeneratorPaths(gitGenerator["files"], sourcePaths)
		collectArgoGeneratorPaths(gitGenerator["directories"], sourcePaths)
	}

	for _, value := range generator {
		switch typed := value.(type) {
		case map[string]any:
			collectArgoGeneratorSources(typed, sourceRepos, sourcePaths)
		case []any:
			for _, item := range typed {
				nested, ok := item.(map[string]any)
				if !ok {
					continue
				}
				collectArgoGeneratorSources(nested, sourceRepos, sourcePaths)
			}
		}
	}
}

func collectArgoGeneratorPaths(raw any, sourcePaths *[]string) {
	entries, ok := raw.([]any)
	if !ok {
		return
	}
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		path := strings.TrimSpace(fmt.Sprint(object["path"]))
		if path == "" || isTemplateOnlyArgoValue(path) {
			continue
		}
		*sourcePaths = append(*sourcePaths, path)
	}
}

func extractArgoTemplateSources(templateSpec map[string]any) ([]string, []string) {
	sourceRepos := make([]string, 0)
	sourcePaths := make([]string, 0)

	if source, ok := templateSpec["source"].(map[string]any); ok {
		repoURL := strings.TrimSpace(fmt.Sprint(source["repoURL"]))
		sourcePath := strings.TrimSpace(fmt.Sprint(source["path"]))
		if repoURL != "" && !isTemplateOnlyArgoValue(repoURL) {
			sourceRepos = append(sourceRepos, repoURL)
		}
		if sourcePath != "" && !isTemplateOnlyArgoValue(sourcePath) {
			sourcePaths = append(sourcePaths, sourcePath)
		}
	}

	if sources, ok := templateSpec["sources"].([]any); ok {
		for _, rawSource := range sources {
			source, ok := rawSource.(map[string]any)
			if !ok {
				continue
			}
			repoURL := strings.TrimSpace(fmt.Sprint(source["repoURL"]))
			sourcePath := strings.TrimSpace(fmt.Sprint(source["path"]))
			if repoURL != "" && !isTemplateOnlyArgoValue(repoURL) {
				sourceRepos = append(sourceRepos, repoURL)
			}
			if sourcePath != "" && !isTemplateOnlyArgoValue(sourcePath) {
				sourcePaths = append(sourcePaths, sourcePath)
			}
		}
	}

	return sourceRepos, sourcePaths
}

func isTemplateOnlyArgoValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "{{") && strings.HasSuffix(trimmed, "}}")
}

func normalizeArgoSourceRoot(rawPath string) string {
	segments := make([]string, 0)
	for _, segment := range strings.Split(strings.TrimSpace(rawPath), "/") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if segment == "*" || segment == "**" || isTemplateOnlyArgoValue(segment) {
			break
		}
		if strings.HasSuffix(segment, ".yaml") || strings.HasSuffix(segment, ".yml") || strings.HasSuffix(segment, ".json") {
			break
		}
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		return ""
	}
	for index, segment := range segments {
		if segment == "overlays" || segment == "base" {
			if index == 0 {
				return segment + "/"
			}
			return strings.Join(segments[:index], "/") + "/"
		}
	}
	return strings.Join(segments, "/") + "/"
}

func collectArgoSyncPolicy(value any) (string, string) {
	syncPolicy, ok := value.(map[string]any)
	if !ok || len(syncPolicy) == 0 {
		return "", ""
	}

	summaryParts := make([]string, 0, 2)
	if automated, ok := syncPolicy["automated"].(map[string]any); ok {
		automatedParts := make([]string, 0, 3)
		if boolValue(automated["prune"]) {
			automatedParts = append(automatedParts, "prune=true")
		}
		if boolValue(automated["selfHeal"]) {
			automatedParts = append(automatedParts, "selfHeal=true")
		}
		if boolValue(automated["allowEmpty"]) {
			automatedParts = append(automatedParts, "allowEmpty=true")
		}
		if len(automatedParts) == 0 {
			summaryParts = append(summaryParts, "automated")
		} else {
			summaryParts = append(summaryParts, "automated("+strings.Join(automatedParts, ",")+")")
		}
	}

	options := collectArgoSyncOptions(syncPolicy["syncOptions"])
	if len(options) > 0 {
		summaryParts = append(summaryParts, "syncOptions="+strings.Join(options, "|"))
	}

	return strings.Join(summaryParts, ","), strings.Join(options, "|")
}

func collectArgoSyncOptions(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	options := make([]string, 0, len(items))
	for _, item := range items {
		option := strings.TrimSpace(fmt.Sprint(item))
		if option == "" || option == "<nil>" {
			continue
		}
		options = append(options, option)
	}
	sort.Strings(options)
	return options
}

func dedupeAndSortStrings(values []string) []string {
	cleaned := dedupeNonEmptyStrings(values)
	sort.Strings(cleaned)
	return cleaned
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func cleanYAMLString(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}
