package query

import (
	"fmt"
	"sort"
	"strings"
)

func extractDockerComposeConfigPathRows(repoName string, files []FileContent) []map[string]any {
	rows := make([]map[string]any, 0)
	seen := map[string]struct{}{}
	for _, file := range files {
		if !isDockerComposeArtifact(file) {
			continue
		}
		rows = append(rows, dockerComposeConfigPathRows(repoName, file.RelativePath, file.Content, seen)...)
	}
	return rows
}

func dockerComposeConfigPathRows(repoName, relativePath, content string, seen map[string]struct{}) []map[string]any {
	documents, err := decodeYAMLMaps(content)
	if err != nil {
		return nil
	}

	rows := make([]map[string]any, 0)
	for _, document := range documents {
		configFiles := composeNamedFileDefinitions(document["configs"])
		secretFiles := composeNamedFileDefinitions(document["secrets"])
		services, ok := document["services"].(map[string]any)
		if !ok {
			continue
		}
		for _, rawService := range services {
			service, ok := rawService.(map[string]any)
			if !ok {
				continue
			}
			for _, pathValue := range composeEnvFilePaths(service["env_file"]) {
				rows = appendConfigArtifactRow(rows, seen, pathValue, repoName, relativePath, "docker_compose_env_file")
			}
			for _, pathValue := range composeServiceFileRefs(service["configs"], configFiles) {
				rows = appendConfigArtifactRow(rows, seen, pathValue, repoName, relativePath, "docker_compose_config_file")
			}
			for _, pathValue := range composeServiceFileRefs(service["secrets"], secretFiles) {
				rows = appendConfigArtifactRow(rows, seen, pathValue, repoName, relativePath, "docker_compose_secret_file")
			}
		}
	}
	return rows
}

func composeNamedFileDefinitions(value any) map[string]string {
	objects, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	definitions := map[string]string{}
	for name, rawDefinition := range objects {
		definition, ok := rawDefinition.(map[string]any)
		if !ok {
			continue
		}
		pathValue := normalizeComposeLocalConfigPath(fmt.Sprint(definition["file"]))
		if pathValue == "" {
			continue
		}
		definitions[strings.TrimSpace(name)] = pathValue
	}
	return definitions
}

func composeEnvFilePaths(value any) []string {
	paths := make([]string, 0)
	seen := map[string]struct{}{}
	for _, rawValue := range composeSequenceValues(value) {
		switch typed := rawValue.(type) {
		case string:
			paths = appendComposePath(paths, seen, normalizeComposeLocalConfigPath(typed))
		case map[string]any:
			paths = appendComposePath(paths, seen, normalizeComposeLocalConfigPath(fmt.Sprint(typed["path"])))
			paths = appendComposePath(paths, seen, normalizeComposeLocalConfigPath(fmt.Sprint(typed["file"])))
		}
	}
	return paths
}

func composeServiceFileRefs(value any, definitions map[string]string) []string {
	paths := make([]string, 0)
	seen := map[string]struct{}{}
	for _, rawValue := range composeSequenceValues(value) {
		switch typed := rawValue.(type) {
		case string:
			paths = appendComposePath(paths, seen, definitions[strings.TrimSpace(typed)])
			paths = appendComposePath(paths, seen, normalizeComposeLocalConfigPath(typed))
		case map[string]any:
			paths = appendComposePath(paths, seen, definitions[strings.TrimSpace(fmt.Sprint(typed["source"]))])
			paths = appendComposePath(paths, seen, normalizeComposeLocalConfigPath(fmt.Sprint(typed["file"])))
		}
	}
	sort.Strings(paths)
	return paths
}

func appendComposePath(paths []string, seen map[string]struct{}, pathValue string) []string {
	if pathValue == "" {
		return paths
	}
	if _, ok := seen[pathValue]; ok {
		return paths
	}
	seen[pathValue] = struct{}{}
	return append(paths, pathValue)
}

func composeSequenceValues(value any) []any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		return typed
	case []string:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return []any{typed}
	}
}

func normalizeComposeLocalConfigPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "<nil>" {
		return ""
	}
	if len(trimmed) >= 2 {
		if (strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
			(strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'")) {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
	}
	if trimmed == "" ||
		strings.HasPrefix(trimmed, "/") ||
		strings.Contains(trimmed, "://") ||
		strings.Contains(trimmed, "${") {
		return ""
	}
	return cleanRepositoryRelativePath(trimmed)
}
