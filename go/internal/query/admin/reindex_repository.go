// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/runtime"
)

// reindexScopeRepository records a reindex for the named repositories only.
const reindexScopeRepository = "repository"

// maxReindexRepositories bounds one repository-scoped request; each selector
// costs one catalog lookup.
const maxReindexRepositories = 100

// gitDefaultScopePrefix is the scope ID prefix of a git repository's
// default-branch scope. A ref scope appends "@<ref>" to it.
const gitDefaultScopePrefix = "git-repository-scope:"

// repositoryReindexAcceptedDetail states the per-repository watermark
// contract the 202 response promises (#7620).
const repositoryReindexAcceptedDetail = "Repository reindex recorded. Each repository's requested_at is its " +
	"reindex watermark: on each sync cycle the git ingester shard that owns the repository forces a full " +
	"re-parse when its newest activated full generation was ingested before requested_at, syncing " +
	"requested repositories first within ESHU_REPO_RECONCILE_MAX_PER_CYCLE. The request is satisfied when " +
	"that full generation activates; there is no separate completion status. A webhook-only ingester " +
	"honors it only when a webhook triggers the repository, and filesystem mode does not honor it. " +
	"Ingesters older than this API ignore it, so send the request after the rollout completes."

// RepositoryCatalogMatcher returns the exact repository catalog matches for
// one selector. The query ContentReader implements it.
type RepositoryCatalogMatcher interface {
	MatchRepositories(ctx context.Context, selector string) ([]querycontract.RepositoryCatalogEntry, error)
}

// RepositoryReindexRequester records a per-repository reindex watermark for
// each git scope ID and returns the stored watermarks sorted by scope ID
// (#7620).
type RepositoryReindexRequester interface {
	RequestRepositoryReindex(ctx context.Context, scopeIDs []string) ([]runtime.RepositoryReindexRequest, error)
}

// reindexRepositories resolves every selector, then records one watermark
// per distinct scope. It records nothing unless every selector resolves to
// exactly one git default-branch scope.
func (h *Handler) reindexRepositories(w http.ResponseWriter, r *http.Request, req reindexRequest) {
	if h.Repositories == nil || h.RepositoryReindexer == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "repository reindex handler not configured")
		return
	}
	repositoryIDByScope := make(map[string]string, len(req.Repositories))
	var problems []string
	for _, selector := range req.Repositories {
		matches, err := h.Repositories.MatchRepositories(r.Context(), selector)
		if err != nil {
			querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve repository %q: %v", selector, err))
			return
		}
		entry, problem := gitDefaultScopeMatch(matches)
		if problem != "" {
			problems = append(problems, fmt.Sprintf("%q: %s", selector, problem))
			continue
		}
		if _, seen := repositoryIDByScope[entry.ScopeID]; !seen {
			repositoryIDByScope[entry.ScopeID] = entry.ID
		}
	}
	if len(problems) > 0 {
		querycontract.WriteError(w, http.StatusBadRequest,
			"no reindex recorded; every repository must resolve to one git repository: "+strings.Join(problems, "; "))
		return
	}

	scopeIDs := make([]string, 0, len(repositoryIDByScope))
	for scopeID := range repositoryIDByScope {
		scopeIDs = append(scopeIDs, scopeID)
	}
	slices.Sort(scopeIDs)
	stored, err := h.RepositoryReindexer.RequestRepositoryReindex(r.Context(), scopeIDs)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repositories := make([]map[string]any, 0, len(stored))
	for _, request := range stored {
		repositories = append(repositories, map[string]any{
			"repository_id": repositoryIDByScope[request.ScopeID],
			"scope_id":      request.ScopeID,
			"requested_at":  request.RequestedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	querycontract.WriteJSON(w, http.StatusAccepted, map[string]any{
		"status":       "accepted",
		"ingester":     req.Ingester,
		"scope":        req.Scope,
		"force":        true,
		"repositories": repositories,
		"detail":       repositoryReindexAcceptedDetail,
	})
}

// gitDefaultScopeMatch returns the single catalog match when it is a git
// default-branch scope, or a reason the selector cannot be reindexed.
func gitDefaultScopeMatch(matches []querycontract.RepositoryCatalogEntry) (querycontract.RepositoryCatalogEntry, string) {
	switch len(matches) {
	case 0:
		return querycontract.RepositoryCatalogEntry{}, "no repository matched"
	case 1:
	default:
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		slices.Sort(ids)
		return querycontract.RepositoryCatalogEntry{}, "matched multiple repositories: " + strings.Join(ids, ", ")
	}
	entry := matches[0]
	if !strings.HasPrefix(entry.ScopeID, gitDefaultScopePrefix) || strings.Contains(entry.ScopeID, "@") {
		return querycontract.RepositoryCatalogEntry{}, fmt.Sprintf("not a git default-branch repository (scope %q)", entry.ScopeID)
	}
	return entry, ""
}
