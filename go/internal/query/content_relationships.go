package query

import (
	"context"
	"fmt"
	"strings"
)

const contentRelationshipLimit = 20

type contentRelationshipSet struct {
	incoming []map[string]any
	outgoing []map[string]any
}

func buildContentRelationshipSet(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) (contentRelationshipSet, error) {
	outgoing, err := buildOutgoingContentRelationships(ctx, reader, entity)
	if err != nil {
		return contentRelationshipSet{}, err
	}

	incoming, err := buildIncomingContentRelationships(ctx, reader, entity)
	if err != nil {
		return contentRelationshipSet{}, err
	}

	return contentRelationshipSet{incoming: incoming, outgoing: outgoing}, nil
}

func buildOutgoingContentRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, error) {
	if relationships, ok, err := buildOutgoingArgoCDRelationships(entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildOutgoingTerraformRelationships(entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildOutgoingGitHubActionsRelationships(entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildOutgoingDockerfileRelationships(entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildOutgoingDockerComposeRelationships(entity); ok || err != nil {
		return relationships, err
	}
	if reader == nil {
		return nil, nil
	}
	if relationships, ok, err := buildOutgoingK8sSelectRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildOutgoingCloudFormationRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildOutgoingKustomizeRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildOutgoingRustImplBlockRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, err
	}

	componentNames := metadataStringSlice(entity.Metadata, "jsx_component_usage")
	if len(componentNames) == 0 {
		return nil, nil
	}

	relationships := make([]map[string]any, 0, len(componentNames))
	seen := make(map[string]struct{}, len(componentNames))
	for _, componentName := range componentNames {
		if componentName == "" {
			continue
		}
		components, err := reader.SearchEntitiesByName(ctx, entity.RepoID, "Component", componentName, contentRelationshipLimit)
		if err != nil {
			return nil, fmt.Errorf("search referenced components: %w", err)
		}
		for _, component := range components {
			if component.EntityID == entity.EntityID {
				continue
			}
			key := component.EntityID + ":" + component.EntityName
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			relationships = append(relationships, map[string]any{
				"type":        "REFERENCES",
				"target_name": component.EntityName,
				"target_id":   component.EntityID,
				"reason":      "jsx_component_usage",
			})
		}
	}

	return relationships, nil
}

func buildIncomingContentRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, error) {
	if relationships, ok, err := buildIncomingK8sSelectRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, err
	}
	if relationships, ok, err := buildIncomingRustImplBlockRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, err
	}

	if entity.EntityType != "Component" || entity.EntityName == "" {
		return nil, nil
	}

	referencing, err := reader.SearchEntitiesReferencingComponent(ctx, entity.RepoID, entity.EntityName, contentRelationshipLimit)
	if err != nil {
		return nil, fmt.Errorf("search referencing entities: %w", err)
	}

	relationships := make([]map[string]any, 0, len(referencing))
	seen := make(map[string]struct{}, len(referencing))
	for _, source := range referencing {
		if source.EntityID == entity.EntityID {
			continue
		}
		key := source.EntityID + ":" + source.EntityName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        "REFERENCES",
			"source_name": source.EntityName,
			"source_id":   source.EntityID,
			"reason":      "jsx_component_usage",
		})
	}

	return relationships, nil
}

func buildOutgoingArgoCDRelationships(entity EntityContent) ([]map[string]any, bool, error) {
	switch entity.EntityType {
	case "ArgoCDApplication":
		return buildOutgoingArgoCDApplicationRelationships(entity), true, nil
	case "ArgoCDApplicationSet":
		return buildOutgoingArgoCDApplicationSetRelationships(entity), true, nil
	default:
		return nil, false, nil
	}
}

func buildOutgoingGitHubActionsRelationships(entity EntityContent) ([]map[string]any, bool, error) {
	metadataRelationships := githubActionsMetadataRelationships(entity.Metadata)
	sourceRelationships := githubActionsSourceRelationships(entity)
	if len(metadataRelationships)+len(sourceRelationships) == 0 {
		return nil, false, nil
	}

	relationships := make([]map[string]any, 0, len(metadataRelationships)+len(sourceRelationships))
	seen := make(map[string]struct{}, len(metadataRelationships)+len(sourceRelationships))
	add := func(relationship githubActionsRelationship) {
		key := relationship.relationshipType + "|" + relationship.targetName + "|" + relationship.reason
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        relationship.relationshipType,
			"target_name": relationship.targetName,
			"reason":      relationship.reason,
		})
	}

	for _, relationship := range metadataRelationships {
		add(relationship)
	}
	for _, relationship := range sourceRelationships {
		add(relationship)
	}

	return relationships, true, nil
}

func buildOutgoingK8sSelectRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, bool, error) {
	if !isK8sResourceKind(entity, "Service") || entity.EntityName == "" {
		return nil, false, nil
	}

	matches, err := reader.SearchEntitiesByName(
		ctx, entity.RepoID, "K8sResource", entity.EntityName, contentRelationshipLimit,
	)
	if err != nil {
		return nil, true, fmt.Errorf("search selected deployments: %w", err)
	}

	relationships := make([]map[string]any, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if match.EntityID == entity.EntityID || !isK8sResourceKind(match, "Deployment") {
			continue
		}
		if !sameK8sNamespace(entity, match) {
			continue
		}
		key := match.EntityID + ":" + match.EntityName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        "SELECTS",
			"target_name": match.EntityName,
			"target_id":   match.EntityID,
			"reason":      "k8s_service_name_namespace",
		})
	}

	return relationships, true, nil
}

func buildOutgoingKustomizeRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, bool, error) {
	if entity.EntityType != "KustomizeOverlay" {
		return nil, false, nil
	}

	patchTargets := metadataStringSlice(entity.Metadata, "patch_targets")
	relationships := make([]map[string]any, 0, len(patchTargets)+8)
	seen := make(map[string]struct{}, len(patchTargets))
	for _, patchTarget := range patchTargets {
		kind, name, ok := splitKustomizePatchTarget(patchTarget)
		if !ok {
			continue
		}
		matches, err := reader.SearchEntitiesByName(
			ctx, entity.RepoID, "K8sResource", name, contentRelationshipLimit,
		)
		if err != nil {
			return nil, true, fmt.Errorf("search kustomize patch targets: %w", err)
		}
		for _, match := range matches {
			if match.EntityID == entity.EntityID || !isK8sResourceKind(match, kind) {
				continue
			}
			key := match.EntityID + ":" + match.EntityName + ":" + kind
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			relationships = append(relationships, map[string]any{
				"type":        "PATCHES",
				"target_name": match.EntityName,
				"target_id":   match.EntityID,
				"reason":      "kustomize_patch_target",
			})
		}
	}

	for _, value := range metadataStringSlice(entity.Metadata, "resource_refs") {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": value,
			"reason":      "kustomize_resource_reference",
		})
	}
	for _, value := range metadataStringSlice(entity.Metadata, "helm_refs") {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": value,
			"reason":      "kustomize_helm_chart_reference",
		})
	}
	for _, value := range metadataStringSlice(entity.Metadata, "image_refs") {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": value,
			"reason":      "kustomize_image_reference",
		})
	}

	return relationships, true, nil
}

func buildIncomingK8sSelectRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, bool, error) {
	if !isK8sResourceKind(entity, "Deployment") || entity.EntityName == "" {
		return nil, false, nil
	}

	matches, err := reader.SearchEntitiesByName(
		ctx, entity.RepoID, "K8sResource", entity.EntityName, contentRelationshipLimit,
	)
	if err != nil {
		return nil, true, fmt.Errorf("search selecting services: %w", err)
	}

	relationships := make([]map[string]any, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if match.EntityID == entity.EntityID || !isK8sResourceKind(match, "Service") {
			continue
		}
		if !sameK8sNamespace(entity, match) {
			continue
		}
		key := match.EntityID + ":" + match.EntityName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        "SELECTS",
			"source_name": match.EntityName,
			"source_id":   match.EntityID,
			"reason":      "k8s_service_name_namespace",
		})
	}

	return relationships, true, nil
}

func isK8sResourceKind(entity EntityContent, kind string) bool {
	if entity.EntityType != "K8sResource" {
		return false
	}
	value, _ := entity.Metadata["kind"].(string)
	return strings.EqualFold(strings.TrimSpace(value), kind)
}

func sameK8sNamespace(left EntityContent, right EntityContent) bool {
	return strings.EqualFold(k8sNamespace(left.Metadata), k8sNamespace(right.Metadata))
}

func k8sNamespace(metadata map[string]any) string {
	value, _ := metadata["namespace"].(string)
	return strings.TrimSpace(value)
}

func splitKustomizePatchTarget(value string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(value), "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	kind := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])
	if kind == "" || name == "" {
		return "", "", false
	}
	return kind, name, true
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	values, ok := metadata[key]
	if !ok {
		return nil
	}

	switch typed := values.(type) {
	case []string:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := cleanMetadataString(item); value != "" {
				items = append(items, value)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			raw, ok := item.(string)
			if !ok {
				continue
			}
			if value := cleanMetadataString(raw); value != "" {
				items = append(items, value)
			}
		}
		return items
	case string:
		items := strings.Split(typed, ",")
		result := make([]string, 0, len(items))
		for _, item := range items {
			if value := cleanMetadataString(item); value != "" {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}

func cleanMetadataString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" {
		return ""
	}
	return value
}

func metadataNonEmptyString(metadata map[string]any, key string) (string, bool) {
	value, ok := metadata[key].(string)
	if !ok {
		return "", false
	}
	value = cleanMetadataString(value)
	if value == "" {
		return "", false
	}
	return value, true
}
