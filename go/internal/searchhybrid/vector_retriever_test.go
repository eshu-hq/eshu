// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchhybrid

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

type fixedVectorEmbedder struct {
	dims int
}

func (e fixedVectorEmbedder) Dimensions() int { return e.dims }

func (e fixedVectorEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	switch {
	case strings.Contains(text, "tilted-query"):
		return []float64{0.51, 0.50}, nil
	case strings.Contains(text, "cross-best"):
		return []float64{0.50, 0.51}, nil
	case strings.Contains(text, "axis"):
		return []float64{1, 0}, nil
	case strings.Contains(text, "aligned"):
		return []float64{1, 0}, nil
	case strings.Contains(text, "opposite"):
		return []float64{-1, 0}, nil
	case strings.Contains(text, "near"):
		return []float64{0.9, 0.1}, nil
	case strings.Contains(text, "zero"):
		return []float64{0, 0}, nil
	case strings.Contains(text, "mismatch"):
		return []float64{1}, nil
	case strings.Contains(text, "nan"):
		return []float64{math.NaN(), 0}, nil
	case strings.Contains(text, "inf"):
		return []float64{math.Inf(1), 0}, nil
	default:
		return []float64{0, 1}, nil
	}
}

func TestVectorRetrievalApproximateAgreesWithExactForSmallCorpus(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		doc("b-aligned", "repo-1", "aligned", "aligned body"),
		doc("a-near", "repo-1", "near", "near body"),
		doc("c-other", "repo-1", "other", "other body"),
		doc("z-out", "repo-2", "aligned", "aligned body"),
	}
	exact := Backend{Index: mustIndex(t, docs, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalExact,
	})}
	approx := Backend{Index: mustIndex(t, docs, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalApproximate,
	})}
	req := request("aligned query", "repo-1", searchbench.ModeSemantic, 1)

	exactCandidates, err := exact.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("exact Search error = %v", err)
	}
	approxCandidates, err := approx.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("approx Search error = %v", err)
	}
	if got, want := strings.Join(candidateIDs(approxCandidates), ","), strings.Join(candidateIDs(exactCandidates), ","); got != want {
		t.Fatalf("approx ids = %s, want exact ids %s", got, want)
	}
}

func TestVectorRetrievalDefaultUsesExactBaseline(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		doc("cross-bucket-best", "repo-1", "cross-best", "cross-best body"),
		doc("same-bucket-weaker", "repo-1", "axis", "axis body"),
	}
	exact := Backend{Index: mustIndex(t, docs, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalExact,
	})}
	defaulted := Backend{Index: mustIndex(t, docs, Options{
		Embedder: fixedVectorEmbedder{dims: 2},
	})}
	req := request("tilted-query", "repo-1", searchbench.ModeSemantic, 10)

	exactCandidates, err := exact.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("exact Search error = %v", err)
	}
	defaultCandidates, err := defaulted.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("default Search error = %v", err)
	}
	if got, want := strings.Join(candidateIDs(defaultCandidates), ","), strings.Join(candidateIDs(exactCandidates), ","); got != want {
		t.Fatalf("default ids = %s, want exact ids %s", got, want)
	}
}

func TestVectorRetrievalAutoUsesApproximateAboveThreshold(t *testing.T) {
	t.Parallel()

	docs := make([]searchdocs.Document, 0, approximateVectorAutoMinDocuments+1)
	for i := 0; i < approximateVectorAutoMinDocuments+1; i++ {
		docs = append(docs, doc("doc-"+strings.Repeat("a", i+1), "repo-1", "aligned", "aligned body"))
	}

	index := mustIndex(t, docs, Options{
		Embedder: fixedVectorEmbedder{dims: 2},
	})

	if _, ok := index.vector.(approximateVectorRetriever); !ok {
		t.Fatalf("auto vector retriever = %T, want approximateVectorRetriever above threshold", index.vector)
	}
}

func TestVectorRetrievalApproximateFindsNearCrossDominantBucketVector(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		doc("cross-bucket-best", "repo-1", "cross-best", "cross-best body"),
		doc("same-bucket-weaker", "repo-1", "axis", "axis body"),
	}
	backend := Backend{Index: mustIndex(t, docs, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalApproximate,
	})}
	req := request("tilted-query", "repo-1", searchbench.ModeSemantic, 1)

	candidates, err := backend.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if got, want := strings.Join(candidateIDs(candidates), ","), "cross-bucket-best"; got != want {
		t.Fatalf("approx ids = %s, want %s", got, want)
	}
}

func TestVectorRetrievalSkipsMalformedDocumentVectors(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		doc("good", "repo-1", "aligned", "aligned body"),
		doc("zero", "repo-1", "zero", "zero body"),
		doc("mismatch", "repo-1", "mismatch", "mismatch body"),
		doc("nan", "repo-1", "nan", "nan body"),
		doc("inf", "repo-1", "inf", "inf body"),
	}
	backend := Backend{Index: mustIndex(t, docs, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalExact,
	})}

	candidates, err := backend.Search(context.Background(), request("aligned query", "repo-1", searchbench.ModeSemantic, 10))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if got := candidateIDs(candidates); strings.Join(got, ",") != "good" {
		t.Fatalf("candidate ids = %v, want only good", got)
	}
}

func TestVectorRetrievalApproximateFallbackStaysScoped(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		doc("in-scope", "repo-1", "opposite", "opposite body"),
		doc("out-of-scope", "repo-2", "aligned", "aligned body"),
	}
	backend := Backend{Index: mustIndex(t, docs, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalApproximate,
	})}

	candidates, err := backend.Search(context.Background(), request("aligned query", "repo-1", searchbench.ModeSemantic, 10))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if got := candidateIDs(candidates); strings.Join(got, ",") != "in-scope" {
		t.Fatalf("candidate ids = %v, want scoped fallback result", got)
	}
}

func TestVectorRetrievalTieOrderingByDocumentID(t *testing.T) {
	t.Parallel()

	docs := []searchdocs.Document{
		doc("b", "repo-1", "aligned", "aligned body"),
		doc("a", "repo-1", "aligned", "aligned body"),
	}
	backend := Backend{Index: mustIndex(t, docs, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalExact,
	})}

	candidates, err := backend.Search(context.Background(), request("aligned query", "repo-1", searchbench.ModeSemantic, 10))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if got := strings.Join(candidateIDs(candidates), ","); got != "a,b" {
		t.Fatalf("candidate ids = %s, want a,b", got)
	}
}

func TestVectorRetrievalEmptyCorpus(t *testing.T) {
	t.Parallel()

	backend := Backend{Index: mustIndex(t, nil, Options{
		Embedder:        fixedVectorEmbedder{dims: 2},
		VectorRetrieval: VectorRetrievalApproximate,
	})}

	candidates, err := backend.Search(context.Background(), request("aligned query", "repo-1", searchbench.ModeSemantic, 10))
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(candidates))
	}
}
