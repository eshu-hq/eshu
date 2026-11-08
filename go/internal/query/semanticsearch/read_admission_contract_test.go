// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// frozenSemanticSearchReadAdmission pins the exact method set behind
// the versioned #9669 O6 admission contract for POST
// /api/v0/search/semantic: the persisted-index search (IndexSearch),
// the local hybrid search (HybridSearch), the scope resolution pair
// (ResolveScope, ResolveRepositoryForScope), and the search-vector
// watermark probe (VectorReadyWatermark). The entries re-declare the
// live-surface signatures in this SemanticSearch-scoped shape instead
// of embedding the live surfaces, so no other family can cite this
// pin. A renamed, added, removed, or re-signed method fails here until
// that list is deliberately updated; bump
// SemanticSearchReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape, keeping the eshu-api extraction
// boundary from drifting silently.
//
// D-wave consumer: O6 pin 4 (per the arbiter O6 ruling minting order).
var frozenSemanticSearchReadAdmission = []string{
	"HybridSearch func(context.Context, semanticsearch.SemanticSearchIndexQuery) (semanticsearch.SemanticSearchIndexResult, error)",
	"IndexSearch func(context.Context, semanticsearch.SemanticSearchIndexQuery) (semanticsearch.SemanticSearchIndexResult, error)",
	"ResolveRepositoryForScope func(context.Context, string) (string, error)",
	"ResolveScope func(context.Context, string) (string, error)",
	"VectorReadyWatermark func(context.Context) (semanticsearch.SearchVectorReadyFreshness, error)",
}

// semanticSearchReadAdmissionWitness proves
// SemanticSearchReadAdmission is satisfiable: the core-owned index,
// hybrid, scope, and watermark readers satisfy it jointly, composed
// by the consumer — no single store carries all five methods. It
// reuses the compile-time mirror with seeded fakes rather than
// inventing a new satisfier.
type semanticSearchReadAdmissionWitness struct {
	semanticSearchReadAdmissionMirror
}

var _ SemanticSearchReadAdmission = semanticSearchReadAdmissionWitness{}

// semanticSearchReadAdmissionMethodSet reflects the live
// SemanticSearchReadAdmission method set into sorted "name signature"
// strings for comparison with the frozen list.
func semanticSearchReadAdmissionMethodSet(t *testing.T) []string {
	t.Helper()
	rt := reflect.TypeOf((*SemanticSearchReadAdmission)(nil)).Elem()
	var got []string
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		got = append(got, m.Name+" "+m.Type.String())
	}
	sort.Strings(got)
	return got
}

// TestSemanticSearchReadAdmissionContractIsVersioned pins the #9669 O6
// admission-contract version for the semantic-search read. The frozen
// method set below is the enforcement: an added, removed, renamed, or
// re-signed method fails this test until the frozen list is
// deliberately updated. Bump
// SemanticSearchReadAdmissionContractVersion alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
func TestSemanticSearchReadAdmissionContractIsVersioned(t *testing.T) {
	t.Parallel()

	if SemanticSearchReadAdmissionContractVersion != "v0" {
		t.Errorf("SemanticSearchReadAdmissionContractVersion = %q, want %q: bump it deliberately with the shape change", SemanticSearchReadAdmissionContractVersion, "v0")
	}
	got := semanticSearchReadAdmissionMethodSet(t)
	if len(got) != len(frozenSemanticSearchReadAdmission) {
		t.Fatalf("SemanticSearchReadAdmission method set = %q (len %d), want frozen %q (len %d): bump %s with the shape change", got, len(got), frozenSemanticSearchReadAdmission, len(frozenSemanticSearchReadAdmission), SemanticSearchReadAdmissionContractVersion)
	}
	for i := range got {
		if got[i] != frozenSemanticSearchReadAdmission[i] {
			t.Errorf("SemanticSearchReadAdmission method set differs at index %d: got %q, want %q: bump %s with the shape change", i, got[i], frozenSemanticSearchReadAdmission[i], SemanticSearchReadAdmissionContractVersion)
		}
	}
}

// seedSemanticSearchReadAdmissionWitness composes the package's
// existing fakes into a witness over one bounded seeded corpus: a
// single curated document, a fixed scope binding, and a published
// search-vector watermark.
func seedSemanticSearchReadAdmissionWitness() semanticSearchReadAdmissionWitness {
	return semanticSearchReadAdmissionWitness{
		semanticSearchReadAdmissionMirror: semanticSearchReadAdmissionMirror{
			index: &fakeSemanticSearchIndexStore{
				result: SemanticSearchIndexResult{
					IndexedDocumentCount: 1,
					CorpusLimit:          500,
					RetrievalState:       "keyword_only",
				},
			},
			hybrid: NewLocalSemanticSearchHybrid(&fakeSemanticSearchDocumentStore{
				rows: []semanticSearchDocumentRow{
					{Document: testutil.SemanticSearchDocumentFixture("searchdoc:pin", "repo-pin", "Pin", "pin seeded proof")},
				},
			}),
			scope: &fakeSemanticSearchScopeResolver{
				scopeID:      "git-repository-scope:repo-pin",
				directRepoID: "repo-pin",
			},
			watermark: &fakeSearchVectorReadyReader{
				freshness: SearchVectorReadyFreshness{
					Signaled:       true,
					Present:        true,
					MaterializedAt: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC),
				},
			},
		},
	}
}

// TestSemanticSearchReadAdmissionServesSeededReads is the GREEN proof:
// every pin method serves its seeded read through the witness.
func TestSemanticSearchReadAdmissionServesSeededReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin SemanticSearchReadAdmission = seedSemanticSearchReadAdmissionWitness()
	query := SemanticSearchIndexQuery{
		ScopeID: "git-repository-scope:repo-pin",
		RepoID:  "repo-pin",
		Request: semanticSearchRequestFixture("pin", 10),
	}

	indexResult, err := pin.IndexSearch(ctx, query)
	if err != nil {
		t.Fatalf("IndexSearch() error = %v", err)
	}
	if indexResult.IndexedDocumentCount != 1 {
		t.Errorf("IndexSearch() IndexedDocumentCount = %d, want 1", indexResult.IndexedDocumentCount)
	}

	hybridResult, err := pin.HybridSearch(ctx, query)
	if err != nil {
		t.Fatalf("HybridSearch() error = %v", err)
	}
	if hybridResult.IndexedDocumentCount == 0 {
		t.Error("HybridSearch() IndexedDocumentCount = 0, want at least 1 from the seeded document")
	}

	scopeID, err := pin.ResolveScope(ctx, "repo-pin")
	if err != nil {
		t.Fatalf("ResolveScope() error = %v", err)
	}
	if scopeID != "git-repository-scope:repo-pin" {
		t.Errorf("ResolveScope() = %q, want %q", scopeID, "git-repository-scope:repo-pin")
	}

	repoID, err := pin.ResolveRepositoryForScope(ctx, "git-repository-scope:repo-pin")
	if err != nil {
		t.Fatalf("ResolveRepositoryForScope() error = %v", err)
	}
	if repoID != "repo-pin" {
		t.Errorf("ResolveRepositoryForScope() = %q, want %q", repoID, "repo-pin")
	}

	watermark, err := pin.VectorReadyWatermark(ctx)
	if err != nil {
		t.Fatalf("VectorReadyWatermark() error = %v", err)
	}
	if !watermark.Signaled || !watermark.Present {
		t.Errorf("VectorReadyWatermark() = %+v, want signaled and present", watermark)
	}
}

// TestSemanticSearchReadAdmissionSurfacesSeededFailures is the RED
// proof: a seeded backend failure fails closed through the pin — no
// method swallows it into a silently served read.
func TestSemanticSearchReadAdmissionSurfacesSeededFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	boom := errors.New("seeded backend failure")
	var pin SemanticSearchReadAdmission = semanticSearchReadAdmissionWitness{
		semanticSearchReadAdmissionMirror: semanticSearchReadAdmissionMirror{
			index:     &fakeSemanticSearchIndexStore{err: boom},
			hybrid:    NewLocalSemanticSearchHybrid(&fakeSemanticSearchDocumentStore{err: boom}),
			scope:     &fakeSemanticSearchScopeResolver{err: boom},
			watermark: &fakeSearchVectorReadyReader{err: boom},
		},
	}
	query := SemanticSearchIndexQuery{
		ScopeID: "git-repository-scope:repo-pin",
		RepoID:  "repo-pin",
		Request: semanticSearchRequestFixture("pin", 10),
	}

	if _, err := pin.IndexSearch(ctx, query); !errors.Is(err, boom) {
		t.Errorf("IndexSearch() error = %v, want %v", err, boom)
	}
	if _, err := pin.HybridSearch(ctx, query); !errors.Is(err, boom) {
		t.Errorf("HybridSearch() error = %v, want %v", err, boom)
	}
	if _, err := pin.ResolveScope(ctx, "repo-pin"); !errors.Is(err, boom) {
		t.Errorf("ResolveScope() error = %v, want %v", err, boom)
	}
	if _, err := pin.ResolveRepositoryForScope(ctx, "git-repository-scope:repo-pin"); !errors.Is(err, boom) {
		t.Errorf("ResolveRepositoryForScope() error = %v, want %v", err, boom)
	}
	if _, err := pin.VectorReadyWatermark(ctx); !errors.Is(err, boom) {
		t.Errorf("VectorReadyWatermark() error = %v, want %v", err, boom)
	}
}
