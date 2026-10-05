// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"go.opentelemetry.io/otel/trace"
)

type securityAlertProviderRepositoryScopeStore interface {
	SecurityAlertProviderRepositoryScopes(context.Context, string) ([]string, error)
}

func (h *Handler) resolveSupplyChainRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	rawSelector string,
	capability string,
) (string, bool) {
	rawSelector = strings.TrimSpace(rawSelector)
	if rawSelector == "" {
		return "", true
	}
	repoID, err := selector.ResolveExact(r.Context(), h.Neo4j, h.Content, rawSelector)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, capability) {
			return "", false
		}
		status := http.StatusBadRequest
		if selector.IsNotFound(err) {
			status = http.StatusNotFound
		}
		querycontract.WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}

// securityAlertSelectorRoute names the route that owns a security-alert
// selector read: the handler span and stage-log operation failStage reports
// against. The capability stays a plain string parameter so the root
// WriteGraphReadError capability sweep can resolve it through the callers.
type securityAlertSelectorRoute struct {
	span      trace.Span
	operation string
}

func (h *Handler) resolveSupplyChainSecurityAlertRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	rawSelector string,
	capability string,
	route securityAlertSelectorRoute,
) (string, []string, bool) {
	rawSelector = strings.TrimSpace(rawSelector)
	if rawSelector == "" {
		return "", nil, true
	}

	if h.Content != nil {
		// repo_id stays empty: the selector is unbounded caller input and
		// nothing is resolved yet.
		matchTimer := startSupplyChainQueryStage(r.Context(), h.Logger, route.operation, "", "repository_catalog_match")
		entries, err := h.Content.MatchRepositories(r.Context(), rawSelector)
		matchTimer.Done(r.Context(), slog.Bool("error", err != nil))
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, capability) {
				return "", nil, false
			}
			failStage(r.Context(), route.span, matchTimer, err)
			querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
			return "", nil, false
		}
		matches := selector.CatalogMatches(entries, rawSelector)
		switch len(matches) {
		case 0:
		case 1:
			scopes, ok := h.securityAlertRepositoryScopeIDsForCatalog(w, r, rawSelector, matches[0], entries, capability, route)
			if !ok {
				return "", nil, false
			}
			return matches[0], scopes, true
		default:
			querycontract.WriteError(w, http.StatusBadRequest, selector.AmbiguousError{Selector: rawSelector, Matches: matches}.Error())
			return "", nil, false
		}
	}

	if selector.LooksCanonicalRepositoryID(rawSelector) {
		return rawSelector, SecurityAlertRepositoryScopeIDs(rawSelector, nil), true
	}
	repoID, ok := h.resolveSupplyChainRepositorySelector(w, r, rawSelector, capability)
	if !ok {
		return "", nil, false
	}
	return repoID, SecurityAlertRepositoryScopeIDs(repoID, nil), true
}

func (h *Handler) securityAlertRepositoryScopeIDsForCatalog(
	w http.ResponseWriter,
	r *http.Request,
	selector string,
	repositoryID string,
	entries []querycontract.RepositoryCatalogEntry,
	capability string,
	route securityAlertSelectorRoute,
) ([]string, bool) {
	scopes := securityAlertRepositoryScopesForCatalog(repositoryID, entries)
	if store := h.securityAlertProviderScopeStore(); len(scopes) == 0 && store != nil {
		lookupTimer := startSupplyChainQueryStage(r.Context(), h.Logger, route.operation, repositoryID, "provider_repository_scope_lookup")
		var err error
		scopes, err = securityAlertRepositoryScopesForNames(r.Context(), store, repositoryID, entries)
		lookupTimer.Done(r.Context(), slog.Bool("error", err != nil))
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, capability) {
				return nil, false
			}
			failStage(r.Context(), route.span, lookupTimer, err)
			querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
			return nil, false
		}
	}
	scopes = UniqueSortedNonEmpty(scopes)
	if len(scopes) > 1 {
		querycontract.WriteError(w, http.StatusBadRequest, securityAlertProviderScopeAmbiguousError{
			Selector: selector,
			Scopes:   scopes,
		}.Error())
		return nil, false
	}
	return SecurityAlertRepositoryScopeIDs(repositoryID, scopes), true
}

func securityAlertRepositoryScopesForNames(
	ctx context.Context,
	store securityAlertProviderRepositoryScopeStore,
	repositoryID string,
	entries []querycontract.RepositoryCatalogEntry,
) ([]string, error) {
	names := securityAlertRepositoryNamesForCatalog(repositoryID, entries)
	scopes := make([]string, 0, len(names))
	for _, name := range names {
		found, err := store.SecurityAlertProviderRepositoryScopes(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("lookup provider security alert repository scopes: %w", err)
		}
		scopes = append(scopes, found...)
	}
	return scopes, nil
}

func (h *Handler) securityAlertProviderScopeStore() securityAlertProviderRepositoryScopeStore {
	if h == nil {
		return nil
	}
	if store, ok := h.SecurityAlerts.(securityAlertProviderRepositoryScopeStore); ok {
		return store
	}
	if store, ok := h.SecurityAlertAggregates.(securityAlertProviderRepositoryScopeStore); ok {
		return store
	}
	return nil
}

func securityAlertRepositoryScopesForCatalog(
	repositoryID string,
	entries []querycontract.RepositoryCatalogEntry,
) []string {
	out := make([]string, 0, 2)
	for _, entry := range entries {
		if entry.ID != repositoryID {
			continue
		}
		for _, raw := range []string{entry.RepoSlug, entry.RemoteURL} {
			if slug := cleanSecurityAlertGitHubSlug(raw); slug != "" {
				out = append(out, "security-alert:github:"+slug)
			}
		}
	}
	return out
}

func securityAlertRepositoryNamesForCatalog(
	repositoryID string,
	entries []querycontract.RepositoryCatalogEntry,
) []string {
	out := make([]string, 0, 1)
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.ID != repositoryID {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// SecurityAlertRepositoryScopeIDs merges repositoryID and scopeIDs into one
// deduplicated, sorted, trimmed slice for the security-alert route's
// repository scope filter.
func SecurityAlertRepositoryScopeIDs(repositoryID string, scopeIDs []string) []string {
	out := make([]string, 0, len(scopeIDs)+1)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(repositoryID)
	for _, scopeID := range scopeIDs {
		add(scopeID)
	}
	slices.Sort(out)
	return out
}

// UniqueSortedNonEmpty trims every entry in values, drops empty and duplicate
// results, and returns the remainder sorted.
func UniqueSortedNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

type securityAlertProviderScopeAmbiguousError struct {
	Selector string
	Scopes   []string
}

func (e securityAlertProviderScopeAmbiguousError) Error() string {
	return fmt.Sprintf(
		"repository selector %q matched multiple provider security alert repository scopes: %s; add repo_slug or remote_url evidence to disambiguate",
		e.Selector,
		strings.Join(e.Scopes, ", "),
	)
}

func cleanSecurityAlertGitHubSlug(value string) string {
	value = strings.Trim(strings.ToLower(strings.TrimSpace(value)), "/")
	value = strings.TrimPrefix(value, "git@github.com:")
	value = strings.TrimPrefix(value, "ssh://git@github.com/")
	value = strings.TrimPrefix(value, "https://github.com/")
	value = strings.TrimPrefix(value, "http://github.com/")
	value = strings.TrimSuffix(value, ".git")
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return ""
	}
	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(strings.TrimSuffix(parts[1], ".git"))
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}
