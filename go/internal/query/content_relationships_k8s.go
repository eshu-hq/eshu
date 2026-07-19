// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// k8s SELECTS relationship building (Service -> Deployment and its incoming
// counterpart) lives in this file, split out of content_relationships.go to
// keep both files under the repo's 500-line package-file cap.

func buildOutgoingK8sSelectRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
	logger *slog.Logger,
) ([]map[string]any, bool, bool, error) {
	if !isK8sResourceKind(entity, "Service") || entity.EntityName == "" {
		return nil, false, false, nil
	}

	serviceInput := k8sSelectMatchInputFromEntity(entity)
	// A Service with a known, empty selector is genuinely selectorless
	// (ExternalName/manual Endpoints): no SELECTS edge is possible and no
	// fallback applies (see k8sSelectMatch), so skip the query entirely.
	if serviceInput.selectorPresent && serviceInput.selector == "" {
		return nil, true, false, nil
	}

	// Selector matches are not name-scoped (a Service and the Deployment it
	// selects commonly have different names), so candidates come from a
	// repo-wide entity scan rather than a name lookup. The scan is typed
	// (ListRepoEntitiesByType, not ListRepoEntities) so the LIMIT applies to
	// the K8sResource-filtered row set: a repo-wide scan can push late-sorting
	// K8sResource rows past the limit and silently drop them, producing a
	// missing SELECTS edge in a repo with more than repositorySemanticEntityLimit
	// entities. fetchK8sResourceCandidates over-fetches by one row to turn
	// "did we still truncate?" into an exact signal (see its doc comment);
	// truncated is surfaced to the caller for response-level disclosure.
	candidates, truncated, err := fetchK8sResourceCandidates(ctx, reader, entity.RepoID)
	if err != nil {
		return nil, true, false, err
	}

	relationships := make([]map[string]any, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, match := range candidates {
		if match.EntityID == entity.EntityID || !isK8sResourceKind(match, "Deployment") {
			continue
		}
		matched, reason, mixedVintageDrop := k8sSelectMatch(serviceInput, k8sSelectMatchInputFromEntity(match))
		if mixedVintageDrop {
			logK8sSelectMixedVintageDrop(ctx, logger, entity.EntityID, match.EntityID)
		}
		if !matched {
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
			"reason":      reason,
		})
	}

	return relationships, true, truncated, nil
}

func buildIncomingK8sSelectRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
	logger *slog.Logger,
) ([]map[string]any, bool, bool, error) {
	if !isK8sResourceKind(entity, "Deployment") || entity.EntityName == "" {
		return nil, false, false, nil
	}

	workloadInput := k8sSelectMatchInputFromEntity(entity)

	// A matching Service can have any name, so candidates come from a typed
	// repo-wide entity scan (see buildOutgoingK8sSelectRelationships for the
	// same reasoning, including why ListRepoEntitiesByType and not
	// ListRepoEntities, and fetchK8sResourceCandidates for the truncation
	// disclosure over-fetch) rather than a name lookup.
	candidates, truncated, err := fetchK8sResourceCandidates(ctx, reader, entity.RepoID)
	if err != nil {
		return nil, true, false, err
	}

	relationships := make([]map[string]any, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, match := range candidates {
		if match.EntityID == entity.EntityID || !isK8sResourceKind(match, "Service") {
			continue
		}
		matched, reason, mixedVintageDrop := k8sSelectMatch(k8sSelectMatchInputFromEntity(match), workloadInput)
		if mixedVintageDrop {
			logK8sSelectMixedVintageDrop(ctx, logger, match.EntityID, entity.EntityID)
		}
		if !matched {
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
			"reason":      reason,
		})
	}

	return relationships, true, truncated, nil
}

// logK8sSelectMixedVintageDrop fires the Debug-level operator diagnostic for
// a k8sSelectMatch mixed-vintage drop (see k8sSelectMatch's doc comment): the
// Service has a known, matching-eligible selector but the candidate
// Deployment row predates pod_template_labels capture, so no SELECTS edge is
// produced even though one may well exist once the workload is re-ingested.
// Debug, not Warn, because this self-heals on re-ingest and is not itself an
// operator-actionable failure -- it is context for diagnosing an otherwise
// silent, transient missing edge at 3 AM.
func logK8sSelectMixedVintageDrop(ctx context.Context, logger *slog.Logger, serviceEntityID, workloadEntityID string) {
	if logger == nil {
		return
	}
	logger.DebugContext(
		ctx, "k8s SELECTS mixed-vintage drop: candidate workload predates pod_template_labels capture",
		"service_entity_id", serviceEntityID,
		"workload_entity_id", workloadEntityID,
	)
}

// fetchK8sResourceCandidates lists up to repositorySemanticEntityLimit+1
// K8sResource rows for repoID and reports whether the true candidate set
// exceeded the limit. Fetching one extra row turns "did we truncate?" into
// an exact signal: a repo with precisely repositorySemanticEntityLimit
// K8sResource rows is not falsely flagged as truncated, only a repo with
// MORE than the limit is. The extra row (if any) is dropped before the
// candidate list is returned, so callers still see at most
// repositorySemanticEntityLimit rows. See #5343 review (truncation
// disclosure) and follow-up #5367 (real pagination, not yet implemented).
func fetchK8sResourceCandidates(ctx context.Context, reader ContentStore, repoID string) ([]EntityContent, bool, error) {
	candidates, err := reader.ListRepoEntitiesByType(ctx, repoID, "K8sResource", repositorySemanticEntityLimit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list repo entities for k8s selects: %w", err)
	}
	if len(candidates) > repositorySemanticEntityLimit {
		return candidates[:repositorySemanticEntityLimit], true, nil
	}
	return candidates, false, nil
}

func isK8sResourceKind(entity EntityContent, kind string) bool {
	if entity.EntityType != "K8sResource" {
		return false
	}
	value, _ := entity.Metadata["kind"].(string)
	return strings.EqualFold(strings.TrimSpace(value), kind)
}

func k8sNamespace(metadata map[string]any) string {
	value, _ := metadata["namespace"].(string)
	return strings.TrimSpace(value)
}
