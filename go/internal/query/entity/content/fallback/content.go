// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fallback

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const contentRelationshipLimit = 20

// contentRelationshipFetchLimit is what a capped content lookup asks the
// store for: one row past contentRelationshipLimit, so an over-full lookup
// is an exact signal rather than a silent clip (#7151). capContentLookup
// trims the extra row back off.
const contentRelationshipFetchLimit = contentRelationshipLimit + 1

// capContentLookup trims a lookup fetched with contentRelationshipFetchLimit
// back to contentRelationshipLimit and reports whether the store held more
// rows than the limit. Exactly contentRelationshipLimit rows is complete.
func capContentLookup(rows []querycontract.EntityContent) ([]querycontract.EntityContent, bool) {
	if len(rows) > contentRelationshipLimit {
		return rows[:contentRelationshipLimit], true
	}
	return rows, false
}

// contentClip reports what one direction's content build clipped (#7151).
// scan is the k8s SELECTS candidate scan (querycontract.RepositorySemanticEntityLimit),
// which alone feeds the entity route's k8s telemetry. edgeType names the
// relationship type whose neighbours were clipped -- by that scan or by a
// contentRelationshipLimit lookup -- and is empty when nothing was clipped, so
// a relationship_type filter can tell whether it hides the clip.
type contentClip struct {
	scan     bool
	edgeType string
}

// lookupClip is the clip a contentRelationshipLimit lookup reports for
// edgeType, or no clip.
func lookupClip(clipped bool, edgeType string) contentClip {
	if !clipped {
		return contentClip{}
	}
	return contentClip{edgeType: edgeType}
}

// buildOutgoingContentRelationships and buildIncomingContentRelationships
// are the two directions Builder serves. Each classifier either handles the
// entity (returning ok=true) or declines it so the chain proceeds; reader
// is nil in unit tests that only exercise source-local classifiers.
func buildOutgoingContentRelationships(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity querycontract.EntityContent,
	logger *slog.Logger,
) ([]map[string]any, contentClip, error) {
	if relationships, ok, err := buildOutgoingArgoCDRelationships(entity); ok || err != nil {
		return relationships, contentClip{}, err
	}
	if relationships, ok, err := deployment.BuildOutgoingTerraformRelationships(entity); ok || err != nil {
		return relationships, contentClip{}, err
	}
	if relationships, ok, err := buildOutgoingGitHubActionsRelationships(entity); ok || err != nil {
		return relationships, contentClip{}, err
	}
	if relationships, ok, err := buildOutgoingDockerfileRelationships(entity); ok || err != nil {
		return relationships, contentClip{}, err
	}
	if relationships, ok, err := buildOutgoingDockerComposeRelationships(entity); ok || err != nil {
		return relationships, contentClip{}, err
	}
	if reader == nil {
		return nil, contentClip{}, nil
	}
	if relationships, ok, truncated, err := BuildOutgoingK8sSelectRelationships(ctx, reader, entity, logger); ok || err != nil {
		return relationships, selectsClip(truncated), err
	}
	if relationships, ok, err := buildOutgoingCloudFormationRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, contentClip{}, err
	}
	if relationships, ok, truncated, err := buildOutgoingKustomizeRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, lookupClip(truncated, "PATCHES"), err
	}
	if relationships, ok, truncated, err := buildOutgoingRustImplBlockRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, lookupClip(truncated, "CONTAINS"), err
	}

	componentNames := querycontract.MetadataStringSlice(entity.Metadata, "jsx_component_usage")
	if len(componentNames) == 0 {
		return nil, contentClip{}, nil
	}

	relationships := make([]map[string]any, 0, len(componentNames))
	seen := make(map[string]struct{}, len(componentNames))
	clipped := false
	for _, componentName := range componentNames {
		if componentName == "" {
			continue
		}
		components, err := reader.SearchEntitiesByName(ctx, entity.RepoID, "Component", componentName, contentRelationshipFetchLimit)
		if err != nil {
			return nil, contentClip{}, fmt.Errorf("search referenced components: %w", err)
		}
		components, lookupClipped := capContentLookup(components)
		clipped = clipped || lookupClipped
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

	return relationships, lookupClip(clipped, "REFERENCES"), nil
}

func buildIncomingContentRelationships(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity querycontract.EntityContent,
	logger *slog.Logger,
) ([]map[string]any, contentClip, error) {
	if relationships, ok, truncated, err := BuildIncomingK8sSelectRelationships(ctx, reader, entity, logger); ok || err != nil {
		return relationships, selectsClip(truncated), err
	}
	if relationships, ok, truncated, err := buildIncomingRustImplBlockRelationships(ctx, reader, entity); ok || err != nil {
		return relationships, lookupClip(truncated, "CONTAINS"), err
	}

	if entity.EntityType != "Component" || entity.EntityName == "" {
		return nil, contentClip{}, nil
	}

	referencing, err := reader.SearchEntitiesReferencingComponent(ctx, entity.RepoID, entity.EntityName, contentRelationshipFetchLimit)
	if err != nil {
		return nil, contentClip{}, fmt.Errorf("search referencing entities: %w", err)
	}
	referencing, clipped := capContentLookup(referencing)

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

	return relationships, lookupClip(clipped, "REFERENCES"), nil
}

func buildOutgoingArgoCDRelationships(entity querycontract.EntityContent) ([]map[string]any, bool, error) {
	switch entity.EntityType {
	case "ArgoCDApplication":
		return buildOutgoingArgoCDApplicationRelationships(entity), true, nil
	case "ArgoCDApplicationSet":
		return buildOutgoingArgoCDApplicationSetRelationships(entity), true, nil
	default:
		return nil, false, nil
	}
}

// buildOutgoingGitHubActionsRelationships derives DEPLOYS_FROM,
// DISCOVERS_CONFIG_IN, and DEPENDS_ON edges for a GitHub Actions workflow or
// composite-action file from a single source:
//
//   - githubActionsSourceRelationships: a structured YAML decode of
//     entity.SourceCache (the only production signal path -- SourceCache is
//     populated). This replaced the former YAML-unaware raw-text line scanner
//     (issue #5337 Detector 4), which mistook a `uses:` line inside a `run: |`
//     block scalar for a real step key and fabricated edges.
//
// A parallel entity.Metadata decode (githubActionsMetadataRelationships)
// existed alongside the source path but read structured keys no production
// path ever wrote into content_entities.metadata, so it emitted nothing while
// duplicating the source decode and risking stale-JSONB resurrection of
// fabricated edges. #5377 removed that dead path; SourceCache is now the sole
// source.
//
// The single source path still emits duplicate (type, target, reason) tuples
// when a reference recurs in a file (the same action used in two steps, the
// same repository checked out in two jobs), so edges are deduped by
// (type, target, reason). When the source path yields nothing this returns
// (nil, false, nil) so the classifier chain proceeds to later classifiers
// (issue #5337, codex P1 on PR #5379).
func buildOutgoingGitHubActionsRelationships(entity querycontract.EntityContent) ([]map[string]any, bool, error) {
	sourceRelationships := githubActionsSourceRelationships(entity)
	if len(sourceRelationships) == 0 {
		return nil, false, nil
	}

	relationships := make([]map[string]any, 0, len(sourceRelationships))
	seen := make(map[string]struct{}, len(sourceRelationships))
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

	for _, relationship := range sourceRelationships {
		add(relationship)
	}

	return relationships, true, nil
}

func buildOutgoingKustomizeRelationships(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity querycontract.EntityContent,
) ([]map[string]any, bool, bool, error) {
	if entity.EntityType != "KustomizeOverlay" {
		return nil, false, false, nil
	}

	patchTargets := querycontract.MetadataStringSlice(entity.Metadata, "patch_targets")
	relationships := make([]map[string]any, 0, len(patchTargets)+8)
	seen := make(map[string]struct{}, len(patchTargets))
	clipped := false
	for _, patchTarget := range patchTargets {
		kind, name, ok := splitKustomizePatchTarget(patchTarget)
		if !ok {
			continue
		}
		matches, err := reader.SearchEntitiesByName(
			ctx, entity.RepoID, "K8sResource", name, contentRelationshipFetchLimit,
		)
		if err != nil {
			return nil, true, false, fmt.Errorf("search kustomize patch targets: %w", err)
		}
		matches, lookupClipped := capContentLookup(matches)
		clipped = clipped || lookupClipped
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

	for _, value := range querycontract.MetadataStringSlice(entity.Metadata, "resource_refs") {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": value,
			"reason":      "kustomize_resource_reference",
		})
	}
	for _, value := range querycontract.MetadataStringSlice(entity.Metadata, "helm_refs") {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": value,
			"reason":      "kustomize_helm_chart_reference",
		})
	}
	for _, value := range querycontract.MetadataStringSlice(entity.Metadata, "image_refs") {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": value,
			"reason":      "kustomize_image_reference",
		})
	}

	return relationships, true, clipped, nil
}

// selectsClip is the clip a k8s SELECTS candidate scan reports: the scan
// flag the entity route's telemetry reads, and the SELECTS edge type the
// code relationships route scopes its flag by.
func selectsClip(truncated bool) contentClip {
	if !truncated {
		return contentClip{}
	}
	return contentClip{scan: true, edgeType: "SELECTS"}
}

// K8s SELECTS relationship building (BuildOutgoingK8sSelectRelationships,
// buildIncomingK8sSelectRelationships, fetchK8sResourceCandidates,
// isK8sResourceKind) lives in content_k8s.go to keep both
// files under the repo's 500-line package-file cap.

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
