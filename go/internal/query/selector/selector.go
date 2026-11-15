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
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
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

// AmbiguousCandidate is one repository a selector matched, shaped for the
// structured ambiguity body. RepoID is the canonical follow-up handle and the
// only field every resolving branch guarantees. RepoName names the repository
// and MatchBasis names the selector field that matched (id, name, path,
// local_path, remote_url, or repo_slug); both are additive and empty when the
// resolving branch could not retain them (#10707).
type AmbiguousCandidate struct {
	RepoID     string `json:"repo_id"`
	RepoName   string `json:"repo_name,omitempty"`
	MatchBasis string `json:"match_basis,omitempty"`
}

// AmbiguousError is the errors.As target for a repository selector that
// matched more than one repository. Selector carries the raw input string;
// Matches carries the matched repositories (id, name, and the field that
// matched), so the caller can report which repositories collided and how.
type AmbiguousError struct {
	Selector string
	Matches  []AmbiguousCandidate
}

func (e AmbiguousError) Error() string {
	ids := make([]string, 0, len(e.Matches))
	for _, match := range e.Matches {
		ids = append(ids, match.RepoID)
	}
	return fmt.Sprintf("repository selector %q matched multiple repositories: %s", e.Selector, strings.Join(ids, ", "))
}

// AmbiguousCandidates normalizes raw matches into deterministic candidate
// objects: it drops empty ids, collapses duplicates by id, and sorts by id, so
// a directly constructed AmbiguousError with an empty or duplicated Matches
// list still answers a well-formed body without a panic (#10655).
func AmbiguousCandidates(matches []AmbiguousCandidate) []AmbiguousCandidate {
	seen := make(map[string]struct{}, len(matches))
	candidates := make([]AmbiguousCandidate, 0, len(matches))
	for _, match := range matches {
		if match.RepoID == "" {
			continue
		}
		if _, ok := seen[match.RepoID]; ok {
			continue
		}
		seen[match.RepoID] = struct{}{}
		candidates = append(candidates, match)
	}
	slices.SortFunc(candidates, func(a, b AmbiguousCandidate) int {
		return strings.Compare(a.RepoID, b.RepoID)
	})
	return candidates
}

// AmbiguousDetails renders the structured ambiguity packet shared by the
// plain and envelope bodies: status, the caller's selector, the normalized
// candidates, and truncated false, since resolution returns every match.
func AmbiguousDetails(err AmbiguousError) map[string]any {
	return map[string]any{
		"status":     "ambiguous",
		"selector":   err.Selector,
		"candidates": AmbiguousCandidates(err.Matches),
		"truncated":  false,
	}
}

// WriteAmbiguousError answers a repository-selector AmbiguousError for a
// caller that maps selector errors itself, and reports whether it wrote.
// An ambiguous match answers 409 with the structured body (the codemodel
// complexity precedent, #10655): an envelope request gets the ErrorEnvelope
// with the stable ambiguous code, any other request the same packet merged
// with the error and detail text. Every other error writes nothing and
// reports false, so the caller's own not-found mapping runs next. Call it
// after querycontract.WriteGraphReadError and WriteLookupFailure, in the same
// order as writeResolveFailure. The 404 and 500 answers keep their helpers.
func WriteAmbiguousError(w http.ResponseWriter, r *http.Request, err error, capability string) bool {
	var ambiguous AmbiguousError
	if !errors.As(err, &ambiguous) {
		return false
	}
	details := AmbiguousDetails(ambiguous)
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteErrorEnvelope(w, r, http.StatusConflict, &querycontract.ErrorEnvelope{
			Code:       querycontract.ErrorCodeAmbiguous,
			Message:    ambiguous.Error(),
			Capability: capability,
			Details:    details,
		})
		return true
	}
	body := make(map[string]any, len(details)+3)
	for key, value := range details {
		body[key] = value
	}
	body["code"] = querycontract.ErrorCodeAmbiguous
	body["error"] = http.StatusText(http.StatusConflict)
	body["detail"] = ambiguous.Error()
	querycontract.WriteJSON(w, http.StatusConflict, body)
	return true
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

// Error renders LookupFailureMessage followed by the backend error text, or
// LookupFailureMessage alone when Err is nil. The text is for logs and spans,
// never a response body.
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
			return matches[0].RepoID, nil
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
			RETURN r.id as id,
			       r.name as name,
			       CASE
			         WHEN r.id = $repo_selector THEN 'id'
			         WHEN r.name = $repo_selector THEN 'name'
			         WHEN r.path = $repo_selector THEN 'path'
			         WHEN r.local_path = $repo_selector THEN 'local_path'
			         WHEN r.remote_url = $repo_selector THEN 'remote_url'
			         WHEN r.repo_slug = $repo_selector THEN 'repo_slug'
			       END as match_basis
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
			matches := make([]AmbiguousCandidate, 0, len(rows))
			for _, row := range rows {
				id := querycontract.StringVal(row, "id")
				if id == "" {
					continue
				}
				matches = append(matches, AmbiguousCandidate{
					RepoID:     id,
					RepoName:   querycontract.StringVal(row, "name"),
					MatchBasis: querycontract.StringVal(row, "match_basis"),
				})
			}
			slices.SortFunc(matches, func(a, b AmbiguousCandidate) int {
				return strings.Compare(a.RepoID, b.RepoID)
			})
			return "", AmbiguousError{Selector: selector, Matches: matches}
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
// selector answers 404 with its own typed text; an ambiguous match answers
// 409 with the structured candidate body (#10655). Any other error answers
// the same fixed 500 as a lookup failure (#7674).
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
		writeResolveFailure(w, r, err, capability)
		return "", false
	}
	return repoID, true
}

// writeResolveFailure answers a failed ResolveExactForAccess in the order
// ResolveForRequestWithAccess documents. The 404 body is the typed answer's
// own text, which quotes only the caller's selector; an ambiguous match
// answers 409 through WriteAmbiguousError with the structured candidate body,
// whose ids the caller's grant may read. An error that is none of
// the classified answers is a server fault and answers LookupFailureMessage,
// so no unclassified error text reaches a 4xx body (#7674).
func writeResolveFailure(w http.ResponseWriter, r *http.Request, err error, capability string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	if WriteLookupFailure(w, r, err) {
		return
	}
	var notFound NotFoundError
	if errors.As(err, &notFound) {
		querycontract.WriteError(w, http.StatusNotFound, notFound.Error())
		return
	}
	if WriteAmbiguousError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, LookupFailureMessage)
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

// WriteLookupFailure answers a lookup failure for a caller that maps selector
// errors itself: when IsLookupFailure(err) holds it answers through
// tracing.WriteServerFailure with LookupFailureMessage as the body (500 with
// the error recorded on the request span, or 499 with only the client-cancel
// event when the caller canceled the request) and reports true. Any other
// error writes nothing and reports false, so the caller's own not-found and
// 400 mapping runs next. Call it after querycontract.WriteGraphReadError,
// which owns the fence and graph-availability verdicts that also wrap a
// LookupError.
func WriteLookupFailure(w http.ResponseWriter, r *http.Request, err error) bool {
	if !IsLookupFailure(err) {
		return false
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, LookupFailureMessage)
	return true
}

// LooksCanonicalRepositoryID reports whether a selector already has the shape
// of a canonical repository id, so a caller can skip catalog matching.
func LooksCanonicalRepositoryID(selector string) bool {
	return strings.HasPrefix(selector, "repo://") ||
		strings.HasPrefix(selector, "repo-") ||
		strings.HasPrefix(selector, "repository:")
}

// CatalogMatches returns the repositories whose catalog entry matches the
// selector, one candidate per entry in id order, with the repository name and
// the selector field that matched (#10707). The caller decides what a zero,
// one, or many result means.
func CatalogMatches(entries []querycontract.RepositoryCatalogEntry, selector string) []AmbiguousCandidate {
	if strings.TrimSpace(selector) == "" {
		return nil
	}
	matches := make([]AmbiguousCandidate, 0, 1)
	seen := make(map[string]struct{})
	for _, entry := range entries {
		basis := repositoryMatchBasis(entry, selector)
		if basis == "" {
			continue
		}
		if entry.ID == "" {
			continue
		}
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		seen[entry.ID] = struct{}{}
		matches = append(matches, AmbiguousCandidate{RepoID: entry.ID, RepoName: entry.Name, MatchBasis: basis})
	}
	slices.SortFunc(matches, func(a, b AmbiguousCandidate) int {
		return strings.Compare(a.RepoID, b.RepoID)
	})
	return matches
}

// repositoryMatchBasis names the field of entry the selector matched, or ""
// when it matches none. Fields are checked in the resolution order: id, name,
// path, local_path, remote_url, repo_slug.
func repositoryMatchBasis(entry querycontract.RepositoryCatalogEntry, selector string) string {
	switch selector {
	case entry.ID:
		return "id"
	case entry.Name:
		return "name"
	case entry.Path:
		return "path"
	case entry.LocalPath:
		return "local_path"
	case entry.RemoteURL:
		return "remote_url"
	case entry.RepoSlug:
		return "repo_slug"
	default:
		return ""
	}
}
