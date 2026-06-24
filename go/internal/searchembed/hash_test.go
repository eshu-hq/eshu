// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchembed

import (
	"context"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

func TestHashEmbedderDeterministicAndNormalized(t *testing.T) {
	t.Parallel()

	embedder, err := NewHashEmbedder(32)
	if err != nil {
		t.Fatalf("NewHashEmbedder error = %v", err)
	}
	first, err := embedder.Embed(context.Background(), "Payment, REFUND!")
	if err != nil {
		t.Fatalf("Embed first error = %v", err)
	}
	second, err := embedder.Embed(context.Background(), "payment refund")
	if err != nil {
		t.Fatalf("Embed second error = %v", err)
	}
	if len(first) != 32 {
		t.Fatalf("embedding dimensions = %d, want 32", len(first))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("embedding not normalized/deterministic:\nfirst=%v\nsecond=%v", first, second)
	}
	for i, value := range first {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("embedding[%d] = %v, want finite", i, value)
		}
	}
}

func TestHashEmbedderEmptyInput(t *testing.T) {
	t.Parallel()

	embedder, err := NewHashEmbedder(DefaultDimensions)
	if err != nil {
		t.Fatalf("NewHashEmbedder error = %v", err)
	}
	vector, err := embedder.Embed(context.Background(), " , \n\t ")
	if err != nil {
		t.Fatalf("Embed error = %v", err)
	}
	if len(vector) != DefaultDimensions {
		t.Fatalf("embedding dimensions = %d, want %d", len(vector), DefaultDimensions)
	}
	for i, value := range vector {
		if value != 0 {
			t.Fatalf("embedding[%d] = %v, want zero for empty input", i, value)
		}
	}
}

func TestHashEmbedderRejectsInvalidDimensions(t *testing.T) {
	t.Parallel()

	if _, err := NewHashEmbedder(0); err == nil {
		t.Fatal("NewHashEmbedder accepted zero dimensions")
	}
	if _, err := NewHashEmbedder(-1); err == nil {
		t.Fatal("NewHashEmbedder accepted negative dimensions")
	}
}

func TestHashEmbedderBoundsUniqueTerms(t *testing.T) {
	t.Parallel()

	embedder, err := NewHashEmbedder(16)
	if err != nil {
		t.Fatalf("NewHashEmbedder error = %v", err)
	}
	var builder strings.Builder
	for i := 0; i < MaxUniqueTerms+100; i++ {
		if i > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString("term")
		builder.WriteString(strconv.Itoa(i))
	}
	vector, err := embedder.Embed(context.Background(), builder.String())
	if err != nil {
		t.Fatalf("Embed error = %v", err)
	}
	if len(vector) != 16 {
		t.Fatalf("embedding dimensions = %d, want 16", len(vector))
	}
}

func TestHashEmbedderSupportsSemanticRanking(t *testing.T) {
	t.Parallel()

	embedder, err := NewHashEmbedder(64)
	if err != nil {
		t.Fatalf("NewHashEmbedder error = %v", err)
	}
	index, err := searchhybrid.NewIndex(searchCorpus(), searchhybrid.Options{Embedder: embedder})
	if err != nil {
		t.Fatalf("NewIndex error = %v", err)
	}
	backend := searchhybrid.Backend{Index: index}
	candidates, err := backend.Search(context.Background(), searchretrieval.Request{
		Query:   "process payment refund charge",
		Scope:   searchretrieval.Scope{RepoID: "repo-1"},
		Mode:    searchbench.ModeSemantic,
		Limit:   5,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected semantic candidates")
	}
	if got, want := candidates[0].Document.ID, "payment"; got != want {
		t.Fatalf("top semantic document = %q, want %q", got, want)
	}
	if got, want := candidates[0].Metadata["search_method"], "vector"; got != want {
		t.Fatalf("search_method = %q, want %q", got, want)
	}
}

func searchDoc(id, repo, title, body string) searchdocs.Document {
	return searchdocs.Document{
		ID:           id,
		RepoID:       repo,
		SourceKind:   searchdocs.SourceKindCodeEntity,
		Title:        title,
		ContextText:  body,
		GraphHandles: []searchdocs.GraphHandle{{Kind: "repository", ID: repo}, {Kind: "content_entity", ID: id}},
		TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		UpdatedAt:    time.Unix(0, 0),
	}
}

func searchCorpus() []searchdocs.Document {
	return []searchdocs.Document{
		searchDoc("payment", "repo-1", "Payment processor", "charge card and process payment refund"),
		searchDoc("auth", "repo-1", "Auth handler", "validate session token and login user"),
		searchDoc("invoice", "repo-1", "Invoice builder", "build invoice line items and totals"),
		searchDoc("other-repo-payment", "repo-2", "Payment gateway", "process payment with external gateway"),
	}
}
