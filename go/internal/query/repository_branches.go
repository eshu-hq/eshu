// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
)

// getRepositoryBranches returns the source-backed refs the console branch
// selector needs. Older indexed repositories may only have the derived indexed
// commit SHA; those continue to report a single truth-labeled fallback row.
//
// GET /api/v0/repositories/{repo_id}/branches
func (h *RepositoryHandler) getRepositoryBranches(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	repoRef, _, err := h.repositoryStatsRepositoryRef(ctx, repoID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository failed: %v", err))
		return
	}
	if repoRef == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	refs, err := repositoryRefs(ctx, h.Content, repoID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository refs failed: %v", err))
		return
	}
	if len(refs) > 0 {
		WriteSuccess(
			w,
			r,
			http.StatusOK,
			map[string]any{
				"default_branch": repositoryRefsDefaultBranch(refs),
				"branches":       repositoryRefBranchEntries(refs),
				"tags":           repositoryRefTagEntries(refs),
			},
			BuildTruthEnvelope(
				h.profile(),
				"platform_impact.context_overview",
				TruthBasisContentIndex,
				"reports source-backed git refs captured during repository ingestion",
			),
		)
		return
	}

	branches := make([]map[string]any, 0, 1)
	if h.Content != nil {
		commitSHA := h.indexedCommitSHA(ctx, repoID)
		if commitSHA != "" {
			entry := map[string]any{
				"name":     "",
				"head_sha": commitSHA,
			}
			if coverage, err := h.Content.RepositoryCoverage(ctx, repoID); err == nil {
				if latest := latestCoverageTimestamp(coverage.FileIndexedAt, coverage.EntityIndexedAt); !latest.IsZero() {
					entry["last_indexed_at"] = formatCoverageTimestamp(latest)
				}
			}
			branches = append(branches, entry)
		}
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"default_branch": "", // not captured by ingestion yet (#1433)
			"branches":       branches,
		},
		BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			TruthBasisContentIndex,
			"reports the single indexed commit ref because source-backed git refs are unavailable; no branch names are invented",
		),
	)
}

// indexedCommitSHA returns the commit SHA recorded for the repository's indexed
// files, or "" when none is available.
func (h *RepositoryHandler) indexedCommitSHA(ctx context.Context, repoID string) string {
	files, err := h.Content.ListRepoFiles(ctx, repoID, 1)
	if err != nil {
		return ""
	}
	for _, file := range files {
		if file.CommitSHA != "" {
			return file.CommitSHA
		}
	}
	return ""
}
