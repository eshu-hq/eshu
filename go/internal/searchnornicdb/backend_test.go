// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchnornicdb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	nornicsearch "github.com/orneryd/nornicdb/pkg/nornicgrpc/gen"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestBackendSearchSendsBoundedSemanticContextRequest(t *testing.T) {
	client := &fakeSearchClient{
		response: &nornicsearch.SearchTextResponse{
			SearchMethod:      "rrf_hybrid",
			FallbackTriggered: false,
			Hits:              []*nornicsearch.SearchHit{semanticContextHit(t, "searchdoc:semantic_context:checkout", "service:checkout-api", 0.84)},
		},
	}
	backend := Backend{Client: client, Database: "eshu-test"}

	candidates, err := backend.Search(context.Background(), searchretrieval.Request{
		QueryID: "semantic-context-001",
		Query:   "which policy owns checkout deploy alerts",
		Scope: searchretrieval.Scope{
			ServiceID: "service:checkout-api",
		},
		Mode:    searchbench.ModeHybrid,
		Limit:   1,
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Backend.Search returned error: %v", err)
	}
	if client.request == nil {
		t.Fatal("SearchText was not called")
	}
	if got, want := client.request.Database, "eshu-test"; got != want {
		t.Fatalf("request.Database = %q, want %q", got, want)
	}
	if got, want := client.request.Query, "which policy owns checkout deploy alerts"; got != want {
		t.Fatalf("request.Query = %q, want %q", got, want)
	}
	if got, want := client.request.Limit, uint32(2); got != want {
		t.Fatalf("request.Limit = %d, want %d", got, want)
	}
	if got, want := client.request.Labels, []string{SemanticContextLabel}; !reflect.DeepEqual(got, want) {
		t.Fatalf("request.Labels = %#v, want %#v", got, want)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	candidate := candidates[0]
	if got, want := candidate.Document.ID, "searchdoc:semantic_context:checkout"; got != want {
		t.Fatalf("candidate.Document.ID = %q, want %q", got, want)
	}
	if got, want := candidate.Metadata["search_method"], "rrf_hybrid"; got != want {
		t.Fatalf("candidate.Metadata[search_method] = %q, want %q", got, want)
	}
	if got, want := candidate.Metadata["fallback_triggered"], "false"; got != want {
		t.Fatalf("candidate.Metadata[fallback_triggered] = %q, want %q", got, want)
	}
}

func TestBackendSearchRejectsFallbackMode(t *testing.T) {
	client := &fakeSearchClient{
		response: &nornicsearch.SearchTextResponse{
			SearchMethod:      "fulltext",
			FallbackTriggered: true,
			Hits: []*nornicsearch.SearchHit{
				semanticContextHit(t, "searchdoc:semantic_context:checkout", "service:checkout-api", 0.71),
			},
		},
	}
	backend := Backend{Client: client}

	_, err := backend.Search(context.Background(), boundedHybridRequest())
	if !errorContains(err, "hybrid") || !errorContains(err, "fallback") {
		t.Fatalf("Backend.Search error = %v, want hybrid fallback rejection", err)
	}
}

func TestBackendSearchRejectsUnscopedLabelEscape(t *testing.T) {
	hit := semanticContextHit(t, "searchdoc:semantic_context:checkout", "service:checkout-api", 0.71)
	hit.Labels = []string{"Repository"}
	client := &fakeSearchClient{
		response: &nornicsearch.SearchTextResponse{
			SearchMethod: "rrf_hybrid",
			Hits:         []*nornicsearch.SearchHit{hit},
		},
	}
	backend := Backend{Client: client}

	_, err := backend.Search(context.Background(), boundedHybridRequest())
	if !errorContains(err, SemanticContextLabel) {
		t.Fatalf("Backend.Search error = %v, want SemanticContext label rejection", err)
	}
}

func TestBackendSearchRejectsCandidateOutsideRequestScope(t *testing.T) {
	client := &fakeSearchClient{
		response: &nornicsearch.SearchTextResponse{
			SearchMethod: "rrf_hybrid",
			Hits: []*nornicsearch.SearchHit{
				semanticContextHit(t, "searchdoc:semantic_context:other", "service:billing-api", 0.71),
			},
		},
	}
	backend := Backend{Client: client}

	_, err := backend.Search(context.Background(), boundedHybridRequest())
	if !errorContains(err, "scope") {
		t.Fatalf("Backend.Search error = %v, want scope rejection", err)
	}
}

func TestBackendSearchRequiresHybridMode(t *testing.T) {
	backend := Backend{Client: &fakeSearchClient{}}
	req := boundedHybridRequest()
	req.Mode = searchbench.ModeKeyword

	_, err := backend.Search(context.Background(), req)
	if !errorContains(err, "hybrid") {
		t.Fatalf("Backend.Search error = %v, want hybrid-mode rejection", err)
	}
}

func TestBackendSearchPropagatesClientError(t *testing.T) {
	backend := Backend{Client: &fakeSearchClient{err: errors.New("nornicdb unavailable")}}

	_, err := backend.Search(context.Background(), boundedHybridRequest())
	if !errorContains(err, "nornicdb unavailable") {
		t.Fatalf("Backend.Search error = %v, want client error", err)
	}
}

type fakeSearchClient struct {
	request  *nornicsearch.SearchTextRequest
	response *nornicsearch.SearchTextResponse
	err      error
}

func (client *fakeSearchClient) SearchText(
	_ context.Context,
	request *nornicsearch.SearchTextRequest,
	_ ...grpc.CallOption,
) (*nornicsearch.SearchTextResponse, error) {
	client.request = request
	if client.err != nil {
		return nil, client.err
	}
	return client.response, nil
}

func boundedHybridRequest() searchretrieval.Request {
	return searchretrieval.Request{
		QueryID: "semantic-context-001",
		Query:   "which policy owns checkout deploy alerts",
		Scope: searchretrieval.Scope{
			ServiceID: "service:checkout-api",
		},
		Mode:    searchbench.ModeHybrid,
		Limit:   5,
		Timeout: 50 * time.Millisecond,
	}
}

func semanticContextHit(
	t *testing.T,
	documentID string,
	serviceID string,
	score float32,
) *nornicsearch.SearchHit {
	t.Helper()
	properties, err := structpb.NewStruct(map[string]interface{}{
		"document_id":     documentID,
		"repo_id":         "repo-checkout",
		"source_kind":     "semantic_context",
		"title":           "Checkout alert routing context",
		"context_text":    "checkout-api alerts route through pagerduty primary during deploys",
		"truth_level":     "derived",
		"truth_basis":     "read_model",
		"freshness_state": "fresh",
		"graph_handles": []interface{}{
			"semantic_context:semantic-context:checkout",
			serviceID,
			"workload:workload:checkout-api",
		},
		"service_id": serviceID,
	})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return &nornicsearch.SearchHit{
		NodeId:     "semantic-context:checkout",
		Labels:     []string{SemanticContextLabel},
		Properties: properties,
		Score:      score,
		RrfScore:   score,
		VectorRank: 1,
		Bm25Rank:   2,
	}
}

func errorContains(err error, needle string) bool {
	return err != nil && strings.Contains(err.Error(), needle)
}
