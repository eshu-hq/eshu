// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchnornicdb

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
	nornicsearch "github.com/orneryd/nornicdb/pkg/nornicgrpc/gen"
	"google.golang.org/protobuf/types/known/structpb"
)

func BenchmarkBackendSearch(b *testing.B) {
	for _, hitCount := range []int{1, 10, 100} {
		b.Run(strconv.Itoa(hitCount), func(b *testing.B) {
			client := &fakeSearchClient{
				response: makeSearchResponse(hitCount),
			}
			backend := Backend{Client: client, Database: "eshu-test"}
			req := searchretrieval.Request{
				QueryID: "q-1",
				Query:   "payment refund token",
				Scope:   searchretrieval.Scope{ServiceID: "service:checkout-api"},
				Mode:    searchbench.ModeHybrid,
				Limit:   20,
				Timeout: time.Second,
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = backend.Search(context.Background(), req)
			}
		})
	}
}

func BenchmarkBackendSearchNoHits(b *testing.B) {
	client := &fakeSearchClient{
		response: &nornicsearch.SearchTextResponse{
			SearchMethod: "rrf_hybrid",
		},
	}
	backend := Backend{Client: client, Database: "eshu-test"}
	req := searchretrieval.Request{
		QueryID: "q-1",
		Query:   "payment refund token",
		Scope:   searchretrieval.Scope{ServiceID: "service:checkout-api"},
		Mode:    searchbench.ModeHybrid,
		Limit:   20,
		Timeout: time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = backend.Search(context.Background(), req)
	}
}

func BenchmarkBackendCandidateMapping(b *testing.B) {
	// Isolate the candidateFromHit transformation by benchmarking
	// just the conversion loop, skipping the gRPC round-trip.
	backend := Backend{Client: &fakeSearchClient{}, Database: "eshu-test"}
	response := makeSearchResponse(100)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidates := make([]searchretrieval.Candidate, 0, len(response.GetHits()))
		for _, hit := range response.GetHits() {
			c, err := backend.candidateFromHit(response, hit, searchretrieval.Scope{ServiceID: "service:checkout-api"})
			if err != nil {
				b.Fatalf("candidateFromHit: %v", err)
			}
			candidates = append(candidates, c)
		}
		_ = candidates
	}
}

func makeSearchResponse(hitCount int) *nornicsearch.SearchTextResponse {
	hits := make([]*nornicsearch.SearchHit, hitCount)
	for i := range hits {
		properties, _ := structpb.NewStruct(map[string]interface{}{
			"document_id":     "searchdoc:semantic_context:d-" + strconv.Itoa(i),
			"repo_id":         "repo-checkout",
			"source_kind":     "semantic_context",
			"title":           "Checkout context " + strconv.Itoa(i),
			"context_text":    "checkout-api alerts route through pagerduty primary during deploys",
			"truth_level":     "derived",
			"truth_basis":     "read_model",
			"freshness_state": "fresh",
			"graph_handles": []interface{}{
				"semantic_context:semantic-context:d-" + strconv.Itoa(i),
				"service:checkout-api",
				"workload:workload:checkout-api",
			},
			"service_id": "service:checkout-api",
		})
		hits[i] = &nornicsearch.SearchHit{
			NodeId:     "semantic-context:d-" + strconv.Itoa(i),
			Labels:     []string{SemanticContextLabel},
			Properties: properties,
			Score:      0.9 - float32(i)*0.01,
			RrfScore:   0.9 - float32(i)*0.01,
			VectorRank: int32(i + 1),
			Bm25Rank:   int32(i + 2),
		}
	}
	return &nornicsearch.SearchTextResponse{
		SearchMethod: "rrf_hybrid",
		Hits:         hits,
	}
}
