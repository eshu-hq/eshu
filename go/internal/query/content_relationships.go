// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const contentRelationshipLimit = 20

// contentRelationshipFetchLimit is what a capped content lookup asks the store
// for: one row past contentRelationshipLimit, so an over-full lookup is an exact
// signal rather than a silent clip (#7151). capContentLookup trims the extra
// row back off.
const contentRelationshipFetchLimit = contentRelationshipLimit + 1

// capContentLookup trims a lookup fetched with contentRelationshipFetchLimit
// back to contentRelationshipLimit and reports whether the store held more
// rows than the limit. Exactly contentRelationshipLimit rows is complete.
func capContentLookup(rows []EntityContent) ([]EntityContent, bool) {
	if len(rows) > contentRelationshipLimit {
		return rows[:contentRelationshipLimit], true
	}
	return rows, false
}

// contentClip reports what one direction's content build clipped (#7151).
// scan is the k8s SELECTS candidate scan (repositorySemanticEntityLimit),
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

// k8sSelectCandidateScanTruncationReason is the machine-readable disclosure
// reason emitted on the entity-context API/MCP response when a k8s SELECTS
// relationship build's K8sResource candidate scan hits
// repositorySemanticEntityLimit and had to be truncated. It is emitted only
// when truncation actually occurs (see contentRelationshipSet.scanTruncated)
// so every repo under the limit -- every golden-corpus repo included -- gets
// byte-identical responses. Pagination past the limit is deferred to #5367;
// this is disclosure-only so a truncated response is never silently
// presented as complete.
// k8sSelectCandidateScanTruncationReason forwards to
// querycontract.K8sSelectCandidateScanTruncationReason. The implementation
// moved to querycontract for #6060; this alias keeps root callers unchanged.
const k8sSelectCandidateScanTruncationReason = querycontract.K8sSelectCandidateScanTruncationReason

type contentRelationshipSet struct {
	incoming []map[string]any
	outgoing []map[string]any
	// scanTruncated reports whether either the outgoing (Service) or
	// incoming (Deployment) k8s SELECTS candidate scan hit
	// repositorySemanticEntityLimit and was truncated. Only one of the two
	// scans runs per request (outgoing fires for kind=Service, incoming for
	// kind=Deployment), so ORing both is safe and future-proof against that
	// invariant changing.
	scanTruncated bool
	// outgoingTruncated and incomingTruncated report, per direction, that a
	// scan or contentRelationshipLimit lookup clipped neighbours (#7151).
	// scanTruncated is deliberately narrower: only the k8s SELECTS scan.
	outgoingTruncated, incomingTruncated bool
	// outgoingClipType and incomingClipType name the relationship type each
	// direction's clip belongs to, so the code relationships route can scope
	// the flag to a relationship_type filter.
	outgoingClipType, incomingClipType string
}

func buildContentRelationshipSet(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
	logger *slog.Logger,
) (contentRelationshipSet, error) {
	outgoing, outgoingClip, err := buildOutgoingContentRelationships(ctx, reader, entity, logger)
	if err != nil {
		return contentRelationshipSet{}, err
	}

	incoming, incomingClip, err := buildIncomingContentRelationships(ctx, reader, entity, logger)
	if err != nil {
		return contentRelationshipSet{}, err
	}

	return contentRelationshipSet{
		incoming:          incoming,
		outgoing:          outgoing,
		scanTruncated:     outgoingClip.scan || incomingClip.scan,
		outgoingTruncated: outgoingClip.edgeType != "",
		incomingTruncated: incomingClip.edgeType != "",
		outgoingClipType:  outgoingClip.edgeType,
		incomingClipType:  incomingClip.edgeType,
	}, nil
}

func buildOutgoingContentRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
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
	if relationships, ok, truncated, err := buildOutgoingK8sSelectRelationships(ctx, reader, entity, logger); ok || err != nil {
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

	componentNames := metadataStringSlice(entity.Metadata, "jsx_component_usage")
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
	reader ContentStore,
	entity EntityContent,
	logger *slog.Logger,
) ([]map[string]any, contentClip, error) {
	if relationships, ok, truncated, err := buildIncomingK8sSelectRelationships(ctx, reader, entity, logger); ok || err != nil {
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
func buildOutgoingGitHubActionsRelationships(entity EntityContent) ([]map[string]any, bool, error) {
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
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, bool, bool, error) {
	if entity.EntityType != "KustomizeOverlay" {
		return nil, false, false, nil
	}

	patchTargets := metadataStringSlice(entity.Metadata, "patch_targets")
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

// K8s SELECTS relationship building (buildOutgoingK8sSelectRelationships,
// buildIncomingK8sSelectRelationships, fetchK8sResourceCandidates,
// logK8sSelectMixedVintageDrop, isK8sResourceKind) lives in
// content_relationships_k8s.go to keep this file under the repo's 500-line
// package-file cap.

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

// metadataStringSlice extracts a cleaned []string from a metadata map. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func metadataStringSlice(metadata map[string]any, key string) []string {
	return querycontract.MetadataStringSlice(metadata, key)
}

// metadataNonEmptyString extracts a cleaned non-empty string from a metadata
// map. The implementation moved to querycontract for #6060; this wrapper
// keeps root callers unchanged.
func metadataNonEmptyString(metadata map[string]any, key string) (string, bool) {
	return querycontract.MetadataNonEmptyString(metadata, key)
}
