// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ssmConfigPathPattern   = regexp.MustCompile(`(?i)((?:/(?:configd|api)/[A-Za-z0-9._*/-]+))`)
	ssmParameterARNPattern = regexp.MustCompile(`(?i):parameter((?:/(?:configd|api)/[A-Za-z0-9._*/-]+))`)
)

// buildKustomizeIndex keys files by their cleaned repository-relative path.
// When two files clean to the same path the later one in files wins. The
// extractor and the needed-content planner both build the index through it, so
// they agree on which file a kustomize reference resolves to.
func buildKustomizeIndex(files []File) map[string]File {
	index := make(map[string]File, len(files))
	for _, file := range files {
		cleaned := CleanRelativePath(file.RelativePath)
		if cleaned == "" {
			continue
		}
		index[cleaned] = file
	}
	return index
}

// isKustomizationIndexPath reports whether a cleaned index path names a
// kustomization file by base name. It is the selector the kustomize extractor
// walks from.
func isKustomizationIndexPath(cleanedPath string) bool {
	return isKustomizationBaseName(strings.ToLower(path.Base(cleanedPath)))
}

func isKustomizationBaseName(lowerBase string) bool {
	return lowerBase == "kustomization.yaml" || lowerBase == "kustomization.yml" || lowerBase == "kustomization"
}

func extractKustomizeConfigPathRows(repoName string, files []File) []map[string]any {
	index := buildKustomizeIndex(files)

	kustomizationFiles := make([]string, 0)
	for relativePath := range index {
		if isKustomizationIndexPath(relativePath) {
			kustomizationFiles = append(kustomizationFiles, relativePath)
		}
	}
	sort.Strings(kustomizationFiles)

	rows := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, kustomizationPath := range kustomizationFiles {
		for _, resourceFile := range collectKustomizeResourceFiles(index, kustomizationPath) {
			for _, configPath := range extractPolicyDocumentConfigPaths(resourceFile.Content) {
				key := strings.Join([]string{repoName, resourceFile.RelativePath, configPath}, "|")
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				rows = append(rows, map[string]any{
					"path":          configPath,
					"source_repo":   repoName,
					"relative_path": resourceFile.RelativePath,
					"evidence_kind": "kustomize_policy_document_resource",
				})
			}
		}
	}
	return rows
}

func collectKustomizeResourceFiles(index map[string]File, kustomizationPath string) []File {
	resourceFiles := make([]File, 0)
	visitedKustomizations := map[string]struct{}{}
	seenResources := map[string]struct{}{}

	var visitKustomization func(string)
	visitKustomization = func(current string) {
		current = CleanRelativePath(current)
		if current == "" {
			return
		}
		if _, ok := visitedKustomizations[current]; ok {
			return
		}
		visitedKustomizations[current] = struct{}{}

		file, ok := index[current]
		if !ok {
			return
		}
		refs := localKustomizeRefs(file.Content)
		baseDir := path.Dir(current)
		for _, ref := range refs {
			resolved := CleanRelativePath(path.Join(baseDir, ref))
			if resolved == "" {
				continue
			}
			if nextKustomization, ok := resolveKustomizationTarget(index, resolved); ok {
				visitKustomization(nextKustomization)
				continue
			}
			resourceFile, ok := index[resolved]
			if !ok || !isKustomizeResourceFile(resourceFile.RelativePath) {
				continue
			}
			if _, ok := seenResources[resolved]; ok {
				continue
			}
			seenResources[resolved] = struct{}{}
			resourceFiles = append(resourceFiles, resourceFile)
		}
	}

	visitKustomization(kustomizationPath)
	sort.Slice(resourceFiles, func(i, j int) bool {
		return resourceFiles[i].RelativePath < resourceFiles[j].RelativePath
	})
	return resourceFiles
}

func localKustomizeRefs(content string) []string {
	documents, err := DecodeYAMLMaps(content)
	if err != nil {
		return nil
	}

	refs := make([]string, 0)
	for _, document := range documents {
		refs = append(refs, stringSequenceValue(document["resources"])...)
		refs = append(refs, stringSequenceValue(document["bases"])...)
		refs = append(refs, stringSequenceValue(document["components"])...)
		refs = append(refs, patchPathValues(document["patches"])...)
	}

	filtered := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		if ref == "" || strings.Contains(ref, "://") {
			continue
		}
		cleaned := CleanRelativePath(ref)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		filtered = append(filtered, cleaned)
	}
	sort.Strings(filtered)
	return filtered
}

// DecodeYAMLMaps decodes every non-empty document of a multi-document YAML
// stream as a map. The query-side workflow and GitHub Actions readers share
// it with the extractors, so one decoder defines what counts as a document.
func DecodeYAMLMaps(content string) ([]map[string]any, error) {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	documents := make([]map[string]any, 0)
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				return documents, nil
			}
			return nil, fmt.Errorf("decode yaml document: %w", err)
		}
		if len(document) == 0 {
			continue
		}
		documents = append(documents, document)
	}
}

func stringSequenceValue(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text == "" || text == "<nil>" {
			continue
		}
		result = append(result, text)
	}
	return result
}

func patchPathValues(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pathValue := strings.TrimSpace(fmt.Sprint(object["path"]))
		if pathValue == "" || pathValue == "<nil>" {
			continue
		}
		result = append(result, pathValue)
	}
	return result
}

func resolveKustomizationTarget(index map[string]File, resolved string) (string, bool) {
	if file, ok := index[resolved]; ok && isKustomizationFile(file.RelativePath) {
		return resolved, true
	}
	for _, candidate := range []string{"kustomization.yaml", "kustomization.yml", "kustomization"} {
		joined := CleanRelativePath(path.Join(resolved, candidate))
		if _, ok := index[joined]; ok {
			return joined, true
		}
	}
	return "", false
}

func isKustomizeResourceFile(relativePath string) bool {
	lower := strings.ToLower(relativePath)
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json")
}

func isKustomizationFile(relativePath string) bool {
	return isKustomizationBaseName(strings.ToLower(path.Base(relativePath)))
}

func extractPolicyDocumentConfigPaths(content string) []string {
	documents, err := DecodeYAMLMaps(content)
	if err != nil {
		return nil
	}

	paths := make([]string, 0)
	seen := map[string]struct{}{}
	for _, document := range documents {
		walkPolicyDocumentResources(document, func(resource string) {
			configPath := normalizeConfigPath(resource)
			if configPath == "" {
				return
			}
			if _, ok := seen[configPath]; ok {
				return
			}
			seen[configPath] = struct{}{}
			paths = append(paths, configPath)
		})
	}
	sort.Strings(paths)
	return paths
}

func walkPolicyDocumentResources(value any, emit func(string)) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.EqualFold(key, "Resource") {
				for _, resource := range flattenStringValues(child) {
					emit(resource)
				}
			}
			walkPolicyDocumentResources(child, emit)
		}
	case []any:
		for _, child := range typed {
			walkPolicyDocumentResources(child, emit)
		}
	}
}

func flattenStringValues(value any) []string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, flattenStringValues(item)...)
		}
		return values
	default:
		return nil
	}
}

func normalizeConfigPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if match := ssmParameterARNPattern.FindStringSubmatch(value); len(match) == 2 {
		return match[1]
	}
	if match := ssmConfigPathPattern.FindStringSubmatch(value); len(match) == 2 {
		return match[1]
	}
	return ""
}
