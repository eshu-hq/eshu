// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/workflowimage"
)

const cicdStaticWorkflowEvidencePathLimit = 20

type cicdRunCorrelationEvidenceSummary struct {
	StaticWorkflowArtifacts cicdStaticWorkflowArtifactEvidence `json:"static_workflow_artifacts"`
	LiveRunCorrelations     cicdLiveRunCorrelationEvidence     `json:"live_run_correlations"`
	RunArtifactEvidence     cicdRunArtifactEvidence            `json:"run_artifact_evidence"`
	MissingEvidence         []string                           `json:"missing_evidence,omitempty"`
	Reason                  string                             `json:"reason,omitempty"`
}

type cicdStaticWorkflowArtifactEvidence struct {
	State           string   `json:"state"`
	Count           int      `json:"count"`
	Paths           []string `json:"paths,omitempty"`
	Truncated       bool     `json:"truncated,omitempty"`
	ImageRefCount   int      `json:"image_ref_count,omitempty"`
	UnresolvedCount int      `json:"unresolved_count,omitempty"`
	AmbiguousCount  int      `json:"ambiguous_count,omitempty"`
	EvidenceClass   string   `json:"evidence_class,omitempty"`
	Reason          string   `json:"reason,omitempty"`
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
		artifact := missingCICDRunArtifactEvidence(live.Reason)
		return cicdRunCorrelationEvidenceSummary{
			StaticWorkflowArtifacts: static,
			LiveRunCorrelations:     live,
			RunArtifactEvidence:     artifact,
			MissingEvidence:         cicdSummaryMissingEvidence(static, live, artifact),
			Reason:                  live.Reason,
		}
	}
	if liveCount > 0 {
		artifact := cicdRunArtifactEvidenceFromRows(rows)
		live.State = "present"
		return cicdRunCorrelationEvidenceSummary{
			StaticWorkflowArtifacts: static,
			LiveRunCorrelations:     live,
			RunArtifactEvidence:     artifact,
			MissingEvidence:         cicdSummaryMissingEvidence(static, live, artifact),
		}
	}

	summaryReason := "live_run_correlation_missing"
	switch static.State {
	case "present":
		if static.ImageRefCount > 0 {
			summaryReason = "workflow_image_ref_static_only"
		} else {
			summaryReason = "static_workflow_only_live_run_correlation_missing"
		}
	case "absent":
		summaryReason = "no_ci_cd_evidence_found"
	}
	live.Reason = summaryReason
	artifact := missingCICDRunArtifactEvidence(summaryReason)

	return cicdRunCorrelationEvidenceSummary{
		StaticWorkflowArtifacts: static,
		LiveRunCorrelations:     live,
		RunArtifactEvidence:     artifact,
		MissingEvidence:         cicdSummaryMissingEvidence(static, live, artifact),
		Reason:                  summaryReason,
	}
}

func cicdSummaryMissingEvidence(
	static cicdStaticWorkflowArtifactEvidence,
	live cicdLiveRunCorrelationEvidence,
	artifact cicdRunArtifactEvidence,
) []string {
	var missing []string
	switch live.State {
	case "unavailable":
		missing = append(missing, "live_ci_provider_evidence_unavailable")
	case "missing":
		switch static.State {
		case "present":
			missing = append(missing, "source_to_ci_run_evidence_missing")
		case "absent":
			missing = append(missing, "ci_cd_evidence_missing")
		case "unavailable":
			missing = append(missing, "static_workflow_evidence_unavailable", "source_to_ci_run_evidence_missing")
		case "not_checked":
			missing = append(missing, "ci_cd_run_correlation_missing")
		}
	}
	if artifact.State == "missing" {
		missing = append(missing, "ci_run_to_image_artifact_evidence_missing")
	}
	if static.UnresolvedCount > 0 {
		missing = append(missing, "workflow_image_ref_unresolved")
	}
	if static.AmbiguousCount > 0 {
		missing = append(missing, "workflow_image_ref_ambiguous")
	}
	return uniqueSortedNonEmpty(missing)
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
	imageRefCount, unresolvedCount, ambiguousCount, imageEvidenceErr := h.staticWorkflowImageEvidenceCounts(ctx, repositoryID, files)

	slices.Sort(paths)
	truncated := len(paths) > cicdStaticWorkflowEvidencePathLimit
	if truncated {
		paths = paths[:cicdStaticWorkflowEvidencePathLimit]
	}

	out := cicdStaticWorkflowArtifactEvidence{
		State:     "present",
		Count:     count,
		Paths:     paths,
		Truncated: truncated,
	}
	out.ImageRefCount = imageRefCount
	out.UnresolvedCount = unresolvedCount
	out.AmbiguousCount = ambiguousCount
	if imageEvidenceErr != nil {
		out.Reason = "workflow_image_evidence_read_failed"
	}
	switch {
	case imageRefCount > 0 && ambiguousCount == 0 && unresolvedCount == 0:
		out.EvidenceClass = workflowimage.EvidenceClassImageRef
	case ambiguousCount > 0:
		out.EvidenceClass = workflowimage.EvidenceClassAmbiguous
	case unresolvedCount > 0:
		out.EvidenceClass = workflowimage.EvidenceClassUnresolved
	}
	return out
}

func (h *CICDHandler) staticWorkflowImageEvidenceCounts(
	ctx context.Context,
	repositoryID string,
	files []FileContent,
) (int, int, int, error) {
	if h == nil || h.Content == nil {
		return 0, 0, 0, nil
	}
	hydrated, err := hydrateRepositoryCandidateFiles(ctx, h.Content, repositoryID, files, isGitHubActionsWorkflowFile)
	if err != nil {
		return 0, 0, 0, err
	}
	exactRefs := map[string]struct{}{}
	unresolvedCount := 0
	ambiguousCount := 0
	for _, file := range hydrated {
		if !isGitHubActionsWorkflowFile(file) {
			continue
		}
		for _, evidence := range workflowimage.ExtractGitHubActions(file.RelativePath, file.Content) {
			switch evidence.EvidenceClass {
			case workflowimage.EvidenceClassImageRef:
				exactRefs[evidence.ImageRef] = struct{}{}
			case workflowimage.EvidenceClassUnresolved:
				unresolvedCount++
			case workflowimage.EvidenceClassAmbiguous:
				ambiguousCount++
			}
		}
	}
	return len(exactRefs), unresolvedCount, ambiguousCount, nil
}
