package parser

import (
	"path/filepath"
	"strings"
)

func parseRawText(path string, isDependency bool) map[string]any {
	payload := basePayload(path, rawTextLanguageForPath(path), isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}
	if source, err := readSource(path); err == nil {
		metadata := inferContentMetadata(filepath.Clean(path), string(source))
		if metadata.ArtifactType != "" {
			payload["artifact_type"] = metadata.ArtifactType
		}
		if metadata.TemplateDialect != "" {
			payload["template_dialect"] = metadata.TemplateDialect
		}
		payload["iac_relevant"] = metadata.IACRelevant
	}
	return payload
}

func rawTextLanguageForPath(path string) string {
	name := strings.ToLower(filepath.Base(path))
	extensionSet := make(map[string]struct{})
	for _, suffix := range splitSuffixes(path) {
		extensionSet[suffix] = struct{}{}
	}

	if name == "dockerfile" || strings.HasPrefix(name, "dockerfile.") {
		return "dockerfile"
	}
	if name == "jenkinsfile" || strings.HasPrefix(name, "jenkinsfile.") {
		return "groovy"
	}
	if hasAny(extensionSet, ".conf", ".cfg", ".cnf") {
		if hasAny(extensionSet, ".j2", ".jinja", ".jinja2", ".tpl", ".tftpl") {
			return "config_template"
		}
		return "config"
	}
	if hasAny(extensionSet, ".yaml", ".yml") && hasAny(extensionSet, ".j2", ".jinja", ".jinja2") {
		return "yaml_template"
	}
	if hasAny(extensionSet, ".j2", ".jinja", ".jinja2", ".tpl", ".tftpl") {
		return "template"
	}
	return "raw_text"
}

func splitSuffixes(path string) []string {
	base := filepath.Base(path)
	parts := strings.Split(base, ".")
	if len(parts) <= 1 {
		return nil
	}

	suffixes := make([]string, 0, len(parts)-1)
	for i := 1; i < len(parts); i++ {
		suffixes = append(suffixes, "."+strings.ToLower(parts[i]))
	}
	return suffixes
}

func hasAny(values map[string]struct{}, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}
