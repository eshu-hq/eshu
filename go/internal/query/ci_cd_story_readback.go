// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

const cicdStoryRunCorrelationLimit = 20

func loadRepositoryScopedCICDEvidence(
	ctx context.Context,
	content ContentStore,
	correlations CICDRunCorrelationStore,
	repositoryID string,
) (map[string]any, error) {
	if repositoryID == "" {
		return nil, nil
	}
	static := (&CICDHandler{Content: content}).staticWorkflowArtifactEvidence(ctx, repositoryID)
	if correlations == nil {
		summary := buildCICDRunCorrelationEvidenceSummary(static, nil, false, true)
		return cicdRunCorrelationEvidenceSummaryMap(summary), nil
	}

	rows, err := correlations.ListCICDRunCorrelations(ctx, CICDRunCorrelationFilter{
		RepositoryID: repositoryID,
		Limit:        cicdStoryRunCorrelationLimit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("load repository ci/cd run correlations: %w", err)
	}
	truncated := len(rows) > cicdStoryRunCorrelationLimit
	if truncated {
		rows = rows[:cicdStoryRunCorrelationLimit]
	}
	results := make([]CICDRunCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, CICDRunCorrelationResult(row))
	}
	summary := buildCICDRunCorrelationEvidenceSummary(static, results, truncated, false)
	return cicdRunCorrelationEvidenceSummaryMap(summary), nil
}

func cicdRunCorrelationEvidenceSummaryMap(summary cicdRunCorrelationEvidenceSummary) map[string]any {
	out := map[string]any{
		"static_workflow_artifacts": cicdStaticWorkflowArtifactEvidenceMap(summary.StaticWorkflowArtifacts),
		"live_run_correlations":     cicdLiveRunCorrelationEvidenceMap(summary.LiveRunCorrelations),
		"run_artifact_evidence":     cicdRunArtifactEvidenceMap(summary.RunArtifactEvidence),
	}
	if summary.Reason != "" {
		out["reason"] = summary.Reason
	}
	return out
}

func cicdStaticWorkflowArtifactEvidenceMap(value cicdStaticWorkflowArtifactEvidence) map[string]any {
	out := map[string]any{
		"state": value.State,
		"count": value.Count,
	}
	if len(value.Paths) > 0 {
		out["paths"] = append([]string(nil), value.Paths...)
	}
	if value.Truncated {
		out["truncated"] = true
	}
	if value.ImageRefCount > 0 {
		out["image_ref_count"] = value.ImageRefCount
	}
	if value.UnresolvedCount > 0 {
		out["unresolved_count"] = value.UnresolvedCount
	}
	if value.AmbiguousCount > 0 {
		out["ambiguous_count"] = value.AmbiguousCount
	}
	if value.EvidenceClass != "" {
		out["evidence_class"] = value.EvidenceClass
	}
	if value.Reason != "" {
		out["reason"] = value.Reason
	}
	return out
}

func cicdLiveRunCorrelationEvidenceMap(value cicdLiveRunCorrelationEvidence) map[string]any {
	out := map[string]any{
		"state": value.State,
		"count": value.Count,
	}
	if value.Truncated {
		out["truncated"] = true
	}
	if value.Reason != "" {
		out["reason"] = value.Reason
	}
	return out
}

func cicdRunArtifactEvidenceMap(value cicdRunArtifactEvidence) map[string]any {
	out := map[string]any{
		"state":                 value.State,
		"count":                 value.Count,
		"artifact_digest_count": value.ArtifactDigestCount,
		"image_ref_count":       value.ImageRefCount,
		"ambiguous_count":       value.AmbiguousCount,
	}
	if value.Reason != "" {
		out["reason"] = value.Reason
	}
	return out
}

func cicdEvidenceStorySummary(summary map[string]any) string {
	static := mapValue(summary, "static_workflow_artifacts")
	live := mapValue(summary, "live_run_correlations")
	bridge := mapValue(summary, "run_artifact_evidence")
	return fmt.Sprintf(
		"CI/CD evidence has static_workflow=%s, provider_runs=%s, run_artifact=%s (%s).",
		firstNonEmptyString(StringVal(static, "state"), "unknown"),
		firstNonEmptyString(StringVal(live, "state"), "unknown"),
		firstNonEmptyString(StringVal(bridge, "state"), "unknown"),
		firstNonEmptyString(StringVal(bridge, "reason"), StringVal(summary, "reason"), "no_reason"),
	)
}
