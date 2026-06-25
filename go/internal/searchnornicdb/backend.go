// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchnornicdb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	nornicsearch "github.com/orneryd/nornicdb/pkg/nornicgrpc/gen"
	"google.golang.org/grpc"
)

const maxBackendLimit = 100

// SemanticContextLabel is the only NornicDB label admitted by this prototype.
const SemanticContextLabel = "SemanticContext"

// SearchTextClient is the NornicDB gRPC search method used by Backend.
type SearchTextClient interface {
	SearchText(
		context.Context,
		*nornicsearch.SearchTextRequest,
		...grpc.CallOption,
	) (*nornicsearch.SearchTextResponse, error)
}

// Backend converts NornicDB semantic context hits into bounded retrieval candidates.
type Backend struct {
	Client   SearchTextClient
	Database string
}

// Search implements searchretrieval.Backend for NornicDB hybrid retrieval.
func (backend Backend) Search(
	ctx context.Context,
	req searchretrieval.Request,
) ([]searchretrieval.Candidate, error) {
	if err := searchretrieval.ValidateRequest(req); err != nil {
		return nil, err
	}
	if req.Mode != searchbench.ModeHybrid {
		return nil, fmt.Errorf("nornicdb retrieval requires hybrid mode: mode=%q", req.Mode)
	}
	if backend.Client == nil {
		return nil, errors.New("nornicdb search client is required")
	}

	response, err := backend.Client.SearchText(ctx, &nornicsearch.SearchTextRequest{
		Database: strings.TrimSpace(backend.Database),
		Query:    strings.TrimSpace(req.Query),
		Limit:    uint32(backendLimit(req.Limit)), // #nosec G115 -- bounded: backendLimit caps the value to a small positive int before conversion
		Labels:   []string{SemanticContextLabel},
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("nornicdb search returned nil response")
	}
	if !isHybridSearchMethod(response.GetSearchMethod()) || response.GetFallbackTriggered() {
		return nil, fmt.Errorf(
			"nornicdb hybrid search fell back or returned non-hybrid mode: search_method=%q fallback_triggered=%t",
			response.GetSearchMethod(),
			response.GetFallbackTriggered(),
		)
	}

	candidates := make([]searchretrieval.Candidate, 0, len(response.GetHits()))
	for _, hit := range response.GetHits() {
		candidate, err := backend.candidateFromHit(response, hit, req.Scope)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func backendLimit(limit int) int {
	if limit < maxBackendLimit {
		return limit + 1
	}
	return maxBackendLimit
}

func isHybridSearchMethod(method string) bool {
	method = strings.TrimSpace(method)
	return method == "rrf_hybrid" ||
		method == "chunked_rrf_hybrid" ||
		strings.HasPrefix(method, "rrf_hybrid_")
}

func (backend Backend) candidateFromHit(
	response *nornicsearch.SearchTextResponse,
	hit *nornicsearch.SearchHit,
	scope searchretrieval.Scope,
) (searchretrieval.Candidate, error) {
	if hit == nil {
		return searchretrieval.Candidate{}, errors.New("nornicdb search returned nil hit")
	}
	if !hasLabel(hit.GetLabels(), SemanticContextLabel) {
		return searchretrieval.Candidate{}, fmt.Errorf(
			"nornicdb hit escaped required %s label: node_id=%q labels=%v",
			SemanticContextLabel,
			hit.GetNodeId(),
			hit.GetLabels(),
		)
	}
	props := map[string]interface{}{}
	if hit.GetProperties() != nil {
		props = hit.GetProperties().AsMap()
	}
	doc, err := documentFromProperties(hit, props)
	if err != nil {
		return searchretrieval.Candidate{}, err
	}
	if !matchesScope(doc, props, scope.Anchor()) {
		return searchretrieval.Candidate{}, fmt.Errorf(
			"nornicdb hit is outside request scope: document_id=%q anchor=%s:%s",
			doc.ID,
			scope.Anchor().Kind,
			scope.Anchor().ID,
		)
	}
	score := float64(hit.GetRrfScore())
	if score == 0 {
		score = float64(hit.GetScore())
	}
	return searchretrieval.Candidate{
		Document: doc,
		Score:    score,
		Metadata: map[string]string{
			"search_method":      response.GetSearchMethod(),
			"fallback_triggered": strconv.FormatBool(response.GetFallbackTriggered()),
			"node_id":            hit.GetNodeId(),
			"vector_rank":        strconv.Itoa(int(hit.GetVectorRank())),
			"bm25_rank":          strconv.Itoa(int(hit.GetBm25Rank())),
		},
	}, nil
}

func documentFromProperties(
	hit *nornicsearch.SearchHit,
	props map[string]interface{},
) (searchdocs.Document, error) {
	docID := stringProp(props, "document_id")
	if docID == "" {
		return searchdocs.Document{}, fmt.Errorf("nornicdb hit missing document_id: node_id=%q", hit.GetNodeId())
	}
	if sourceKind := searchdocs.SourceKind(stringProp(props, "source_kind")); sourceKind != searchdocs.SourceKindSemanticContext {
		return searchdocs.Document{}, fmt.Errorf(
			"nornicdb hit source_kind must be semantic_context: document_id=%q source_kind=%q",
			docID,
			sourceKind,
		)
	}
	truthLevel := searchdocs.TruthLevel(stringProp(props, "truth_level"))
	if truthLevel != searchdocs.TruthLevelDerived {
		return searchdocs.Document{}, fmt.Errorf(
			"nornicdb hit truth_level must be derived: document_id=%q truth_level=%q",
			docID,
			truthLevel,
		)
	}
	truthBasis := searchdocs.TruthBasis(stringProp(props, "truth_basis"))
	if truthBasis != searchdocs.TruthBasisReadModel {
		return searchdocs.Document{}, fmt.Errorf(
			"nornicdb hit truth_basis must be read_model: document_id=%q truth_basis=%q",
			docID,
			truthBasis,
		)
	}
	freshness := searchdocs.FreshnessState(stringProp(props, "freshness_state"))
	if freshness != searchdocs.FreshnessFresh {
		return searchdocs.Document{}, fmt.Errorf(
			"nornicdb hit freshness_state must be fresh: document_id=%q freshness_state=%q",
			docID,
			freshness,
		)
	}
	graphHandles := graphHandlesFromValue(props["graph_handles"])
	if len(graphHandles) == 0 {
		return searchdocs.Document{}, fmt.Errorf("nornicdb hit missing graph_handles: document_id=%q", docID)
	}
	return searchdocs.Document{
		ID:           docID,
		RepoID:       stringProp(props, "repo_id"),
		SourceKind:   searchdocs.SourceKindSemanticContext,
		Title:        stringProp(props, "title"),
		Path:         stringProp(props, "path"),
		ContextText:  stringProp(props, "context_text"),
		GraphHandles: graphHandles,
		Labels:       stringSliceProp(props["labels"]),
		TruthScope: searchdocs.TruthScope{
			Level: truthLevel,
			Basis: truthBasis,
		},
		Freshness: searchdocs.Freshness{
			State: freshness,
		},
		AccessScope: searchdocs.AccessScope{
			RepoID: stringProp(props, "repo_id"),
		},
	}, nil
}

func matchesScope(
	doc searchdocs.Document,
	props map[string]interface{},
	anchor searchretrieval.Anchor,
) bool {
	switch anchor.Kind {
	case searchretrieval.ScopeKindService:
		return stringProp(props, "service_id") == anchor.ID || hasHandle(doc, "service", anchor.ID)
	case searchretrieval.ScopeKindWorkload:
		return stringProp(props, "workload_id") == anchor.ID || hasHandle(doc, "workload", anchor.ID)
	case searchretrieval.ScopeKindRepo:
		return doc.RepoID == anchor.ID ||
			stringProp(props, "repo_id") == anchor.ID ||
			hasHandle(doc, "repository", anchor.ID)
	case searchretrieval.ScopeKindEnvironment:
		return stringProp(props, "environment") == anchor.ID || hasHandle(doc, "environment", anchor.ID)
	default:
		return false
	}
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}

func hasHandle(doc searchdocs.Document, kind string, id string) bool {
	for _, handle := range doc.GraphHandles {
		if handle.Kind == kind && handle.ID == id {
			return true
		}
	}
	return false
}

func graphHandlesFromValue(value interface{}) []searchdocs.GraphHandle {
	values, ok := value.([]interface{})
	if !ok {
		return nil
	}
	handles := make([]searchdocs.GraphHandle, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if handle, ok := graphHandleFromString(typed); ok {
				handles = append(handles, handle)
			}
		case map[string]interface{}:
			handle := searchdocs.GraphHandle{
				Kind: strings.TrimSpace(stringProp(typed, "kind")),
				ID:   strings.TrimSpace(stringProp(typed, "id")),
			}
			if handle.Kind != "" && handle.ID != "" {
				handles = append(handles, handle)
			}
		}
	}
	return handles
}

func graphHandleFromString(value string) (searchdocs.GraphHandle, bool) {
	kind, id, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(id) == "" {
		return searchdocs.GraphHandle{}, false
	}
	return searchdocs.GraphHandle{Kind: strings.TrimSpace(kind), ID: strings.TrimSpace(id)}, true
}

func stringProp(props map[string]interface{}, key string) string {
	value, ok := props[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func stringSliceProp(value interface{}) []string {
	values, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			out = append(out, text)
		}
	}
	return out
}
