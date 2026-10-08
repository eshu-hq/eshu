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
// workflow evidence defines: the path-ordered file read returned a sentinel row
// beyond its limit, so files past the clipped page were not scanned. It matches the OpenAPI enum.
const candidatePoolUnknownAtLimit = "unknown_at_limit"

// StaticWorkflowArtifactEvidence lists one bounded repository file page and
// reports observed workflow evidence with explicit candidate coverage. It reads
// querycontract.RepositorySemanticEntityLimit+1 rows: the extra row is a
// sentinel that proves files exist beyond the limit. A repository with exactly
// the limit's files therefore reads as a complete scan, and only a sentinel row
// marks the candidate pool unknown (#7619).
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

	const limit = querycontract.RepositorySemanticEntityLimit
	files, err := content.ListRepoFiles(ctx, repositoryID, limit+1)
	if err != nil {
		return CicdStaticWorkflowArtifactEvidence{
			State:  "unavailable",
			Reason: "workflow_artifact_read_failed",
		}
	}
	filesTruncated := len(files) > limit
	if filesTruncated {
		files = files[:limit]
	}
	return StaticWorkflowArtifactEvidenceFromFiles(ctx, content, repositoryID, files, filesTruncated)
}

// StaticWorkflowArtifactEvidenceFromFiles builds the same static workflow
// evidence as StaticWorkflowArtifactEvidence from a repository file list the
// caller already read with ListRepoFiles(repositoryID,
// querycontract.RepositorySemanticEntityLimit+1) and clipped to
// querycontract.RepositorySemanticEntityLimit. The repository story passes the
// list it listed for its semantic overview so the story pays for that read once
// instead of once per consumer (#7126). The list is only read, never modified.
//
// filesTruncated reports that the caller's read returned the sentinel row beyond
// the limit, so repository files exist past the clipped list. The candidate pool
// is unknown exactly when filesTruncated is true; a clipped list of exactly
// querycontract.RepositorySemanticEntityLimit files with filesTruncated false is
// a complete scan (#7619). Classification and image evidence read only the
// clipped list.
func StaticWorkflowArtifactEvidenceFromFiles(
	ctx context.Context,
	content querycontract.ContentStore,
	repositoryID string,
	files []querycontract.FileContent,
	filesTruncated bool,
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
	if filesTruncated {
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
