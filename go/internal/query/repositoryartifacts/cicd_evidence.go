// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/workflowimage"
)

type CicdRunCorrelationEvidenceSummary struct {
	StaticWorkflowArtifacts CicdStaticWorkflowArtifactEvidence `json:"static_workflow_artifacts"`
	LiveRunCorrelations     CicdLiveRunCorrelationEvidence     `json:"live_run_correlations"`
	RunArtifactEvidence     CicdRunArtifactEvidence            `json:"run_artifact_evidence"`
	MissingEvidence         []string                           `json:"missing_evidence,omitempty"`
	Reason                  string                             `json:"reason,omitempty"`
}

type CicdStaticWorkflowArtifactEvidence struct {
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

type CicdLiveRunCorrelationEvidence struct {
	State     string `json:"state"`
	Count     int    `json:"count"`
	Truncated bool   `json:"truncated,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type CicdRunArtifactEvidence struct {
	State               string `json:"state"`
	Count               int    `json:"count"`
	ArtifactDigestCount int    `json:"artifact_digest_count"`
	ImageRefCount       int    `json:"image_ref_count"`
	AmbiguousCount      int    `json:"ambiguous_count"`
	Reason              string `json:"reason,omitempty"`
}

func BuildCICDRunCorrelationEvidenceSummary(
	static CicdStaticWorkflowArtifactEvidence,
	rows []querycontract.CICDRunCorrelationResult,
	liveTruncated bool,
	liveUnavailable bool,
) CicdRunCorrelationEvidenceSummary {
	liveCount := len(rows)
	live := CicdLiveRunCorrelationEvidence{
		State:     "missing",
		Count:     liveCount,
		Truncated: liveTruncated,
	}
	if liveUnavailable {
		live.State = "unavailable"
		live.Reason = "run_correlation_read_model_unavailable"
		artifact := missingCICDRunArtifactEvidence(live.Reason)
		return CicdRunCorrelationEvidenceSummary{
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
		return CicdRunCorrelationEvidenceSummary{
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

	return CicdRunCorrelationEvidenceSummary{
		StaticWorkflowArtifacts: static,
		LiveRunCorrelations:     live,
		RunArtifactEvidence:     artifact,
		MissingEvidence:         cicdSummaryMissingEvidence(static, live, artifact),
		Reason:                  summaryReason,
	}
}

func cicdSummaryMissingEvidence(
	static CicdStaticWorkflowArtifactEvidence,
	live CicdLiveRunCorrelationEvidence,
	artifact CicdRunArtifactEvidence,
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
	return querycontract.UniqueSortedStrings(missing)
}

func cicdRunArtifactEvidenceFromRows(rows []querycontract.CICDRunCorrelationResult) CicdRunArtifactEvidence {
	out := CicdRunArtifactEvidence{State: "missing", Reason: "artifact_or_image_evidence_missing"}
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

func missingCICDRunArtifactEvidence(reason string) CicdRunArtifactEvidence {
	return CicdRunArtifactEvidence{
		State:  "missing",
		Reason: reason,
	}
}

func StaticWorkflowArtifactEvidence(
	ctx context.Context,
	content querycontract.ContentStore,
	repositoryID string,
) CicdStaticWorkflowArtifactEvidence {
	if repositoryID == "" {
		return CicdStaticWorkflowArtifactEvidence{
			State:  "not_checked",
			Reason: "repository_scope_required",
		}
	}
	if content == nil {
		return CicdStaticWorkflowArtifactEvidence{
			State:  "unavailable",
			Reason: "content_store_unavailable",
		}
	}

	files, err := content.ListRepoFiles(ctx, repositoryID, querycontract.RepositorySemanticEntityLimit)
	if err != nil {
		return CicdStaticWorkflowArtifactEvidence{
			State:  "unavailable",
			Reason: "workflow_artifact_read_failed",
		}
	}

	count := 0
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if !IsGitHubActionsWorkflowFile(file) {
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
		return CicdStaticWorkflowArtifactEvidence{State: "absent"}
	}
	imageRefCount, unresolvedCount, ambiguousCount, imageEvidenceErr := staticWorkflowImageEvidenceCounts(ctx, content, repositoryID, files)

	slices.Sort(paths)
	truncated := len(paths) > cicdStaticWorkflowEvidencePathLimit
	if truncated {
		paths = paths[:cicdStaticWorkflowEvidencePathLimit]
	}

	out := CicdStaticWorkflowArtifactEvidence{
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

func staticWorkflowImageEvidenceCounts(
	ctx context.Context,
	content querycontract.ContentStore,
	repositoryID string,
	files []querycontract.FileContent,
) (int, int, int, error) {
	if content == nil {
		return 0, 0, 0, nil
	}
	hydrated, err := HydrateRepositoryCandidateFiles(ctx, content, repositoryID, files, IsGitHubActionsWorkflowFile)
	if err != nil {
		return 0, 0, 0, err
	}
	exactRefs := map[string]struct{}{}
	unresolvedCount := 0
	ambiguousCount := 0
	for _, file := range hydrated {
		if !IsGitHubActionsWorkflowFile(file) {
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

const cicdStaticWorkflowEvidencePathLimit = 20

func LoadRepositoryScopedCICDEvidence(
	ctx context.Context,
	content querycontract.ContentStore,
	correlations querycontract.CICDRunCorrelationStore,
	repositoryID string,
) (map[string]any, error) {
	if repositoryID == "" {
		return nil, nil
	}
	static := StaticWorkflowArtifactEvidence(ctx, content, repositoryID)
	if correlations == nil {
		summary := BuildCICDRunCorrelationEvidenceSummary(static, nil, false, true)
		return cicdRunCorrelationEvidenceSummaryMap(summary), nil
	}

	rows, err := correlations.ListCICDRunCorrelations(ctx, querycontract.CICDRunCorrelationFilter{
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
	results := make([]querycontract.CICDRunCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, querycontract.CICDRunCorrelationResult(row))
	}
	summary := BuildCICDRunCorrelationEvidenceSummary(static, results, truncated, false)
	return cicdRunCorrelationEvidenceSummaryMap(summary), nil
}

func cicdRunCorrelationEvidenceSummaryMap(summary CicdRunCorrelationEvidenceSummary) map[string]any {
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

func cicdStaticWorkflowArtifactEvidenceMap(value CicdStaticWorkflowArtifactEvidence) map[string]any {
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

func cicdLiveRunCorrelationEvidenceMap(value CicdLiveRunCorrelationEvidence) map[string]any {
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

func cicdRunArtifactEvidenceMap(value CicdRunArtifactEvidence) map[string]any {
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

// CICDStoryRunCorrelationLimit bounds the CI/CD run-correlation rows one
// read carries. Exported for #6060 so root CI/CD tests can name it from
// outside this package.
const CICDStoryRunCorrelationLimit = 20

const cicdStoryRunCorrelationLimit = CICDStoryRunCorrelationLimit
