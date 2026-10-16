// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Fixed client-facing messages for the failures of
// POST /api/v0/search/semantic (#7674). The body carries exactly one of these:
// a scope-resolver or index error quotes SQL, relation names, hosts, and
// stored document ids, so err.Error() never reaches a 5xx body. The cause is
// recorded on the handler span instead.
const (
	// semanticSearchScopeResolutionFailedMessage answers a failed lookup of
	// the active ingestion scope for the requested repository.
	semanticSearchScopeResolutionFailedMessage = "semantic search scope resolution failed"
	// semanticSearchRetrievalFailedMessage answers a failed index or local
	// hybrid retrieval, and a retrieved candidate that fails response
	// normalization.
	semanticSearchRetrievalFailedMessage = "semantic search retrieval failed"
	// semanticSearchIndexNotConfiguredMessage is the configuration 503 when
	// no persisted search index is wired on this API instance.
	semanticSearchIndexNotConfiguredMessage = "semantic search requires the persisted search index"
	// semanticSearchEmbedderRequiredMessage is the configuration 503 when
	// semantic mode is requested and no embedder is wired.
	semanticSearchEmbedderRequiredMessage = "semantic mode requires an embedder"
)

// errSemanticSearchIndexNotConfigured and errSemanticModeRequiresEmbedder are
// the two configuration verdicts the handler answers 503 with no Retry-After.
// The retrieval runner wraps the embedder error with %w, so the handler tells
// it apart with errors.Is.
var (
	errSemanticSearchIndexNotConfigured = errors.New(semanticSearchIndexNotConfiguredMessage)
	errSemanticModeRequiresEmbedder     = errors.New(semanticSearchEmbedderRequiredMessage)
)

// writeSemanticSearchFailure answers a failed scope-resolution or retrieval
// read. Both read through the guarded PostgreSQL reader, so
// querycontract.WriteGraphReadError runs first: a stale or timed-out reader
// answers the retryable 503 with Retry-After under Capability. Anything else
// keeps the route's own error shape through tracing.ServerFailureEnvelope:
// 500 internal_error with err recorded on the handler span, or 499 with only
// the client-cancel event when the caller canceled its own request. err's
// text never reaches the body.
func writeSemanticSearchFailure(w http.ResponseWriter, r *http.Request, err error, message string) {
	if querycontract.WriteGraphReadError(w, r, err, Capability) {
		return
	}
	status, errEnv := tracing.ServerFailureEnvelope(r.Context(), err, message, Capability)
	writeSemanticSearchError(w, r, status, errEnv.Code, errEnv.Message)
}
