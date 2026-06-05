package query

import (
	"context"
	"fmt"
	"net/http"
)

// getRepositoryBranches returns the repository refs the console branch selector
// needs. Git refs are not captured by ingestion yet (see #1433), so the only
// ref-like datum is the single commit SHA recorded per indexed file. This
// endpoint therefore reports that single indexed ref, truth-labeled as derived,
// rather than fabricating a multi-branch list. When ref ingestion lands, this
// handler can return the full default_branch + per-branch head list.
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

	branches := make([]map[string]any, 0, 1)
	if h.Content != nil {
		commitSHA := h.indexedCommitSHA(ctx, repoID)
		if commitSHA != "" {
			entry := map[string]any{
				"name":     "", // branch names are not captured by ingestion yet (#1433)
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
			"reports the single indexed commit ref per repository; git branch names are not captured by ingestion yet, so no multi-branch list is invented",
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
