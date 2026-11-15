// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fallback

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/kubernetes"
)

// k8s SELECTS relationship building (Service -> Deployment and its incoming
// counterpart) lives in this file, split out of content.go to
// keep both files under the repo's 500-line package-file cap.

// BuildOutgoingK8sSelectRelationships builds the outgoing SELECTS edges for
// a K8sResource Service, and BuildIncomingK8sSelectRelationships the
// incoming edges for a Deployment. Both are exported so the query root's
// staying regression tests can target the k8s classifiers directly without
// importing this leaf's unexported surface; satellite consumers use
// Builder, which serves both directions.
func BuildOutgoingK8sSelectRelationships(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity querycontract.EntityContent,
	logger *slog.Logger,
) ([]map[string]any, bool, bool, error) {
	if !isK8sResourceKind(entity, "Service") || entity.EntityName == "" {
		return nil, false, false, nil
	}

	serviceInput := kubernetes.SelectMatchInputFromEntity(entity)
	// A Service with a known, empty selector is genuinely selectorless
	// (ExternalName/manual Endpoints): no SELECTS edge is possible and no
	// fallback applies (see kubernetes.SelectMatch), so skip the query entirely.
	if serviceInput.SelectorPresent && serviceInput.Selector == "" {
		return nil, true, false, nil
	}

	// Selector matches are not name-scoped (a Service and the Deployment it
	// selects commonly have different names), so candidates come from a
	// repo-wide entity scan rather than a name lookup. The scan is typed
	// (ListRepoEntitiesByType, not ListRepoEntities) so the LIMIT applies to
	// the K8sResource-filtered row set: a repo-wide scan can push late-sorting
	// K8sResource rows past the limit and silently drop them, producing a
	// missing SELECTS edge in a repo with more than
	// querycontract.RepositorySemanticEntityLimit entities.
	// fetchK8sResourceCandidates over-fetches by one row to turn
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
		matched, reason, mixedVintageDrop := kubernetes.SelectMatch(serviceInput, kubernetes.SelectMatchInputFromEntity(match))
		if mixedVintageDrop {
			kubernetes.LogSelectMixedVintageDrop(ctx, logger, entity.EntityID, match.EntityID)
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

func BuildIncomingK8sSelectRelationships(
	ctx context.Context,
	reader querycontract.ContentRelationshipReader,
	entity querycontract.EntityContent,
	logger *slog.Logger,
) ([]map[string]any, bool, bool, error) {
	if !isK8sResourceKind(entity, "Deployment") || entity.EntityName == "" {
		return nil, false, false, nil
	}

	workloadInput := kubernetes.SelectMatchInputFromEntity(entity)

	// A matching Service can have any name, so candidates come from a typed
	// repo-wide entity scan (see BuildOutgoingK8sSelectRelationships for the
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
		matched, reason, mixedVintageDrop := kubernetes.SelectMatch(kubernetes.SelectMatchInputFromEntity(match), workloadInput)
		if mixedVintageDrop {
			kubernetes.LogSelectMixedVintageDrop(ctx, logger, match.EntityID, entity.EntityID)
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

// fetchK8sResourceCandidates lists up to
// querycontract.RepositorySemanticEntityLimit+1 K8sResource rows for repoID
// and reports whether the true candidate set exceeded the limit. Fetching
// one extra row turns "did we truncate?" into an exact signal: a repo with
// precisely querycontract.RepositorySemanticEntityLimit K8sResource rows is
// not falsely flagged as truncated, only a repo with MORE than the limit
// is. The extra row (if any) is dropped before the candidate list is
// returned, so callers still see at most
// querycontract.RepositorySemanticEntityLimit rows. See #5343 review
// (truncation disclosure) and follow-up #5367 (real pagination, not yet
// implemented).
func fetchK8sResourceCandidates(ctx context.Context, reader querycontract.ContentRelationshipReader, repoID string) ([]querycontract.EntityContent, bool, error) {
	candidates, err := reader.ListRepoEntitiesByType(ctx, repoID, "K8sResource", querycontract.RepositorySemanticEntityLimit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list repo entities for k8s selects: %w", err)
	}
	if len(candidates) > querycontract.RepositorySemanticEntityLimit {
		return candidates[:querycontract.RepositorySemanticEntityLimit], true, nil
	}
	return candidates, false, nil
}

// isK8sResourceKind reports whether entity is a K8sResource of the given
// kind.
func isK8sResourceKind(entity querycontract.EntityContent, kind string) bool {
	return querycontract.IsK8sResourceKind(entity, kind)
}

// The namespace normalization this file used to declare moved to
// kubernetes.Namespace, because SelectCandidateFromEntity needs it
// from there (#6060). Call sites in this package call it directly rather than
// through a wrapper; a second copy would be free to drift, and namespace
// equality gates SELECTS matching.
