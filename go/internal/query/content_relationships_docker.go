package query

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"gopkg.in/yaml.v3"
)

func buildOutgoingDockerfileRelationships(entity EntityContent) ([]map[string]any, bool, error) {
	if !looksLikeDockerfileEntity(entity) {
		return nil, false, nil
	}

	relationships := make([]map[string]any, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, label := range mapSliceValue(parser.ExtractDockerfileRuntimeMetadata(entity.SourceCache), "dockerfile_labels") {
		name := strings.ToLower(strings.TrimSpace(StringVal(label, "name")))
		value := strings.TrimSpace(StringVal(label, "value"))
		if value == "" || !isDockerfileSourceLabel(name) {
			continue
		}
		key := name + "|" + value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": value,
			"reason":      "dockerfile_source_label",
		})
	}
	if len(relationships) == 0 {
		return nil, true, nil
	}
	return relationships, true, nil
}

func buildOutgoingDockerComposeRelationships(entity EntityContent) ([]map[string]any, bool, error) {
	if !looksLikeDockerComposeEntity(entity) {
		return nil, false, nil
	}

	var document map[string]any
	if err := yaml.Unmarshal([]byte(entity.SourceCache), &document); err != nil {
		return nil, true, fmt.Errorf("parse docker compose content: %w", err)
	}

	services, ok := document["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return nil, true, nil
	}

	relationships := make([]map[string]any, 0, len(services)*2)
	seen := make(map[string]struct{}, len(services)*2)
	add := func(relationshipType, targetName, reason string) {
		targetName = strings.TrimSpace(targetName)
		if targetName == "" {
			return
		}
		key := relationshipType + "|" + targetName + "|" + reason
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        relationshipType,
			"target_name": targetName,
			"reason":      reason,
		})
	}

	for _, rawService := range services {
		service, ok := rawService.(map[string]any)
		if !ok {
			continue
		}
		switch build := service["build"].(type) {
		case string:
			add("DEPLOYS_FROM", build, "docker_compose_build_context")
		case map[string]any:
			add("DEPLOYS_FROM", StringVal(build, "context"), "docker_compose_build_context")
		}
		add("DEPLOYS_FROM", StringVal(service, "image"), "docker_compose_image")
		for _, dependency := range dockerComposeDependsOnValues(service["depends_on"]) {
			add("DEPENDS_ON", dependency, "docker_compose_depends_on")
		}
	}

	if len(relationships) == 0 {
		return nil, true, nil
	}
	return relationships, true, nil
}

func looksLikeDockerfileEntity(entity EntityContent) bool {
	if strings.EqualFold(strings.TrimSpace(entity.Language), "dockerfile") {
		return true
	}

	base := strings.ToLower(strings.TrimSpace(filepath.Base(entity.RelativePath)))
	return base == "dockerfile" || strings.Contains(base, "dockerfile")
}

func looksLikeDockerComposeEntity(entity EntityContent) bool {
	return isDockerComposeFilename(strings.ToLower(filepath.Base(entity.RelativePath)))
}

func isDockerfileSourceLabel(label string) bool {
	switch label {
	case "org.opencontainers.image.source", "org.label-schema.vcs-url":
		return true
	default:
		return false
	}
}

func dockerComposeDependsOnValues(value any) []string {
	switch typed := value.(type) {
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			dependency, ok := item.(string)
			if !ok {
				continue
			}
			if dependency := strings.TrimSpace(dependency); dependency != "" {
				values = append(values, dependency)
			}
		}
		return values
	case map[string]any:
		values := make([]string, 0, len(typed))
		for key := range typed {
			if dependency := strings.TrimSpace(key); dependency != "" {
				values = append(values, dependency)
			}
		}
		return values
	default:
		return nil
	}
}
