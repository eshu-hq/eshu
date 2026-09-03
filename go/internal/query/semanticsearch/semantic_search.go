// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Capability is the capability-catalog identifier this family serves under.
// It gates the route by query profile and labels every error envelope and
// truth envelope the handler writes.
//
// The string lives here, with the family that implements it, and root package
// query's capability matrix reads it from here (#6060). One declaration keeps
// the profile gate, the registered support matrix, and the wire envelopes
// naming the same capability; two literals would drift into a route the matrix
// no longer describes.
const Capability = "semantic_search.curated_retrieval"

// askSearchDataClasses is the data-class set this route requires, resolved once
// at init rather than per request. PermissionDataClassesAskSearch returns a
// fresh slice on every call, so calling it inside the handler would allocate on
// the request path for a value that never changes.
var askSearchDataClasses = queryauth.PermissionDataClassesAskSearch()

// SemanticSearchHandler exposes bounded curated search-document retrieval.
type SemanticSearchHandler struct {
	Index         SemanticSearchIndexStore
	LocalHybrid   SemanticSearchHybridStore
	ScopeResolver SemanticSearchScopeResolver
	Profile       querycontract.QueryProfile
	// SearchVectorReady optionally reports the search-vector build sweep's
	// search_vector_ready watermark. When set, the response's truth freshness
	// is downgraded (pending_search_vector cause) if the sweep has never
	// published the signal, is behind its publish cadence, or the probe
	// itself failed. Nil keeps the envelope fresh (no configured signal).
	SearchVectorReady SemanticSearchVectorReadyReader
}

// SemanticSearchIndexStore searches a persisted curated search-document index
// for one repository-scoped corpus.
type SemanticSearchIndexStore interface {
	Search(context.Context, SemanticSearchIndexQuery) (SemanticSearchIndexResult, error)
}

// SemanticSearchIndexQuery is one bounded retrieval against the curated
// search-document index. ScopeID is the resolved active ingestion scope the
// index reads; RepoID stays the authorized canonical repository identity, and
// the two differ whenever a repository has been re-ingested under a new scope.
// Both are set: a store that filters on only one of them answers outside the
// caller's grant.
//
// It is exported because SemanticSearchIndexStore is: an exported interface
// whose method signature names an unexported type cannot be implemented from
// any other package, which would leave cmd/api's wiring and root package
// query's authorization tests unable to supply a store at all (#6060).
type SemanticSearchIndexQuery struct {
	Request     searchretrieval.Request
	ScopeID     string
	RepoID      string
	SourceKinds []searchdocs.SourceKind
	// Languages filters the bounded corpus to documents whose Labels contain
	// "language:<lang>" for one of the requested languages. Empty means no filter.
	Languages []string
}

// SemanticSearchIndexResult is what a store returns for one
// SemanticSearchIndexQuery: the in-scope candidates plus the corpus bounds the
// response reports so a caller can tell a genuinely empty corpus from a
// truncated one.
//
// RetrievalState may be left empty by a store; the handler fills it with the
// mode's default before the response is built, so an empty value never reaches
// the wire. Exported for the same reason as SemanticSearchIndexQuery.
type SemanticSearchIndexResult struct {
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
	// means no language filter. Any non-empty lowercased token is accepted;
	// an unmatched language returns an empty result set rather than an error.
	// The index is the source of truth for which language values exist.
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

func (h *SemanticSearchHandler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *SemanticSearchHandler) search(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySemanticSearch,
		"POST /api/v0/search/semantic",
		Capability,
	)
	defer span.End()

	if !queryauth.AllowsPermissionFeature(r.Context(), queryauth.PermissionFeatureAskSearch) {
		writeSemanticSearchError(
			w,
			r,
			http.StatusForbidden,
			querycontract.ErrorCodePermissionDenied,
			"permission denied",
		)
		return
	}
	if !queryauth.AllowsPermissionDataClasses(r.Context(), askSearchDataClasses...) {
		writeSemanticSearchError(
			w,
			r,
			http.StatusForbidden,
			querycontract.ErrorCodePermissionDenied,
			"permission denied",
		)
		return
	}

	if querycontract.CapabilityUnsupported(h.profile(), Capability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"semantic search requires curated search-document retrieval",
			querycontract.ErrorCodeUnsupportedCapability,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}

	var body semanticSearchRequest
	if err := querycontract.ReadJSON(r, &body); err != nil {
		writeSemanticSearchError(w, r, http.StatusBadRequest, querycontract.ErrorCodeInvalidArgument, err.Error())
		return
	}
	body = normalizeSemanticSearchRequest(body)
	sourceKinds, err := semanticSearchSourceKinds(body.SourceKinds)
	if err != nil {
		writeSemanticSearchError(w, r, http.StatusBadRequest, querycontract.ErrorCodeInvalidArgument, err.Error())
		return
	}
	languages := semanticSearchLanguages(body.Languages)
	req, err := semanticSearchRetrievalRequest(body)
	if err != nil {
		writeSemanticSearchError(w, r, http.StatusBadRequest, querycontract.ErrorCodeInvalidArgument, err.Error())
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		querycontract.WriteSuccess(w, r, http.StatusOK, emptySemanticSearchResponse(req), h.truthWithSearchVectorFreshness(r, req.Mode))
		return
	}
	directScopeGrant := access.AllowsDirectScopeID(body.RepoID)
	canonicalRepositoryGrant := access.AllowsCanonicalRepositoryID(body.RepoID)
	if !directScopeGrant && !canonicalRepositoryGrant {
		writeSemanticSearchError(w, r, http.StatusNotFound, querycontract.ErrorCodeNotFound, "repository not found")
		return
	}
	resolution, err := h.resolveScope(
		r.Context(),
		body.RepoID,
		access,
		directScopeGrant,
		canonicalRepositoryGrant,
	)
	if err != nil {
		if resolution.ambiguous {
			writeSemanticSearchError(w, r, http.StatusConflict, querycontract.ErrorCodeAmbiguous, err.Error())
			return
		}
		writeSemanticSearchError(w, r, http.StatusServiceUnavailable, querycontract.ErrorCodeBackendUnavailable, err.Error())
		return
	}
	if resolution.scopeID == "" {
		querycontract.WriteSuccess(w, r, http.StatusOK, emptySemanticSearchResponse(req), h.truthWithSearchVectorFreshness(r, req.Mode))
		return
	}
	req = semanticSearchCanonicalAnchorRequest(span, req, resolution.repositoryID)
	var indexResult SemanticSearchIndexResult
	backend, err := h.semanticSearchBackend(
		req,
		resolution.scopeID,
		resolution.repositoryID,
		sourceKinds,
		languages,
		&indexResult,
	)
	if err != nil {
		writeSemanticSearchError(
			w,
			r,
			http.StatusServiceUnavailable,
			querycontract.ErrorCodeBackendUnavailable,
			err.Error(),
		)
		return
	}

	retrieval, err := (searchretrieval.Runner{
		Backend: backend,
	}).Retrieve(r.Context(), req)
	if err != nil {
		annotateSemanticSearchDegradedError(r.Context(), span, err)
		status, code := semanticSearchRetrievalError(err)
		writeSemanticSearchError(w, r, status, code, err.Error())
		return
	}

	annotateSemanticSearchDegraded(r.Context(), span, req.Mode, indexResult.RetrievalState)

	querycontract.WriteSuccess(
		w,
		r,
		http.StatusOK,
		semanticSearchResponseFromRetrieval(req, retrieval, indexResult, body.Rerank),
		h.truthWithSearchVectorFreshness(r, req.Mode),
	)
}

func (h *SemanticSearchHandler) semanticSearchBackend(
	req searchretrieval.Request,
	scopeID string,
	repoID string,
	sourceKinds []searchdocs.SourceKind,
	languages []string,
	indexResult *SemanticSearchIndexResult,
) (searchretrieval.Backend, error) {
	query := SemanticSearchIndexQuery{
		Request: req,
		// The public slice is repository-bounded. ScopeID is the resolved active
		// ingestion scope; RepoID remains the authorized canonical identity.
		ScopeID:     scopeID,
		RepoID:      repoID,
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
	Query         SemanticSearchIndexQuery
	Result        *SemanticSearchIndexResult
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

func semanticSearchRetrievalError(err error) (int, querycontract.ErrorCode) {
	message := err.Error()
	if strings.Contains(message, "semantic mode requires an embedder") {
		return http.StatusServiceUnavailable, querycontract.ErrorCodeBackendUnavailable
	}
	if strings.Contains(message, "search retrieval backend") {
		return http.StatusInternalServerError, querycontract.ErrorCodeInternalError
	}
	return http.StatusBadRequest, querycontract.ErrorCodeInvalidArgument
}

func (h *SemanticSearchHandler) truth() *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisHybrid,
		"resolved from a persisted curated search-document index",
	)
}

// truthWithSearchVectorFreshness builds the response truth envelope and, when
// a search-vector-ready reader is configured AND the resolved mode is
// vector-backed (semantic or hybrid — the same gate semanticSearchBackend
// uses to decide whether LocalHybrid's vector path is even wired in),
// downgrades it from the search-vector build sweep's search_vector_ready
// watermark so an outstanding build is attributable (pending_search_vector)
// instead of served as silently fresh. mode:"keyword" is served entirely by
// the deterministic lexical index and is never degraded by vector/index
// readiness (see semanticSearchDegradation), so it must never be downgraded
// by a pending search-vector build. Mirrors applyWinnersFreshness's call-site
// shape in supply_chain_impact_findings_handler.go: a probe failure reports
// the envelope unavailable rather than dropping the already-served results.
func (h *SemanticSearchHandler) truthWithSearchVectorFreshness(r *http.Request, mode searchbench.Mode) *querycontract.TruthEnvelope {
	truth := h.truth()
	if h.SearchVectorReady == nil || !searchVectorBackedMode(mode) {
		return truth
	}
	watermark, err := h.SearchVectorReady.SearchVectorReadyWatermark(r.Context())
	applySearchVectorFreshness(truth, watermark, err, time.Now())
	return truth
}

// searchVectorBackedMode reports whether mode retrieves through the
// search-vector index (semantic or hybrid), matching the gate
// semanticSearchBackend uses to decide whether LocalHybrid's vector path is
// engaged. mode:"keyword" is served entirely by the deterministic lexical
// index and never touches search-vector state.
func searchVectorBackedMode(mode searchbench.Mode) bool {
	return mode == searchbench.ModeSemantic || mode == searchbench.ModeHybrid
}

func writeSemanticSearchError(w http.ResponseWriter, r *http.Request, status int, code querycontract.ErrorCode, message string) {
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteJSON(w, status, querycontract.ResponseEnvelope{Error: &querycontract.ErrorEnvelope{
			Code:       code,
			Message:    message,
			Capability: Capability,
		}})
		return
	}
	querycontract.WriteJSON(w, status, map[string]any{
		"error_code": code,
		"message":    message,
		"capability": Capability,
	})
}
