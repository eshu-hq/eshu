// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
)

// getRepositoryCoverage returns content store coverage for the repository.
func (h *RepositoryHandler) getRepositoryCoverage(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r, "platform_impact.context_overview")
	if !ok {
		return
	}

	resolvedRepoID, err := h.resolveCoverageRepositoryID(r.Context(), repoID)
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if resolvedRepoID == "" {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}
	repoID = resolvedRepoID

	// Get content store coverage
	coverage, err := h.queryContentStoreCoverage(r.Context(), repoID)
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("coverage query failed: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, coverage)
}

// resolveRepositorySelector resolves a repository selector (canonical id, name,
// or slug) to its canonical repository id using the graph and content backends.
func (h *RepositoryHandler) resolveRepositorySelector(ctx context.Context, selector string) (string, error) {
	return resolveRepositorySelectorExactForAccess(ctx, h.Neo4j, h.Content, selector, repositoryAccessFilterFromContext(ctx))
}

// resolveRepositoryPathSelector reads the {repo_id} path parameter, resolves it
// to a canonical repository id, and writes the appropriate HTTP error when the
// selector is missing, ambiguous, or not found. It returns false when the caller
// must stop after an error response has already been written.
//
// capability names the caller's capability for the bounded graph-read envelope
// so a backend timeout or outage during selector resolution surfaces as the
// same 503/504 contract every other graph-backed read uses, rather than
// falling through to the generic 400/404 branch below.
func (h *RepositoryHandler) resolveRepositoryPathSelector(w http.ResponseWriter, r *http.Request, capability string) (string, bool) {
	repoSelector := PathParam(r, "repo_id")
	if repoSelector == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required")
		return "", false
	}
	repoID, err := h.resolveRepositorySelector(r.Context(), repoSelector)
	if err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return "", false
		}
		status := http.StatusBadRequest
		if isRepositorySelectorNotFound(err) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}

// repositoryCatalogMap projects a content-store catalog entry into the wire map
// shape used by repository list and stats responses.
func repositoryCatalogMap(entry RepositoryCatalogEntry) map[string]any {
	return decorateRepositoryGroupEvidence(map[string]any{
		"id":            entry.ID,
		"name":          entry.Name,
		"path":          entry.Path,
		"local_path":    entry.LocalPath,
		"remote_url":    entry.RemoteURL,
		"repo_slug":     entry.RepoSlug,
		"has_remote":    entry.HasRemote,
		"is_dependency": false,
	})
}
