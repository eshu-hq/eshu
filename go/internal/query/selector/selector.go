// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// LookupFailureMessage is the fixed text a lookup failure answers with, as
// the LookupError prefix, the 500 body, and the span status description. It
// never carries the selector or the backend's error text, so a caller that
// answers a lookup failure itself writes this, never err.Error(), as the body.
const LookupFailureMessage = "repository selector lookup failed"

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

// LookupError is the errors.As target for a selector resolution that failed
// because a backing read failed (the catalog read or either graph read), not
// because of the selector itself. Err is the backend failure; Unwrap exposes
// it, so errors.Is still reaches the db and graph sentinels and
// querycontract.WriteGraphReadError still maps a fence or graph-availability
// verdict first. Anything left is a server fault, never a client error. The
// selector is deliberately not a field, and Error never includes it. Match it
// with IsLookupFailure.
type LookupError struct {
	Err error
}

func (e LookupError) Error() string {
	if e.Err == nil {
		return LookupFailureMessage
	}
	return LookupFailureMessage + ": " + e.Err.Error()
}

// Unwrap returns the backend failure so errors.Is and errors.As see through
// LookupError.
func (e LookupError) Unwrap() error {
	return e.Err
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
			return "", LookupError{Err: fmt.Errorf("match repositories: %w", err)}
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
			return "", LookupError{Err: fmt.Errorf("query graph repository selector: %w", err)}
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
				return "", LookupError{Err: fmt.Errorf("query graph repository selector: %w", err)}
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
//
// A LookupError that is not one of those verdicts (a bare reader failure, a
// SQL error, a driver error) answers 500 with a fixed body and records the
// error on the request span, the only operator signal this helper can reach:
// it has no logger and every caller owns its own span (#7626). An unmatched
// selector answers 404 and anything else, such as an ambiguous match, 400.
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
		if IsLookupFailure(err) {
			span := trace.SpanFromContext(r.Context())
			span.RecordError(err)
			span.SetStatus(codes.Error, LookupFailureMessage)
			querycontract.WriteError(w, http.StatusInternalServerError, LookupFailureMessage)
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

// IsLookupFailure reports whether err is (or wraps) a LookupError: a backing
// read failed during resolution. Check it after
// querycontract.WriteGraphReadError, which owns the fence and
// graph-availability verdicts that also wrap a LookupError; what remains is a
// server fault to answer with 500, never 400.
func IsLookupFailure(err error) bool {
	var target LookupError
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
