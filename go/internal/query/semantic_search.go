// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SemanticSearchHandler exposes bounded curated search-document retrieval.
type SemanticSearchHandler struct {
	Index       SemanticSearchIndexStore
	LocalHybrid SemanticSearchHybridStore
	Profile     QueryProfile
}

// SemanticSearchIndexStore searches a persisted curated search-document index
// for one repository-scoped corpus.
type SemanticSearchIndexStore interface {
	Search(context.Context, semanticSearchIndexQuery) (semanticSearchIndexResult, error)
}

type semanticSearchIndexQuery struct {
	Request     searchretrieval.Request
	ScopeID     string
	RepoID      string
	SourceKinds []searchdocs.SourceKind
	// Languages filters the bounded corpus to documents whose Labels contain
	// "language:<lang>" for one of the requested languages. Empty means no filter.
	Languages []string
}

type semanticSearchIndexResult struct {
	Candidates           []searchretrieval.Candidate
	IndexedDocumentCount int
	CorpusLimit          int
	CorpusMayBeTruncated bool
	RetrievalState       string
}

type semanticSearchRequest struct {
	RepoID      string   `json:"repo_id"`
	Query       string   `json:"query"`
	Mode        string   `json:"mode"`
	Limit       int      `json:"limit"`
	TimeoutMS   int      `json:"timeout_ms"`
	ServiceID   string   `json:"service_id,omitempty"`
	WorkloadID  string   `json:"workload_id,omitempty"`
	Environment string   `json:"environment,omitempty"`
	SourceKinds []string `json:"source_kinds,omitempty"`
	// Languages filters the corpus to documents whose Labels contain
	// "language:<lang>" for one of the requested languages. An empty slice
	// means no language filter. Unknown language values are rejected with
	// HTTP 400.
	Languages []string `json:"languages,omitempty"`
	// Rerank opts the request into graph-neighborhood reranking over the
	// retrieved in-scope results. Off by default; when on, the response reports
	// the reranking state, per-result ranking basis, and recommended next calls.
	Rerank bool `json:"rerank,omitempty"`
}

// semanticSearchFacets carries per-facet counts over the already-bounded
// in-scope candidate corpus.
type semanticSearchFacets struct {
	// Languages maps each "language:<x>" label value (the "<x>" part) to the
	// count of results carrying that language in the post-filter result set.
	Languages map[string]int `json:"languages"`
}

type semanticSearchResponse struct {
	Query                    string                 `json:"query"`
	RepoID                   string                 `json:"repo_id"`
	Anchor                   searchretrieval.Anchor `json:"anchor"`
	Mode                     searchbench.Mode       `json:"mode"`
	SearchMode               string                 `json:"search_mode"`
	Limit                    int                    `json:"limit"`
	TimeoutMS                int                    `json:"timeout_ms"`
	Results                  []semanticSearchResult `json:"results"`
	Truncated                bool                   `json:"truncated"`
	FalseCanonicalClaimCount int                    `json:"false_canonical_claim_count"`
	IndexedDocumentCount     int                    `json:"indexed_document_count"`
	CorpusLimit              int                    `json:"corpus_limit"`
	CorpusMayBeTruncated     bool                   `json:"corpus_may_be_truncated"`
	RetrievalState           string                 `json:"retrieval_state"`
	// Facets carries per-dimension aggregate counts computed over the
	// post-filter result set. The block is always present (never omitted) so
	// callers can rely on the shape unconditionally.
	Facets               semanticSearchFacets  `json:"facets"`
	Rerank               *semanticSearchRerank `json:"rerank,omitempty"`
	RecommendedNextCalls []semanticSearchCall  `json:"recommended_next_calls,omitempty"`
}

type semanticSearchResult struct {
	Rank         int                         `json:"rank"`
	Score        float64                     `json:"score"`
	SearchMethod string                      `json:"search_method"`
	Document     semanticSearchDocument      `json:"document"`
	GraphHandles []semanticSearchGraphHandle `json:"graph_handles"`
	TruthScope   semanticSearchTruthScope    `json:"truth_scope"`
	Freshness    semanticSearchFreshness     `json:"freshness"`
	Failures     []searchbench.FailureClass  `json:"failures,omitempty"`
	Metadata     map[string]string           `json:"metadata,omitempty"`
	RankingBasis *semanticSearchRankingBasis `json:"ranking_basis,omitempty"`
}

type semanticSearchDocument struct {
	ID           string                      `json:"id"`
	RepoID       string                      `json:"repo_id"`
	SourceKind   searchdocs.SourceKind       `json:"source_kind"`
	Title        string                      `json:"title"`
	Path         string                      `json:"path,omitempty"`
	ContextText  string                      `json:"context_text,omitempty"`
	EntityRefs   []semanticSearchEntityRef   `json:"entity_refs,omitempty"`
	GraphHandles []semanticSearchGraphHandle `json:"graph_handles"`
	Labels       []string                    `json:"labels,omitempty"`
	UpdatedAt    time.Time                   `json:"updated_at,omitempty"`
	TruthScope   semanticSearchTruthScope    `json:"truth_scope"`
	Freshness    semanticSearchFreshness     `json:"freshness"`
	AccessScope  semanticSearchAccessScope   `json:"access_scope"`
	Provenance   semanticSearchProvenance    `json:"provenance"`
}

type semanticSearchEntityRef struct {
	ID        string `json:"id"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type semanticSearchGraphHandle struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type semanticSearchTruthScope struct {
	Level searchdocs.TruthLevel `json:"level"`
	Basis searchdocs.TruthBasis `json:"basis"`
}

type semanticSearchFreshness struct {
	State searchdocs.FreshnessState `json:"state"`
}

type semanticSearchAccessScope struct {
	RepoID string `json:"repo_id,omitempty"`
}

type semanticSearchProvenance struct {
	SourceTable string   `json:"source_table,omitempty"`
	SourceIDs   []string `json:"source_ids,omitempty"`
}

// Mount registers semantic-search routes.
func (h *SemanticSearchHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/search/semantic", h.search)
}

func (h *SemanticSearchHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *SemanticSearchHandler) search(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySemanticSearch,
		"POST /api/v0/search/semantic",
		semanticSearchCapability,
	)
	defer span.End()

	if !authContextAllowsPermissionFeature(r.Context(), permissionFeatureAskSearch) {
		writeSemanticSearchError(
			w,
			r,
			http.StatusForbidden,
			ErrorCodePermissionDenied,
			"permission denied",
		)
		return
	}
	if !authContextAllowsPermissionDataClasses(r.Context(), permissionDataClassesAskSearch...) {
		writeSemanticSearchError(
			w,
			r,
			http.StatusForbidden,
			ErrorCodePermissionDenied,
			"permission denied",
		)
		return
	}

	if capabilityUnsupported(h.profile(), semanticSearchCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"semantic search requires curated search-document retrieval",
			ErrorCodeUnsupportedCapability,
			semanticSearchCapability,
			h.profile(),
			requiredProfile(semanticSearchCapability),
		)
		return
	}

	var body semanticSearchRequest
	if err := ReadJSON(r, &body); err != nil {
		writeSemanticSearchError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}
	body = normalizeSemanticSearchRequest(body)
	sourceKinds, err := semanticSearchSourceKinds(body.SourceKinds)
	if err != nil {
		writeSemanticSearchError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}
	languages, err := semanticSearchLanguages(body.Languages)
	if err != nil {
		writeSemanticSearchError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}
	req, err := semanticSearchRetrievalRequest(body)
	if err != nil {
		writeSemanticSearchError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		WriteSuccess(w, r, http.StatusOK, emptySemanticSearchResponse(req), h.truth())
		return
	}
	if !access.allowsRepositoryID(body.RepoID) {
		writeSemanticSearchError(w, r, http.StatusNotFound, ErrorCodeNotFound, "repository not found")
		return
	}
	var indexResult semanticSearchIndexResult
	backend, err := h.semanticSearchBackend(req, body, sourceKinds, languages, &indexResult)
	if err != nil {
		writeSemanticSearchError(
			w,
			r,
			http.StatusServiceUnavailable,
			ErrorCodeBackendUnavailable,
			err.Error(),
		)
		return
	}

	retrieval, err := (searchretrieval.Runner{
		Backend: backend,
	}).Retrieve(r.Context(), req)
	if err != nil {
		status, code := semanticSearchRetrievalError(err)
		writeSemanticSearchError(w, r, status, code, err.Error())
		return
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		semanticSearchResponseFromRetrieval(req, retrieval, indexResult, body.Rerank),
		h.truth(),
	)
}

func (h *SemanticSearchHandler) semanticSearchBackend(
	req searchretrieval.Request,
	body semanticSearchRequest,
	sourceKinds []searchdocs.SourceKind,
	languages []string,
	indexResult *semanticSearchIndexResult,
) (searchretrieval.Backend, error) {
	query := semanticSearchIndexQuery{
		Request: req,
		// The first public slice is repository-bounded; repository id is the
		// active ingestion scope used by the durable search-document index.
		ScopeID:     body.RepoID,
		RepoID:      body.RepoID,
		SourceKinds: sourceKinds,
		Languages:   languages,
	}
	if h.LocalHybrid != nil && (req.Mode == searchbench.ModeSemantic || req.Mode == searchbench.ModeHybrid) {
		return semanticSearchIndexBackend{
			Index:         h.LocalHybrid,
			Query:         query,
			Result:        indexResult,
			AllowSemantic: true,
		}, nil
	}
	if h.Index == nil {
		return nil, errors.New("semantic search requires the persisted search index")
	}
	return semanticSearchIndexBackend{
		Index:  h.Index,
		Query:  query,
		Result: indexResult,
	}, nil
}

type semanticSearchIndexBackend struct {
	Index         SemanticSearchIndexStore
	Query         semanticSearchIndexQuery
	Result        *semanticSearchIndexResult
	AllowSemantic bool
}

func (backend semanticSearchIndexBackend) Search(
	ctx context.Context,
	req searchretrieval.Request,
) ([]searchretrieval.Candidate, error) {
	if req.Mode == searchbench.ModeSemantic && !backend.AllowSemantic {
		return nil, errors.New("semantic mode requires an embedder")
	}
	query := backend.Query
	query.Request = req
	result, err := backend.Index.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	if result.RetrievalState == "" {
		result.RetrievalState = defaultSemanticSearchRetrievalState(req.Mode)
	}
	if backend.Result != nil {
		*backend.Result = result
	}
	return result.Candidates, nil
}

func semanticSearchRetrievalError(err error) (int, ErrorCode) {
	message := err.Error()
	if strings.Contains(message, "semantic mode requires an embedder") {
		return http.StatusServiceUnavailable, ErrorCodeBackendUnavailable
	}
	if strings.Contains(message, "search retrieval backend") {
		return http.StatusInternalServerError, ErrorCodeInternalError
	}
	return http.StatusBadRequest, ErrorCodeInvalidArgument
}

func (h *SemanticSearchHandler) truth() *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		semanticSearchCapability,
		TruthBasisHybrid,
		"resolved from a persisted curated search-document index",
	)
}

func writeSemanticSearchError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Error: &ErrorEnvelope{
			Code:       code,
			Message:    message,
			Capability: semanticSearchCapability,
		}})
		return
	}
	WriteJSON(w, status, map[string]any{
		"error_code": code,
		"message":    message,
		"capability": semanticSearchCapability,
	})
}
