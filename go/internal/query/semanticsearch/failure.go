// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"errors"
	"net/http"
	"strconv"

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
// read. Both read through the guarded PostgreSQL reader, so the shared reader
// verdict comes first (writeSemanticSearchReaderVerdict): a stale or
// timed-out reader answers the retryable 503 with Retry-After under
// Capability. Anything else keeps the route's own error shape through
// tracing.ServerFailureEnvelope: 500 internal_error with err recorded on the
// handler span, or 499 with only the client-cancel event when the caller
// canceled its own request. err's text never reaches the body.
func writeSemanticSearchFailure(w http.ResponseWriter, r *http.Request, err error, message string) {
	if writeSemanticSearchReaderVerdict(w, r, err) {
		return
	}
	status, errEnv := tracing.ServerFailureEnvelope(r.Context(), err, message, Capability)
	writeSemanticSearchError(w, r, status, errEnv.Code, errEnv.Message)
}

// writeSemanticSearchReaderVerdict answers the shared 503 reader verdict for
// err in the route's own error shape, and reports whether it wrote one. A
// caller that negotiated the envelope gets querycontract.WriteErrorEnvelope's
// envelope, whose details carry retry_after_seconds for MCP callers. A plain
// caller gets the route's {error_code, message, capability} body, not the
// shared {error, detail} one. Both get Retry-After when the verdict is
// transient. These reads touch no graph and run on no shared snapshot, so
// only the 503 verdicts apply; anything else, a 504 graph verdict included,
// is left to the caller's server-failure path.
func writeSemanticSearchReaderVerdict(w http.ResponseWriter, r *http.Request, err error) bool {
	status, errEnv, ok := querycontract.GraphReadErrorEnvelope(err, Capability)
	if !ok || status != http.StatusServiceUnavailable {
		return false
	}
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteErrorEnvelope(w, r, http.StatusServiceUnavailable, errEnv)
		return true
	}
	if errEnv.Retryable() {
		w.Header().Set("Retry-After", strconv.Itoa(querycontract.BackendUnavailableRetryAfterSeconds))
	}
	writeSemanticSearchError(w, r, http.StatusServiceUnavailable, errEnv.Code, errEnv.Message)
	return true
}
