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

// resolveScope selects the active search-document scope without adding a
// direct-scope lookup to canonical all-scope requests. Scoped grants retain
// their existing direct-versus-canonical authorization boundary.
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
	if access.allScopes && strings.HasPrefix(requestedID, "git-repository-scope:") {
		resolvedRepoID, err := h.ScopeResolver.ResolveSemanticSearchRepositoryForScope(ctx, requestedID)
		if err != nil {
			return semanticSearchScopeResolution{}, err
		}
		if resolvedRepoID != "" {
			return semanticSearchScopeResolution{scopeID: requestedID, repositoryID: resolvedRepoID}, nil
		}
	}
	if directScopeGrant && !canonicalRepositoryGrant {
		resolvedRepoID, err := h.ScopeResolver.ResolveSemanticSearchRepositoryForScope(ctx, requestedID)
		if err != nil {
			return semanticSearchScopeResolution{}, err
		}
		if resolvedRepoID == "" {
			return semanticSearchScopeResolution{}, nil
		}
		return semanticSearchScopeResolution{scopeID: requestedID, repositoryID: resolvedRepoID}, nil
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
