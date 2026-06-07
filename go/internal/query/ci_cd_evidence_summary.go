package query

import (
	"context"
	"slices"
)

const cicdStaticWorkflowEvidencePathLimit = 20

type cicdRunCorrelationEvidenceSummary struct {
	StaticWorkflowArtifacts cicdStaticWorkflowArtifactEvidence `json:"static_workflow_artifacts"`
	LiveRunCorrelations     cicdLiveRunCorrelationEvidence     `json:"live_run_correlations"`
	RunArtifactEvidence     cicdRunArtifactEvidence            `json:"run_artifact_evidence"`
	Reason                  string                             `json:"reason,omitempty"`
}

type cicdStaticWorkflowArtifactEvidence struct {
	State     string   `json:"state"`
	Count     int      `json:"count"`
	Paths     []string `json:"paths,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

type cicdLiveRunCorrelationEvidence struct {
	State     string `json:"state"`
	Count     int    `json:"count"`
	Truncated bool   `json:"truncated,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type cicdRunArtifactEvidence struct {
	State               string `json:"state"`
	Count               int    `json:"count"`
	ArtifactDigestCount int    `json:"artifact_digest_count"`
	ImageRefCount       int    `json:"image_ref_count"`
	AmbiguousCount      int    `json:"ambiguous_count"`
	Reason              string `json:"reason,omitempty"`
}

func (h *CICDHandler) runCorrelationEvidenceSummary(
	ctx context.Context,
	repositoryID string,
	rows []CICDRunCorrelationResult,
	liveTruncated bool,
) cicdRunCorrelationEvidenceSummary {
	static := h.staticWorkflowArtifactEvidence(ctx, repositoryID)
	return buildCICDRunCorrelationEvidenceSummary(static, rows, liveTruncated, false)
}

func buildCICDRunCorrelationEvidenceSummary(
	static cicdStaticWorkflowArtifactEvidence,
	rows []CICDRunCorrelationResult,
	liveTruncated bool,
	liveUnavailable bool,
) cicdRunCorrelationEvidenceSummary {
	liveCount := len(rows)
	live := cicdLiveRunCorrelationEvidence{
		State:     "missing",
		Count:     liveCount,
		Truncated: liveTruncated,
	}
	if liveUnavailable {
		live.State = "unavailable"
		live.Reason = "run_correlation_read_model_unavailable"
		return cicdRunCorrelationEvidenceSummary{
			StaticWorkflowArtifacts: static,
			LiveRunCorrelations:     live,
			RunArtifactEvidence:     missingCICDRunArtifactEvidence(live.Reason),
			Reason:                  live.Reason,
		}
	}
	if liveCount > 0 {
		live.State = "present"
		return cicdRunCorrelationEvidenceSummary{
			StaticWorkflowArtifacts: static,
			LiveRunCorrelations:     live,
			RunArtifactEvidence:     cicdRunArtifactEvidenceFromRows(rows),
		}
	}

	summaryReason := "live_run_correlation_missing"
	switch static.State {
	case "present":
		summaryReason = "static_workflow_only_live_run_correlation_missing"
	case "absent":
		summaryReason = "no_ci_cd_evidence_found"
	}
	live.Reason = summaryReason

	return cicdRunCorrelationEvidenceSummary{
		StaticWorkflowArtifacts: static,
		LiveRunCorrelations:     live,
		RunArtifactEvidence:     missingCICDRunArtifactEvidence(summaryReason),
		Reason:                  summaryReason,
	}
}

func cicdRunArtifactEvidenceFromRows(rows []CICDRunCorrelationResult) cicdRunArtifactEvidence {
	out := cicdRunArtifactEvidence{State: "missing", Reason: "artifact_or_image_evidence_missing"}
	admittedCount := 0
	unresolvedCount := 0
	for _, row := range rows {
		hasDigest := row.ArtifactDigest != ""
		hasImageRef := row.ImageRef != ""
		if !hasDigest && !hasImageRef {
			continue
		}
		switch row.Outcome {
		case "exact", "derived":
			admittedCount++
			if hasDigest {
				out.ArtifactDigestCount++
			}
			if hasImageRef {
				out.ImageRefCount++
			}
		case "ambiguous":
			out.AmbiguousCount++
		default:
			unresolvedCount++
		}
	}
	out.Count = admittedCount + out.AmbiguousCount
	if out.ArtifactDigestCount > 0 {
		out.State = "present"
		out.Reason = "artifact_digest_present"
		return out
	}
	if out.ImageRefCount > 0 {
		out.State = "present"
		out.Reason = "image_ref_present"
		return out
	}
	if out.AmbiguousCount > 0 {
		out.State = "ambiguous"
		out.Reason = "ambiguous_artifact_evidence"
		return out
	}
	if unresolvedCount > 0 {
		out.Reason = "artifact_evidence_unresolved"
	}
	return out
}

func missingCICDRunArtifactEvidence(reason string) cicdRunArtifactEvidence {
	return cicdRunArtifactEvidence{
		State:  "missing",
		Reason: reason,
	}
}

func (h *CICDHandler) staticWorkflowArtifactEvidence(
	ctx context.Context,
	repositoryID string,
) cicdStaticWorkflowArtifactEvidence {
	if repositoryID == "" {
		return cicdStaticWorkflowArtifactEvidence{
			State:  "not_checked",
			Reason: "repository_scope_required",
		}
	}
	if h == nil || h.Content == nil {
		return cicdStaticWorkflowArtifactEvidence{
			State:  "unavailable",
			Reason: "content_store_unavailable",
		}
	}

	files, err := h.Content.ListRepoFiles(ctx, repositoryID, repositorySemanticEntityLimit)
	if err != nil {
		return cicdStaticWorkflowArtifactEvidence{
			State:  "unavailable",
			Reason: "workflow_artifact_read_failed",
		}
	}

	count := 0
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if !isGitHubActionsWorkflowFile(file) {
			continue
		}
		count++
		path := file.RelativePath
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	if count == 0 {
		return cicdStaticWorkflowArtifactEvidence{State: "absent"}
	}

	slices.Sort(paths)
	truncated := len(paths) > cicdStaticWorkflowEvidencePathLimit
	if truncated {
		paths = paths[:cicdStaticWorkflowEvidencePathLimit]
	}

	return cicdStaticWorkflowArtifactEvidence{
		State:     "present",
		Count:     count,
		Paths:     paths,
		Truncated: truncated,
	}
}
