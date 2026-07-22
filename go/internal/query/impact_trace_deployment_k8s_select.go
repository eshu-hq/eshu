// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The impact-trace directed SELECTS candidate scan (#5363) lives here, split
// out of impact_trace_deployment_resources.go to keep both files under the
// repo's 500-line package-file cap.

// k8sSelectCandidatePoolTruncationReason is the machine-readable reason paired
// with k8s_relationships_complete=false in a deployment-trace k8s_resource_limits
// map when the directed SELECTS candidate scan hit the
// repositorySemanticEntityLimit ceiling (#5363; real pagination is #5367). It
// is distinct from the entity-context path's
// k8s_resource_candidate_scan_truncated_at_5000 reason because it describes the
// impact-trace surfaced-pool widening scan, not the per-entity relationship
// build.
const k8sSelectCandidatePoolTruncationReason = "k8s_select_candidate_pool_truncated"

// anchoredDeploymentTarget pairs a prepared match target with the entity ID of
// the anchored Deployment it was built from, so a mixed-vintage drop during the
// directed scan can name the workload in its Debug diagnostic (see
// logK8sSelectMixedVintageDrop). The k8sWorkloadMatchTarget itself stays pure
// (it holds only the matcher input plus the once-parsed pod-template labels).
type anchoredDeploymentTarget struct {
	entityID string
	target   k8sWorkloadMatchTarget
}

// k8sResourceWireRow builds the surfaced-pool map[string]any for one
// K8sResource content row. selector/pod_template_labels presence carries
// tri-state meaning for k8sSelectMatch (see content_relationships_k8s_match.go):
// the key is omitted entirely, never set to "", when the source content row
// lacks it. Shared by the name-anchored phase and the matched-by-ID hydration
// phase so both surfaced-row shapes are byte-identical.
//
// api_version is projected here too, not only by
// collectDeploymentSourceK8sResources (impact_trace_deployment_gitops_helpers.go):
// mergeDeploymentTraceRows (impact_trace_deployment_resources.go) dedups the
// two resource sources by entity_id and keeps whichever row it saw FIRST, so
// a resource entity discovered by BOTH this name-anchored scan and the
// GitOps controller scan would otherwise silently lose its api_version if
// only the GitOps-derived map carried it. expectedArgoCDTrackingIDs
// (#5471 codex P1) needs api_version on every k8sResource it reaches,
// regardless of which discovery path produced the surfaced row.
func k8sResourceWireRow(row EntityContent) map[string]any {
	kind, _ := metadataNonEmptyString(row.Metadata, "kind")
	qualifiedName, _ := metadataNonEmptyString(row.Metadata, "qualified_name")
	images := metadataStringSlice(row.Metadata, "container_images")
	resource := map[string]any{
		"entity_id":        row.EntityID,
		"repo_id":          row.RepoID,
		"entity_name":      row.EntityName,
		"kind":             kind,
		"qualified_name":   qualifiedName,
		"relative_path":    row.RelativePath,
		"container_images": images,
		"namespace":        k8sNamespace(row.Metadata),
		"api_version":      metadataNonEmptyStringValue(row.Metadata, "api_version"),
	}
	if selector, ok := row.Metadata["selector"].(string); ok {
		resource["selector"] = selector
	}
	if podTemplateLabels, ok := row.Metadata["pod_template_labels"].(string); ok {
		resource["pod_template_labels"] = podTemplateLabels
	}
	return resource
}

// fetchK8sSelectMatchedServiceIDs runs the directed SELECTS candidate scan and
// returns the entity IDs of the Services that actually selector-match one of
// the anchored Deployment targets, plus whether the candidate pool was
// truncated at the repositorySemanticEntityLimit ceiling.
//
// It short-circuits (no fetch, no truncation) when there are no anchored
// Deployment targets: there is nothing for a Service to select, so widening is
// unnecessary and the ~12.5 ms cap-case scan is skipped entirely. Candidacy is
// decided in Go (kind == "Service", EqualFold); already-surfaced rows are
// skipped so a name-anchored Service is never double-counted. Matched IDs are
// capped at serviceStoryItemLimit (the same public cap as the surfaced pool),
// and because the candidate scan is ordered, the retained matches are
// deterministic. A candidate that matches no target but triggers a
// mixed-vintage drop is logged once as a Debug operator diagnostic.
func (h *ImpactHandler) fetchK8sSelectMatchedServiceIDs(
	ctx context.Context,
	repoID string,
	targets []anchoredDeploymentTarget,
	surfaced map[string]struct{},
) ([]string, bool, error) {
	if len(targets) == 0 {
		return nil, false, nil
	}

	candidateLimit := repositorySemanticEntityLimit + 1
	candidates, err := h.Content.ListRepoK8sSelectCandidates(ctx, repoID, candidateLimit)
	if err != nil {
		return nil, false, fmt.Errorf("list repo k8s select candidates: %w", err)
	}
	truncated := len(candidates) >= candidateLimit
	if truncated {
		candidates = candidates[:repositorySemanticEntityLimit]
	}

	matched := make([]string, 0, serviceStoryItemLimit)
	seen := make(map[string]struct{}, serviceStoryItemLimit)
	for _, candidate := range candidates {
		if len(matched) >= serviceStoryItemLimit {
			break
		}
		if !strings.EqualFold(candidate.Kind, "Service") {
			continue
		}
		if _, ok := surfaced[candidate.EntityID]; ok {
			continue
		}
		if _, ok := seen[candidate.EntityID]; ok {
			continue
		}
		input := candidate.matchInput()
		matchedTarget := false
		mixedVintageWorkloadID := ""
		for _, target := range targets {
			ok, _, mixedVintageDrop := target.target.Match(input)
			if ok {
				matched = append(matched, candidate.EntityID)
				seen[candidate.EntityID] = struct{}{}
				matchedTarget = true
				break
			}
			if mixedVintageDrop && mixedVintageWorkloadID == "" {
				mixedVintageWorkloadID = target.entityID
			}
		}
		if !matchedTarget && mixedVintageWorkloadID != "" {
			logK8sSelectMixedVintageDrop(ctx, h.Logger, candidate.EntityID, mixedVintageWorkloadID)
		}
	}

	if truncated {
		h.reportK8sSelectCandidatePoolTruncated(ctx, repoID)
	}
	return matched, truncated, nil
}

// reportK8sSelectCandidatePoolTruncated fires the 3 AM operator signal for a
// truncated impact-trace directed SELECTS candidate scan: a warn log (always,
// via h.Logger, carrying repo_id as a log field, never a metric attribute) and
// the eshu_dp_query_k8s_select_candidate_scan_truncated_total counter (when
// h.Instruments is wired). The counter's only label is the bounded reason
// enum, so it stays low-cardinality: repo_id is deliberately kept out of the
// metric to avoid unbounded series. A non-zero rate is the signal that a repo
// outgrew the repositorySemanticEntityLimit ceiling and some SELECTS edges may
// be missing from a deployment-trace response; the response also carries
// k8s_relationships_complete=false with reason k8sSelectCandidatePoolTruncationReason.
// See #5343 (truncation disclosure) and follow-up #5367 (real pagination).
func (h *ImpactHandler) reportK8sSelectCandidatePoolTruncated(ctx context.Context, repoID string) {
	if h.Logger != nil {
		h.Logger.WarnContext(
			ctx, "k8s SELECTS candidate pool truncated at repository entity limit",
			"repo_id", repoID,
			"limit", repositorySemanticEntityLimit,
			"reason", k8sSelectCandidatePoolTruncationReason,
			"follow_up", "#5367",
		)
	}
	if h.Instruments != nil && h.Instruments.QueryK8sSelectCandidateScanTruncated != nil {
		h.Instruments.QueryK8sSelectCandidateScanTruncated.Add(ctx, 1,
			metric.WithAttributes(telemetry.AttrReason(k8sSelectCandidatePoolTruncationReason)))
	}
}
