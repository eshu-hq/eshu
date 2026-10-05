// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/workflowimage"
)

// candidatePoolUnknownAtLimit is the only candidate_pool_status value the
// workflow evidence defines: the path-ordered file page reached its limit, so
// files beyond it may or may not exist. It matches the OpenAPI enum.
const candidatePoolUnknownAtLimit = "unknown_at_limit"

// StaticWorkflowArtifactEvidence lists one bounded repository file page and
// reports observed workflow evidence with explicit candidate coverage.
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
	return StaticWorkflowArtifactEvidenceFromFiles(ctx, content, repositoryID, files)
}

// StaticWorkflowArtifactEvidenceFromFiles builds the same static workflow
// evidence as StaticWorkflowArtifactEvidence from a repository file list the
// caller already read with ListRepoFiles(repositoryID,
// querycontract.RepositorySemanticEntityLimit). The repository story passes the
// list it listed for its semantic overview so the story pays for that read once
// instead of once per consumer (#7126). The list is only read, never modified.
// A full page reports uncertain coverage even when the caller knows there are
// exactly 5,000 files.
func StaticWorkflowArtifactEvidenceFromFiles(
	ctx context.Context,
	content querycontract.ContentStore,
	repositoryID string,
	files []querycontract.FileContent,
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

	candidatePoolStatus := ""
	if len(files) >= querycontract.RepositorySemanticEntityLimit {
		candidatePoolStatus = candidatePoolUnknownAtLimit
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
		if candidatePoolStatus != "" {
			return CicdStaticWorkflowArtifactEvidence{
				State:               "unknown",
				CandidatePoolStatus: candidatePoolStatus,
				Reason:              "repository_file_scan_limit_reached",
			}
		}
		return CicdStaticWorkflowArtifactEvidence{State: "absent"}
	}
	imageRefCount, unresolvedCount, ambiguousCount, imageEvidenceErr := staticWorkflowImageEvidenceCounts(ctx, content, repositoryID, files)

	slices.Sort(paths)
	truncated := len(paths) > cicdStaticWorkflowEvidencePathLimit
	if truncated {
		paths = paths[:cicdStaticWorkflowEvidencePathLimit]
	}

	out := CicdStaticWorkflowArtifactEvidence{
		State:               "present",
		CandidatePoolStatus: candidatePoolStatus,
		Count:               count,
		Paths:               paths,
		Truncated:           truncated,
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
