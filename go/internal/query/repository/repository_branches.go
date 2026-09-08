// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	readmodel "github.com/eshu-hq/eshu/go/internal/query/repositoryreadmodel"
)

// getRepositoryBranches returns the source-backed refs the console branch
// selector needs, bounded by a single limit+cursor over the combined
// branches+tags stream (#5503). Older indexed repositories may only have the
// derived indexed commit SHA; those continue to report a single
// truth-labeled fallback row and never paginate.
//
// GET /api/v0/repositories/{repo_id}/branches?limit=&cursor=
// GetRepositoryBranches serves repository branches. It forwards to getRepositoryBranches; exported for
// #6060 so the cross-family graph-read sweep tests in package query can name
// it from outside this package.
func (h *RepositoryHandler) GetRepositoryBranches(w http.ResponseWriter, r *http.Request) {
	h.getRepositoryBranches(w, r)
}

func (h *RepositoryHandler) getRepositoryBranches(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r, "platform_impact.context_overview")
	if !ok {
		return
	}

	limit, ok := querycontract.ParseBoundedLimit(w, r, readmodel.RepositoryRefPageDefaultLimit, readmodel.RepositoryRefPageMaxLimit)
	if !ok {
		return
	}
	var cursor *readmodel.RepositoryRefPageCursor
	if raw := querycontract.QueryParam(r, "cursor"); raw != "" {
		decoded, err := readmodel.DecodeRepositoryRefPageCursor(raw, repoID)
		if err != nil {
			querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("invalid cursor: %v", err))
			return
		}
		cursor = &decoded
	}

	ctx := r.Context()
	repoRef, _, err := h.repositoryStatsRepositoryRef(ctx, repoID)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository failed: %v", err))
		return
	}
	if repoRef == nil {
		querycontract.WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	// ListRepositoryRefs intentionally stays unpaged: git-scale ref counts are
	// cheap in memory, readmodel.ValidateSelectedRepositoryRef needs the full list, and
	// default_branch must reflect the true default even on pages that no
	// longer include it.
	refs, err := readmodel.RepositoryRefs(ctx, h.Content, repoID)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query repository refs failed: %v", err))
		return
	}
	if len(refs) > 0 {
		// Re-sort by (kind, name) before paging: this is the exact comparator
		// the cursor's keyset math uses (readmodel.RepositoryRefKeyLess), independent of
		// is_default and independent of the store's collation-dependent name
		// order (T3). readmodel.RepositoryRefsDefaultBranch and
		// readmodel.ValidateSelectedRepositoryRef scan by flag/name, not position, so
		// re-sorting refs here does not affect them.
		readmodel.SortRepositoryRefsForPaging(refs)
		window, remainder, truncated, nextCursor := readmodel.RepositoryRefPageWindow(repoID, refs, cursor, limit)
		branches, tags := readmodel.RepositoryRefWindowEntries(window)
		response := map[string]any{
			"default_branch": readmodel.RepositoryRefsDefaultBranch(refs),
			"branches":       branches,
			"tags":           tags,
			"truncated":      truncated,
		}
		if truncated {
			response["next_cursor"] = nextCursor
		}
		// tags_truncated is deprecated in favor of truncated/next_cursor but
		// keeps its original meaning: more tags exist beyond what tags[]
		// carries in this page, derived from the exact in-memory remainder.
		if readmodel.RepositoryRefsContainTag(remainder) {
			response["tags_truncated"] = true
		}
		querycontract.WriteSuccess(
			w,
			r,
			http.StatusOK,
			response,
			querycontract.BuildTruthEnvelope(
				h.profile(),
				"platform_impact.context_overview",
				querycontract.TruthBasisContentIndex,
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

	querycontract.WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"default_branch": "", // not captured by ingestion yet (#1433)
			"branches":       branches,
			"tags":           make([]map[string]any, 0),
			"truncated":      false, // the single-entry fallback never paginates
		},
		querycontract.BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			querycontract.TruthBasisContentIndex,
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
