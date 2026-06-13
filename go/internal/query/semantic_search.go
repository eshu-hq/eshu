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
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const semanticSearchCorpusLimit = 500

// SemanticSearchHandler exposes bounded curated search-document retrieval.
type SemanticSearchHandler struct {
	Documents SemanticSearchDocumentStore
	Profile   QueryProfile
}

// SemanticSearchDocumentStore loads active curated search documents for one
// repository-scoped corpus.
type SemanticSearchDocumentStore interface {
	ListActiveDocuments(context.Context, semanticSearchDocumentFilter) ([]semanticSearchDocumentRow, error)
}

type semanticSearchDocumentFilter struct {
	ScopeID     string
	RepoID      string
	SourceKinds []searchdocs.SourceKind
	Limit       int
	Offset      int
}

type semanticSearchDocumentRow struct {
	FactID       string
	ScopeID      string
	GenerationID string
	SourceSystem string
	ObservedAt   time.Time
	Document     searchdocs.Document
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
	if h.Documents == nil {
		writeSemanticSearchError(
			w,
			r,
			http.StatusServiceUnavailable,
			ErrorCodeBackendUnavailable,
			"semantic search requires the curated search-document read model",
		)
		return
	}

	rows, err := h.Documents.ListActiveDocuments(r.Context(), semanticSearchDocumentFilter{
		// The first public slice is repository-bounded; repository id is the
		// active ingestion scope used by the durable search-document store.
		ScopeID:     body.RepoID,
		RepoID:      body.RepoID,
		SourceKinds: sourceKinds,
		Limit:       semanticSearchCorpusLimit,
	})
	if err != nil {
		writeSemanticSearchError(w, r, http.StatusInternalServerError, ErrorCodeInternalError, "semantic search document read failed")
		return
	}
	index, err := searchhybrid.NewIndex(semanticSearchDocuments(rows, body.RepoID), searchhybrid.Options{
		MaxDocuments: semanticSearchCorpusLimit,
	})
	if err != nil {
		writeSemanticSearchError(w, r, http.StatusInternalServerError, ErrorCodeInternalError, "semantic search index build failed")
		return
	}
	retrieval, err := (searchretrieval.Runner{
		Backend: searchhybrid.Backend{Index: index},
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
		semanticSearchResponseFromRetrieval(req, retrieval, index, len(rows)),
		h.truth(),
	)
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

func semanticSearchDocuments(rows []semanticSearchDocumentRow, repoID string) []searchdocs.Document {
	docs := make([]searchdocs.Document, 0, len(rows))
	for _, row := range rows {
		if !semanticSearchDocumentMatchesRepository(row.Document, repoID) {
			continue
		}
		docs = append(docs, row.Document)
	}
	return docs
}

func semanticSearchDocumentMatchesRepository(doc searchdocs.Document, repoID string) bool {
	if doc.RepoID == repoID || doc.AccessScope.RepoID == repoID {
		return true
	}
	for _, handle := range doc.GraphHandles {
		if handle.Kind == "repository" && handle.ID == repoID {
			return true
		}
	}
	return false
}

func emptySemanticSearchResponse(req searchretrieval.Request) semanticSearchResponse {
	return semanticSearchResponse{
		Query:       req.Query,
		RepoID:      req.Scope.RepoID,
		Anchor:      req.Scope.Anchor(),
		Mode:        req.Mode,
		SearchMode:  string(req.Mode),
		Limit:       req.Limit,
		TimeoutMS:   int(req.Timeout / time.Millisecond),
		Results:     []semanticSearchResult{},
		CorpusLimit: semanticSearchCorpusLimit,
		Truncated:   false,
	}
}

func semanticSearchResponseFromRetrieval(
	req searchretrieval.Request,
	retrieval searchretrieval.Response,
	index *searchhybrid.Index,
	rowCount int,
) semanticSearchResponse {
	results := make([]semanticSearchResult, 0, len(retrieval.Results))
	for _, result := range retrieval.Results {
		results = append(results, semanticSearchResult{
			Rank:         result.Rank,
			Score:        result.Score,
			SearchMethod: semanticSearchMethod(result.Metadata, retrieval.Mode),
			Document:     semanticSearchDocumentFromSearchDoc(result.Document),
			GraphHandles: semanticSearchGraphHandles(result.Handles),
			TruthScope:   semanticSearchTruthScope(result.TruthScope),
			Freshness:    semanticSearchFreshness(result.Freshness),
			Failures:     append([]searchbench.FailureClass(nil), result.Failures...),
			Metadata:     cloneSemanticSearchMetadata(result.Metadata),
		})
	}
	return semanticSearchResponse{
		Query:                    retrieval.Query,
		RepoID:                   req.Scope.RepoID,
		Anchor:                   retrieval.Anchor,
		Mode:                     retrieval.Mode,
		SearchMode:               string(retrieval.Mode),
		Limit:                    retrieval.Limit,
		TimeoutMS:                int(retrieval.Timeout / time.Millisecond),
		Results:                  results,
		Truncated:                retrieval.Truncated,
		FalseCanonicalClaimCount: retrieval.FalseCanonicalClaimCount,
		IndexedDocumentCount:     index.Size(),
		CorpusLimit:              semanticSearchCorpusLimit,
		CorpusMayBeTruncated:     rowCount >= semanticSearchCorpusLimit || index.Overflow() > 0,
	}
}

func semanticSearchDocumentFromSearchDoc(doc searchdocs.Document) semanticSearchDocument {
	return semanticSearchDocument{
		ID:           doc.ID,
		RepoID:       doc.RepoID,
		SourceKind:   doc.SourceKind,
		Title:        doc.Title,
		Path:         doc.Path,
		ContextText:  doc.ContextText,
		EntityRefs:   semanticSearchEntityRefs(doc.EntityRefs),
		GraphHandles: semanticSearchGraphHandles(doc.GraphHandles),
		Labels:       append([]string(nil), doc.Labels...),
		UpdatedAt:    doc.UpdatedAt,
		TruthScope:   semanticSearchTruthScope(doc.TruthScope),
		Freshness:    semanticSearchFreshness(doc.Freshness),
		AccessScope:  semanticSearchAccessScope{RepoID: doc.AccessScope.RepoID},
		Provenance: semanticSearchProvenance{
			SourceTable: doc.Provenance.SourceTable,
			SourceIDs:   append([]string(nil), doc.Provenance.SourceIDs...),
		},
	}
}

func semanticSearchEntityRefs(refs []searchdocs.EntityRef) []semanticSearchEntityRef {
	out := make([]semanticSearchEntityRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, semanticSearchEntityRef{
			ID:        ref.ID,
			Type:      ref.Type,
			Name:      ref.Name,
			Path:      ref.Path,
			StartLine: ref.StartLine,
			EndLine:   ref.EndLine,
		})
	}
	return out
}

func semanticSearchGraphHandles(handles []searchdocs.GraphHandle) []semanticSearchGraphHandle {
	out := make([]semanticSearchGraphHandle, 0, len(handles))
	for _, handle := range handles {
		out = append(out, semanticSearchGraphHandle{Kind: handle.Kind, ID: handle.ID})
	}
	return out
}

func semanticSearchMethod(metadata map[string]string, mode searchbench.Mode) string {
	if method := strings.TrimSpace(metadata["search_method"]); method != "" {
		return method
	}
	return string(mode)
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

func cloneSemanticSearchMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func (h *SemanticSearchHandler) truth() *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		semanticSearchCapability,
		TruthBasisHybrid,
		"resolved from curated search documents with bounded in-process retrieval",
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
