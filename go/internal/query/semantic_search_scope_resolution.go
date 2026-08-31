// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

type semanticSearchScopeResolution struct {
	scopeID      string
	repositoryID string
	ambiguous    bool
}

// semanticSearchIngestionScopePrefix marks a request id that addresses a
// repository by its ingestion scope id ("git-repository-scope:<repo_id>")
// instead of by its canonical repository id.
const semanticSearchIngestionScopePrefix = "git-repository-scope:"

// resolveScope selects the active search-document scope without adding a
// direct-scope lookup to canonical all-scope requests. Scoped grants retain
// their existing direct-versus-canonical authorization boundary.
//
// The invariant every caller depends on: repositoryID is a canonical repository
// id or the resolution is empty. A scope id must never survive as repositoryID,
// because retrieval compares the anchor built from it against the canonical id
// stored on each document, matches nothing, and returns zero results next to a
// healthy indexed_document_count (#5052). Every path that accepts a scope id
// therefore either returns the canonical id its ingestion scope carries or
// returns an empty resolution, which the handler turns into an empty bounded
// response.
//
// The nil-resolver path is the exception, and it is the degenerate local/test
// wiring: with no resolver there is no mapping to rebind to at all. Both
// services wire one (cmd/api and cmd/mcp-server).
func (h *SemanticSearchHandler) resolveScope(
	ctx context.Context,
	requestedID string,
	access repositoryAccessFilter,
	directScopeGrant bool,
	canonicalRepositoryGrant bool,
) (semanticSearchScopeResolution, error) {
	if h.ScopeResolver == nil {
		return semanticSearchScopeResolution{scopeID: requestedID, repositoryID: requestedID}, nil
	}
	// The two scope-addressed shapes -- an all-scopes caller passing a
	// "git-repository-scope:" id, and a scoped caller whose grant names the
	// scope id directly -- need the same lookup, so they share one branch. Kept
	// apart, they invite a change that lands on one and misses the other.
	scopeAddressed := (access.AllScopes && strings.HasPrefix(requestedID, semanticSearchIngestionScopePrefix)) ||
		(directScopeGrant && !canonicalRepositoryGrant)
	if scopeAddressed {
		resolvedRepoID, err := h.ScopeResolver.ResolveSemanticSearchRepositoryForScope(ctx, requestedID)
		if err != nil {
			return semanticSearchScopeResolution{}, err
		}
		if resolvedRepoID == "" {
			// No active repository scope owns the id, so there is no canonical
			// repository to bound the read by. The all-scopes half used to fall
			// through to the canonical lookup below, which could only ever come
			// back empty: that query matches ingestion_scopes.payload->>'repo_id',
			// and that column holds canonical ids. Same empty response, one
			// fewer round trip.
			return semanticSearchScopeResolution{}, nil
		}
		return semanticSearchScopeResolution{scopeID: requestedID, repositoryID: resolvedRepoID}, nil
	}
	// Defensive, not a fix for an observed failure. A scope-prefixed id reaching
	// here means no branch above claimed it -- today that needs a grant naming
	// the same scope id as both a scope and a repository. The canonical lookup
	// below would then hand the scope id back as repositoryID and break the
	// invariant above. It cannot produce a non-empty scope for a scope-prefixed
	// id anyway, but that safety lives in a query in another file over data
	// another package writes, so pin it here where the invariant is stated.
	if strings.HasPrefix(requestedID, semanticSearchIngestionScopePrefix) {
		return semanticSearchScopeResolution{}, nil
	}

	resolvedScopeID, err := h.ScopeResolver.ResolveSemanticSearchScope(ctx, requestedID)
	if err != nil {
		return semanticSearchScopeResolution{
			ambiguous: errors.Is(err, ErrSemanticSearchScopeAmbiguous),
		}, err
	}
	return semanticSearchScopeResolution{scopeID: resolvedScopeID, repositoryID: requestedID}, nil
}

// semanticSearchCanonicalAnchorRequest rebinds the retrieval anchor to the
// canonical repository id resolved for the request.
//
// A caller may address a repository either by its canonical id or by its
// ingestion scope id ("git-repository-scope:<repo_id>"). resolveScope accepts
// both and hands back the scope that owns the index generation plus the
// canonical repository id. Retrieval, however, filters candidates by comparing
// the anchor id against the repository identity stored on each document --
// searchdocs.Document.RepoID in the in-memory backend and
// eshu_search_index_documents.repo_id in the persisted BM25 query. Those always
// hold the canonical id, so leaving the scope id on the anchor rejected every
// document and returned an empty result set alongside a healthy
// indexed_document_count (#5052).
//
// A canonical-id request is unaffected: repositoryID equals the requested id, so
// the anchor is unchanged. The span records whether a rewrite happened, so an
// operator investigating a search can tell which id form the caller used.
func semanticSearchCanonicalAnchorRequest(
	span trace.Span,
	req searchretrieval.Request,
	repositoryID string,
) searchretrieval.Request {
	repositoryID = strings.TrimSpace(repositoryID)
	rewritten := repositoryID != "" && repositoryID != req.Scope.RepoID
	if span != nil {
		span.SetAttributes(attribute.Bool("search.anchor_rewritten_to_canonical_repository", rewritten))
	}
	if rewritten {
		req.Scope.RepoID = repositoryID
	}
	return req
}
