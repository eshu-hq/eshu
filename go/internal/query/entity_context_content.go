// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// githubActionsSourceCacheTruncationReason is the stable partial-truth reason
// shared by HTTP and MCP entity-context responses when the workflow source
// available to the relationship extractor hit the 32 KiB content-store cap.
const githubActionsSourceCacheTruncationReason = "github_actions_source_cache_truncated"

func (h *EntityHandler) getEntityContextFromContent(ctx context.Context, entityID string) (map[string]any, error) {
	if h == nil || h.Content == nil || entityID == "" {
		return nil, nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return nil, nil
	}

	entity, err := h.Content.GetEntityContent(ctx, entityID)
	if err != nil || entity == nil {
		return nil, err
	}
	if !access.allowsRepositoryID(entity.RepoID) {
		return nil, nil
	}

	response := contentEntityToMap(*entity)
	relationshipSet, err := buildContentRelationshipSet(ctx, h.Content, *entity, h.Logger)
	if err != nil {
		return nil, err
	}
	relationships := append([]map[string]any{}, relationshipSet.incoming...)
	relationships = append(relationships, relationshipSet.outgoing...)
	response["relationships"] = relationships
	// The truncation-disclosure fields are emitted ONLY when the K8sResource
	// candidate scan actually truncated (see contentRelationshipSet.scanTruncated
	// and fetchK8sResourceCandidates), never unconditionally: every existing
	// cassette and the B-12 20-repo snapshot stay byte-identical because no
	// golden repo has more than repositorySemanticEntityLimit K8sResource
	// rows. #5367 tracks real pagination past the limit.
	if relationshipSet.scanTruncated {
		response["relationships_complete"] = false
		response["relationships_truncation_reason"] = k8sSelectCandidateScanTruncationReason
		h.reportK8sSelectCandidateScanTruncated(ctx, entityID, entity)
	}
	if entity.Metadata["source_cache_truncated"] == true && isGitHubActionsArtifactPath(*entity) {
		response["relationships_complete"] = false
		response["relationships_truncation_reason"] = githubActionsSourceCacheTruncationReason
		limitations := StringSliceVal(response, "limitations")
		response["limitations"] = append(limitations, githubActionsSourceCacheTruncationReason)
	}
	attachSemanticSummary(response)
	return response, nil
}

// reportK8sSelectCandidateScanTruncated fires the 3 AM operator signal for a
// truncated k8s SELECTS candidate scan: a warn log (always, via h.Logger) and
// the eshu_dp_query_k8s_select_candidate_scan_truncated_total counter (when
// h.Instruments is wired). direction reflects which builder truncated --
// outgoing for a Service, incoming for a Deployment (see
// buildOutgoingK8sSelectRelationships / buildIncomingK8sSelectRelationships,
// content_relationships.go) -- since at most one of the two typed fetches
// runs per request. See #5343 review (truncation disclosure) and follow-up
// #5367 (real pagination, not yet implemented).
func (h *EntityHandler) reportK8sSelectCandidateScanTruncated(ctx context.Context, entityID string, entity *EntityContent) {
	direction := "incoming"
	if isK8sResourceKind(*entity, "Service") {
		direction = "outgoing"
	}
	if h.Logger != nil {
		h.Logger.WarnContext(
			ctx, "k8s SELECTS candidate scan truncated at repository entity limit",
			"entity_id", entityID,
			"repo_id", entity.RepoID,
			"direction", direction,
			"limit", repositorySemanticEntityLimit,
			"reason", k8sSelectCandidateScanTruncationReason,
			"follow_up", "#5367",
		)
	}
	if h.Instruments != nil && h.Instruments.QueryK8sSelectCandidateScanTruncated != nil {
		h.Instruments.QueryK8sSelectCandidateScanTruncated.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrReason("k8s_select_"+direction)))
	}
}
