package query

import (
	"context"
	"slices"
)

const cicdStaticWorkflowEvidencePathLimit = 20

type cicdRunCorrelationEvidenceSummary struct {
	StaticWorkflowArtifacts cicdStaticWorkflowArtifactEvidence `json:"static_workflow_artifacts"`
	LiveRunCorrelations     cicdLiveRunCorrelationEvidence     `json:"live_run_correlations"`
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

func (h *CICDHandler) runCorrelationEvidenceSummary(
	ctx context.Context,
	repositoryID string,
	liveCount int,
	liveTruncated bool,
) cicdRunCorrelationEvidenceSummary {
	static := h.staticWorkflowArtifactEvidence(ctx, repositoryID)
	live := cicdLiveRunCorrelationEvidence{
		State:     "missing",
		Count:     liveCount,
		Truncated: liveTruncated,
	}
	if liveCount > 0 {
		live.State = "present"
		return cicdRunCorrelationEvidenceSummary{
			StaticWorkflowArtifacts: static,
			LiveRunCorrelations:     live,
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
		Reason:                  summaryReason,
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
