package relationships

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

func isArgoCDGitFileGeneratorPath(rawPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawPath))
	return strings.HasSuffix(lower, ".yaml") ||
		strings.HasSuffix(lower, ".yml") ||
		strings.HasSuffix(lower, ".json")
}

func isArgoTemplateString(raw string) bool {
	return strings.Contains(raw, "{{") || strings.Contains(raw, "}}")
}

func argocdEvaluatedTemplateSources(
	specs []argocdTemplateSourceSpec,
	discovery argocdDiscoveryTarget,
	configRepoID string,
	contentIndex evidenceContentIndex,
) []string {
	if len(specs) == 0 || len(contentIndex) == 0 {
		return nil
	}

	var sources []string
	for _, configFile := range contentIndex[configRepoID] {
		if !argocdGeneratorPathMatches(discovery.path, configFile.path) {
			continue
		}
		params, ok := argocdTemplateParamsFromFile(configFile.path, configFile.content)
		if !ok {
			continue
		}
		for _, spec := range specs {
			repoURL, ok := renderSimpleArgoTemplateString(spec.repoURL, params)
			if !ok || repoURL == "" {
				continue
			}
			sources = append(sources, repoURL)
		}
	}
	return uniqueStrings(sources)
}

func argocdConfigIdentityDeploySources(
	discovery argocdDiscoveryTarget,
	configRepoID string,
	contentIndex evidenceContentIndex,
) []string {
	if len(contentIndex) == 0 {
		return nil
	}

	var sources []string
	for _, configFile := range contentIndex[configRepoID] {
		if !argocdGeneratorPathMatches(discovery.path, configFile.path) {
			continue
		}
		params, ok := argocdTemplateParamsFromFile(configFile.path, configFile.content)
		if !ok {
			continue
		}
		sources = append(sources, argocdServiceIdentityValues(params)...)
	}
	return uniqueStrings(sources)
}

func argocdServiceIdentityValues(params map[string]string) []string {
	identityKeys := []string{
		"addon",
		"app",
		"application",
		"name",
		"service",
		"service.name",
		"serviceName",
		"helm.releaseName",
		"releaseName",
		"metadata.name",
	}
	values := make([]string, 0, len(identityKeys))
	for _, key := range identityKeys {
		value := strings.TrimSpace(params[key])
		if value == "" || isBroadArgoServiceIdentity(value) {
			continue
		}
		values = append(values, value)
	}
	return values
}

func isBroadArgoServiceIdentity(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "api", "app", "application", "backend", "frontend", "platform", "service", "server", "worker":
		return true
	default:
		return len(normalized) < 4
	}
}

func argocdGeneratorPathMatches(pattern, candidate string) bool {
	pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "/")
	candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "/")
	if pattern == "" || candidate == "" {
		return false
	}
	if strings.Contains(pattern, "**") {
		return argocdRecursivePathMatch(pattern, candidate)
	}
	if ok, err := path.Match(pattern, candidate); err == nil && ok {
		return true
	}
	return pattern == candidate
}

func argocdRecursivePathMatch(pattern, candidate string) bool {
	patternParts := strings.Split(path.Clean(pattern), "/")
	candidateParts := strings.Split(path.Clean(candidate), "/")
	return argocdRecursivePathMatchParts(patternParts, candidateParts)
}

func argocdRecursivePathMatchParts(patternParts, candidateParts []string) bool {
	if len(patternParts) == 0 {
		return len(candidateParts) == 0
	}
	if patternParts[0] == "**" {
		if argocdRecursivePathMatchParts(patternParts[1:], candidateParts) {
			return true
		}
		for index := range candidateParts {
			if argocdRecursivePathMatchParts(patternParts[1:], candidateParts[index+1:]) {
				return true
			}
		}
		return false
	}
	if len(candidateParts) == 0 {
		return false
	}
	matched, err := path.Match(patternParts[0], candidateParts[0])
	if err != nil || !matched {
		return false
	}
	return argocdRecursivePathMatchParts(patternParts[1:], candidateParts[1:])
}

func argocdTemplateParamsFromFile(filePath, content string) (map[string]string, bool) {
	if !isArgoCDGitFileGeneratorPath(filePath) {
		return nil, false
	}
	var parsed any
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, false
	}
	params := make(map[string]string)
	flattenArgoTemplateParams("", parsed, params)
	addArgoPathParams(filePath, params)
	return params, len(params) > 0
}

func flattenArgoTemplateParams(prefix string, value any, params map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			nextPrefix := key
			if prefix != "" {
				nextPrefix = prefix + "." + key
			}
			flattenArgoTemplateParams(nextPrefix, item, params)
		}
	case []any:
		return
	default:
		text := strings.TrimSpace(stringValueFromAny(typed))
		if prefix == "" || text == "" {
			return
		}
		params[prefix] = text
	}
}

func addArgoPathParams(filePath string, params map[string]string) {
	cleanPath := strings.TrimPrefix(path.Clean(strings.TrimSpace(filePath)), "/")
	if cleanPath == "." || cleanPath == "" {
		return
	}
	dir := path.Dir(cleanPath)
	base := path.Base(dir)
	if dir == "." {
		base = strings.TrimSuffix(path.Base(cleanPath), path.Ext(cleanPath))
	}
	params["path.path"] = cleanPath
	params["path.basename"] = base
	params["path.basenameNormalized"] = normalizePlatformToken(base)
}

var simpleArgoTemplatePattern = regexp.MustCompile(`{{\s*\.([A-Za-z0-9_.]+)\s*}}`)

func renderSimpleArgoTemplateString(raw string, params map[string]string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var unresolved bool
	rendered := simpleArgoTemplatePattern.ReplaceAllStringFunc(raw, func(match string) string {
		parts := simpleArgoTemplatePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			unresolved = true
			return match
		}
		value := strings.TrimSpace(params[parts[1]])
		if value == "" {
			unresolved = true
			return match
		}
		return value
	})
	if unresolved || isArgoTemplateString(rendered) {
		return "", false
	}
	return strings.TrimSpace(rendered), true
}

func stringValueFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int, int64, float64, bool:
		return fmt.Sprint(typed)
	default:
		return ""
	}
}
