package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func isHelmChartFile(filename string) bool {
	lower := strings.ToLower(filename)
	return lower == "chart.yaml" || lower == "chart.yml"
}

func isHelmValuesFile(filename string) bool {
	lower := strings.ToLower(filename)
	return strings.HasPrefix(lower, "values") &&
		(strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml"))
}

func isHelmTemplateManifest(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index, part := range parts {
		if part != "templates" || index == 0 {
			continue
		}
		chartDir := filepath.Join(filepath.Dir(path), "..")
		if fileExists(filepath.Join(chartDir, "Chart.yaml")) ||
			fileExists(filepath.Join(chartDir, "Chart.yml")) {
			return true
		}
	}
	return false
}

func parseHelmChart(path string, source []byte) map[string]any {
	documents, err := DecodeDocuments(string(source))
	if err != nil || len(documents) == 0 {
		return nil
	}
	document, ok := documents[0].(map[string]any)
	if !ok {
		return nil
	}
	dependencies := make([]string, 0)
	dependencyRepositories := make([]string, 0)
	if items, ok := document["dependencies"].([]any); ok {
		for _, item := range items {
			dependency, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(dependency["name"]))
			if name != "" && name != "<nil>" {
				dependencies = append(dependencies, name)
			}
			if repository := normalizeHelmRepositoryRef(fmt.Sprint(dependency["repository"])); repository != "" {
				dependencyRepositories = append(dependencyRepositories, repository)
			}
		}
	}
	sort.Strings(dependencies)
	sort.Strings(dependencyRepositories)
	return map[string]any{
		"name":                    strings.TrimSpace(fmt.Sprint(document["name"])),
		"line_number":             1,
		"version":                 strings.TrimSpace(fmt.Sprint(document["version"])),
		"app_version":             strings.TrimSpace(fmt.Sprint(document["appVersion"])),
		"chart_type":              fallbackString(document["type"], "application"),
		"description":             strings.TrimSpace(fmt.Sprint(document["description"])),
		"dependencies":            strings.Join(dependencies, ","),
		"dependency_repositories": strings.Join(deduplicateStrings(dependencyRepositories), ","),
		"path":                    path,
		"lang":                    "yaml",
	}
}

func parseHelmValues(path string, source []byte) map[string]any {
	documents, err := DecodeDocuments(string(source))
	if err != nil || len(documents) == 0 {
		return nil
	}
	document, ok := documents[0].(map[string]any)
	if !ok {
		return nil
	}
	delete(document, "__eshu_line_number")
	return map[string]any{
		"name":               strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		"line_number":        1,
		"top_level_keys":     strings.Join(sortedMapKeysAny(document), ","),
		"image_repositories": strings.Join(collectHelmImageRepositories(document), ","),
		"path":               path,
		"lang":               "yaml",
	}
}

func fallbackString(value any, fallback string) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func normalizeHelmRepositoryRef(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, `"`)
	if trimmed == "" || trimmed == "<nil>" {
		return ""
	}
	return strings.TrimPrefix(trimmed, "file://")
}

func collectHelmImageRepositories(document map[string]any) []string {
	var repositories []string

	var walk func(parentKey string, value any)
	walk = func(parentKey string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			if strings.EqualFold(parentKey, "image") {
				if repository := normalizeContainerImageRepository(fmt.Sprint(typed["repository"])); repository != "" {
					repositories = append(repositories, repository)
				}
			}
			for key, item := range typed {
				walk(key, item)
			}
		case []any:
			for _, item := range typed {
				walk(parentKey, item)
			}
		}
	}

	walk("", document)
	return deduplicateStrings(repositories)
}

func normalizeContainerImageRepository(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, `"`)
	if trimmed == "" || trimmed == "<nil>" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon > lastSlash {
		trimmed = trimmed[:lastColon]
	}
	return trimmed
}

func deduplicateStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
