// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryselector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// NotFoundError is the errors.As target for a repository selector that
// matched nothing: no catalog entry, no graph row, and (for a scoped caller)
// an access filter with no grants at all. Selector carries the raw input
// string that failed to resolve, for the caller's error message. Match it
// with IsNotFound rather than a type assertion, since it can arrive wrapped.
type NotFoundError struct {
	Selector string
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("repository selector %q did not match any indexed repository", e.Selector)
}

// AmbiguousError is the errors.As target for a repository selector that
// matched more than one repository. Selector carries the raw input string;
// Matches carries the matched repository ids, so the caller can report or log
// which repositories collided.
type AmbiguousError struct {
	Selector string
	Matches  []string
}

func (e AmbiguousError) Error() string {
	return fmt.Sprintf("repository selector %q matched multiple repositories: %s", e.Selector, strings.Join(e.Matches, ", "))
}

// ResolveExact resolves selector against every indexed repository, ignoring
// caller scope. Use it only where the caller genuinely has no per-request
// access bounds to enforce (local tooling, admin paths); a request-scoped
// caller should call ResolveExactForAccess instead so the resolution stays
// inside its granted repositories.
func ResolveExact(ctx context.Context, graph querycontract.GraphQuery, content querycontract.ContentStore, selector string) (string, error) {
	return ResolveExactForAccess(ctx, graph, content, selector, querycontract.RepositoryAccessFilter{AllScopes: true})
}

// ResolveExactForAccess resolves selector to a canonical repository id,
// trying the catalog match first and falling back to a graph lookup, both
// bound by access.
//
// A scoped caller with no grants resolves nothing, but the two lookups reach
// that answer differently and the distinction matters when editing either. The
// catalog lookup still runs; its results pass through FilterCatalogEntries,
// which allows nothing for an empty filter, so it yields no match and falls
// through. The graph lookup is refused outright by an explicit Empty check
// before the query is built. Remove either guard and a caller with no
// repository access can resolve any repository in the index. See this
// package's AGENTS.md before changing either one, or either query's access
// predicate.
func ResolveExactForAccess(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	selector string,
	access querycontract.RepositoryAccessFilter,
) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", nil
	}
	if LooksCanonicalRepositoryID(selector) {
		if !access.AllowsRepositoryID(selector) {
			return "", NotFoundError{Selector: selector}
		}
		return selector, nil
	}

	if content != nil {
		entries, err := content.MatchRepositories(ctx, selector)
		if err != nil {
			return "", fmt.Errorf("match repositories: %w", err)
		}
		entries = access.FilterCatalogEntries(entries)
		matches := CatalogMatches(entries, selector)
		switch len(matches) {
		case 0:
		case 1:
			return matches[0], nil
		default:
			return "", AmbiguousError{Selector: selector, Matches: matches}
		}
	}

	if graph != nil {
		if access.Empty() {
			return "", NotFoundError{Selector: selector}
		}
		rows, err := graph.Run(ctx, `
			MATCH (r:Repository)
			WHERE (
			   r.id = $repo_selector
			   OR r.name = $repo_selector
			   OR r.path = $repo_selector
			   OR r.local_path = $repo_selector
			   OR r.remote_url = $repo_selector
			   OR r.repo_slug = $repo_selector
			)
			`+access.GraphPredicate("r")+`
			RETURN r.id as id
			ORDER BY r.id
		`, access.GraphParams(map[string]any{"repo_selector": selector}))
		if err != nil {
			return "", fmt.Errorf("query graph repository selector: %w", err)
		}
		switch len(rows) {
		case 0:
			row, err := graph.RunSingle(ctx, `
				MATCH (r:Repository)
				WHERE (
				   r.id = $repo_selector
				   OR r.name = $repo_selector
				   OR r.path = $repo_selector
				   OR r.local_path = $repo_selector
				   OR r.remote_url = $repo_selector
				   OR r.repo_slug = $repo_selector
				)
				`+access.GraphPredicate("r")+`
				RETURN r.id as id
			`, access.GraphParams(map[string]any{"repo_selector": selector}))
			if err != nil {
				return "", fmt.Errorf("query graph repository selector: %w", err)
			}
			if row != nil {
				return querycontract.StringVal(row, "id"), nil
			}
		case 1:
			return querycontract.StringVal(rows[0], "id"), nil
		default:
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				id := querycontract.StringVal(row, "id")
				if id == "" {
					continue
				}
				ids = append(ids, id)
			}
			slices.Sort(ids)
			return "", AmbiguousError{Selector: selector, Matches: ids}
		}
	}

	return "", NotFoundError{Selector: selector}
}

// ResolveForRequestWithAccess resolves a repository selector
// and writes the failure response itself, reporting false when it did.
//
// capability names the caller's capability for the bounded graph-read envelope.
// Selector resolution issues its own graph reads, so a backend timeout or
// outage here must surface as the same 503/504 contract every other
// graph-backed read uses. Without that mapping it fell through to the generic
// branch below and reported HTTP 400, telling the client its request was
// malformed when nothing was wrong with the request at all.
func ResolveForRequestWithAccess(
	w http.ResponseWriter,
	r *http.Request,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	selector string,
	access querycontract.RepositoryAccessFilter,
	capability string,
) (string, bool) {
	repoID, err := ResolveExactForAccess(r.Context(), graph, content, selector, access)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, capability) {
			return "", false
		}
		status := http.StatusBadRequest
		if IsNotFound(err) {
			status = http.StatusNotFound
		}
		querycontract.WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}

// IsNotFound reports whether err is (or wraps) a NotFoundError, so callers
// can map a selector-resolution failure to a 404 without depending on the
// error's concrete type.
func IsNotFound(err error) bool {
	var target NotFoundError
	return errors.As(err, &target)
}

// LooksCanonicalRepositoryID reports whether a selector already has the shape
// of a canonical repository id, so a caller can skip catalog matching.
func LooksCanonicalRepositoryID(selector string) bool {
	return strings.HasPrefix(selector, "repo://") ||
		strings.HasPrefix(selector, "repo-") ||
		strings.HasPrefix(selector, "repository:")
}

// CatalogMatches returns the repository ids whose catalog entry matches the
// selector. The caller decides what a zero, one, or many result means.
func CatalogMatches(entries []querycontract.RepositoryCatalogEntry, selector string) []string {
	if strings.TrimSpace(selector) == "" {
		return nil
	}
	matches := make([]string, 0, 1)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		switch selector {
		case entry.ID, entry.Name, entry.Path, entry.LocalPath, entry.RemoteURL, entry.RepoSlug:
			if entry.ID == "" {
				continue
			}
			if _, ok := seen[entry.ID]; ok {
				continue
			}
			seen[entry.ID] = struct{}{}
			matches = append(matches, entry.ID)
		}
	}
	slices.Sort(matches)
	return matches
}
