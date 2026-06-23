package query

import (
	"context"
	"errors"
	"fmt"
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
	// Rerank opts the request into graph-neighborhood reranking over the
	// retrieved in-scope results. Off by default; when on, the response reports
	// the reranking state, per-result ranking basis, and recommended next calls.
	Rerank bool `json:"rerank,omitempty"`
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
	Rerank                   *semanticSearchRerank  `json:"rerank,omitempty"`
	RecommendedNextCalls     []semanticSearchCall   `json:"recommended_next_calls,omitempty"`
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
	backend, err := h.semanticSearchBackend(req, body, sourceKinds, &indexResult)
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
	indexResult *semanticSearchIndexResult,
) (searchretrieval.Backend, error) {
	query := semanticSearchIndexQuery{
		Request: req,
		// The first public slice is repository-bounded; repository id is the
		// active ingestion scope used by the durable search-document index.
		ScopeID:     body.RepoID,
		RepoID:      body.RepoID,
		SourceKinds: sourceKinds,
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

func normalizeSemanticSearchRequest(req semanticSearchRequest) semanticSearchRequest {
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.Query = strings.TrimSpace(req.Query)
	req.Mode = strings.TrimSpace(req.Mode)
	req.ServiceID = strings.TrimSpace(req.ServiceID)
	req.WorkloadID = strings.TrimSpace(req.WorkloadID)
	req.Environment = strings.TrimSpace(req.Environment)
	for i, kind := range req.SourceKinds {
		req.SourceKinds[i] = strings.TrimSpace(kind)
	}
	return req
}

func semanticSearchRetrievalRequest(body semanticSearchRequest) (searchretrieval.Request, error) {
	if body.RepoID == "" {
		return searchretrieval.Request{}, errors.New("repo_id is required")
	}
	req := searchretrieval.Request{
		Query: body.Query,
		Scope: searchretrieval.Scope{
			ServiceID:   body.ServiceID,
			WorkloadID:  body.WorkloadID,
			RepoID:      body.RepoID,
			Environment: body.Environment,
		},
		Mode:    searchbench.Mode(body.Mode),
		Limit:   body.Limit,
		Timeout: time.Duration(body.TimeoutMS) * time.Millisecond,
	}
	if err := searchretrieval.ValidateRequest(req); err != nil {
		return searchretrieval.Request{}, err
	}
	return req, nil
}

func semanticSearchSourceKinds(raw []string) ([]searchdocs.SourceKind, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	kinds := make([]searchdocs.SourceKind, 0, len(raw))
	for _, value := range raw {
		switch kind := searchdocs.SourceKind(strings.TrimSpace(value)); kind {
		case searchdocs.SourceKindCodeEntity,
			searchdocs.SourceKindRepositoryFile,
			searchdocs.SourceKindRuntimeSummary,
			searchdocs.SourceKindSemanticContext:
			kinds = append(kinds, kind)
		case "":
			continue
		default:
			return nil, fmt.Errorf("source_kinds contains unsupported value %q", value)
		}
	}
	return kinds, nil
}

func emptySemanticSearchResponse(req searchretrieval.Request) semanticSearchResponse {
	return semanticSearchResponse{
		Query:          req.Query,
		RepoID:         req.Scope.RepoID,
		Anchor:         req.Scope.Anchor(),
		Mode:           req.Mode,
		SearchMode:     string(req.Mode),
		Limit:          req.Limit,
		TimeoutMS:      int(req.Timeout / time.Millisecond),
		Results:        []semanticSearchResult{},
		CorpusLimit:    0,
		Truncated:      false,
		RetrievalState: defaultSemanticSearchRetrievalState(req.Mode),
	}
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
