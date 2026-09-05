// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// benchFactloadCorpusEnvelopes returns a synthetic generation covering every
// fact kind the five hoisted wrappers request, so the wall-clock cost of the
// wrapper + forwarder path can be measured without a live store (issue
// #6359). The stubFactLoader test double serves as the store fixture: it
// implements only the base FactLoader port, exercising the same fallback
// branch a non-push-down store takes in production.
func benchFactloadCorpusEnvelopes(n int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, 6*n+1)
	envelopes = append(envelopes, facts.Envelope{
		FactKind: factKindRepository,
		FactID:   "fact-repo",
		Payload:  map[string]any{"repo_id": "bench-repo"},
	})
	for i := 0; i < n; i++ {
		envelopes = append(envelopes,
			facts.Envelope{
				FactKind: factKindCodeownersOwnership,
				FactID:   fmt.Sprintf("fact-codeowners-%d", i),
				Payload:  map[string]any{"repo_id": "bench-repo"},
			},
			facts.Envelope{
				FactKind: facts.DocumentationDocumentFactKind,
				FactID:   fmt.Sprintf("fact-doc-%d", i),
				Payload: map[string]any{
					"document_id": fmt.Sprintf("doc:git:bench-repo:docs/doc-%d.md", i),
				},
			},
			facts.Envelope{
				FactKind: facts.DocumentationEntityMentionFactKind,
				FactID:   fmt.Sprintf("fact-mention-%d", i),
				Payload: map[string]any{
					"document_id": fmt.Sprintf("doc:git:bench-repo:docs/doc-%d.md", i),
				},
			},
			facts.Envelope{
				FactKind: factKindContentEntity,
				FactID:   fmt.Sprintf("fact-entity-%d", i),
				Payload:  map[string]any{"repo_id": "bench-repo"},
			},
			facts.Envelope{
				FactKind: factKindFile,
				FactID:   fmt.Sprintf("fact-file-%d", i),
				Payload:  map[string]any{"repo_id": "bench-repo"},
			},
			facts.Envelope{
				FactKind: factKindSubmodulePin,
				FactID:   fmt.Sprintf("fact-submodule-%d", i),
				Payload:  map[string]any{"repo_id": "bench-repo"},
			},
		)
	}
	return envelopes
}

// TestBenchFactloadCorpusCoversRequestedKinds guards the corpus contract the
// helper comment claims: every fact kind any of the five hoisted wrappers
// requests must be seeded, so a future kind-aware fake exercises each
// wrapper's full kind set instead of silently under-covering one path
// (issue #6359). Each wrapper's kind list lives in one package-level slice
// shared by the wrapper and this test, so adding a requested kind without
// seeding it fails here rather than drifting.
func TestBenchFactloadCorpusCoversRequestedKinds(t *testing.T) {
	t.Parallel()

	seeded := map[string]bool{}
	for _, e := range benchFactloadCorpusEnvelopes(3) {
		seeded[e.FactKind] = true
	}
	requestedBy := map[string]string{}
	for name, kinds := range map[string][]string{
		"codeowners":    codeownersMaterializationFactKinds,
		"documentation": documentationMaterializationFactKinds,
		"rationale":     rationaleMaterializationFactKinds,
		"shellexec":     shellExecMaterializationFactKinds,
		"submodule":     submodulePinMaterializationFactKinds,
	} {
		for _, k := range kinds {
			requestedBy[k] = name
		}
	}
	for kind, name := range requestedBy {
		if !seeded[kind] {
			t.Errorf("kind %q requested by %s wrapper is not seeded in bench corpus", kind, name)
		}
	}
}

// TestFactloadMaterializationWrappersReturnSeededEnvelopes pins that each of
// the five factload-hoisted wrappers (issue #6359) routes through the
// forwarder and returns the seeded generation. RED-first anchor for the
// wall-clock benchmarks below.
func TestFactloadMaterializationWrappersReturnSeededEnvelopes(t *testing.T) {
	t.Parallel()

	envelopes := benchFactloadCorpusEnvelopes(10)
	loader := &stubFactLoader{envelopes: envelopes}
	ctx := context.Background()

	wrappers := map[string]func(context.Context, FactLoader, string, string) ([]facts.Envelope, error){
		"codeowners":    loadCodeownersOwnershipMaterializationFacts,
		"documentation": loadDocumentationMaterializationFacts,
		"rationale":     loadRationaleMaterializationFacts,
		"shellexec":     loadShellExecMaterializationFacts,
		"submodule":     loadSubmodulePinMaterializationFacts,
	}
	wantIDs := make(map[string]int, len(envelopes))
	for _, e := range envelopes {
		wantIDs[e.FactID]++
	}
	for name, wrapper := range wrappers {
		got, err := wrapper(ctx, loader, "scope", "gen")
		if err != nil {
			t.Errorf("%s: error = %v", name, err)
			continue
		}
		gotIDs := make(map[string]int, len(got))
		for _, e := range got {
			gotIDs[e.FactID]++
		}
		if len(got) != len(envelopes) {
			t.Errorf("%s: got %d envelopes, want %d (fallback returns whole generation)", name, len(got), len(envelopes))
		}
		for id, want := range wantIDs {
			if gotIDs[id] != want {
				t.Errorf("%s: FactID %q returned %d times, want %d (wrong generation or dup/drop)", name, id, gotIDs[id], want)
			}
		}
		for id := range gotIDs {
			if wantIDs[id] == 0 {
				t.Errorf("%s: unexpected FactID %q returned", name, id)
			}
		}
	}
}

// The five benchmarks below supply the measured wall-clock half issue #6359
// requires next to the committed `-gcflags=-m=2` inlinability cost figures
// (cost 77-94 against the inline budget of 80). Each measures one hoisted
// wrapper over the shared in-memory corpus; run with e.g.:
// go test ./internal/reducer/ -run '^$' -bench 'MaterializationFacts' -benchmem

// BenchmarkFactloadWrapperFrameOverhead isolates the cost the hoist introduced
// (issue #6359): a direct factload.LoadFactsForKinds call versus the same call
// through the thin codeowners wrapper, with the same compiler, corpus, and
// loader path. The wrappers are new in this change so no pre-hoist wrapper
// baseline exists; direct-vs-wrapper is the comparable base-vs-head delta.
func BenchmarkFactloadWrapperFrameOverhead(b *testing.B) {
	envelopes := benchFactloadCorpusEnvelopes(100)
	ctx := context.Background()
	b.Run("direct", func(b *testing.B) {
		loader := &stubFactLoader{envelopes: envelopes}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			got, err := factload.LoadFactsForKinds(ctx, loader, "scope", "gen", codeownersMaterializationFactKinds)
			if err != nil {
				b.Fatalf("error = %v", err)
			}
			if len(got) != len(envelopes) {
				b.Fatalf("got %d envelopes, want %d", len(got), len(envelopes))
			}
		}
	})
	b.Run("wrapper", func(b *testing.B) {
		loader := &stubFactLoader{envelopes: envelopes}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			got, err := loadCodeownersOwnershipMaterializationFacts(ctx, loader, "scope", "gen")
			if err != nil {
				b.Fatalf("error = %v", err)
			}
			if len(got) != len(envelopes) {
				b.Fatalf("got %d envelopes, want %d", len(got), len(envelopes))
			}
		}
	})
}

func BenchmarkLoadCodeownersOwnershipMaterializationFacts(b *testing.B) {
	envelopes := benchFactloadCorpusEnvelopes(100)
	loader := &stubFactLoader{envelopes: envelopes}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := loadCodeownersOwnershipMaterializationFacts(ctx, loader, "scope", "gen")
		if err != nil {
			b.Fatalf("error = %v", err)
		}
		if len(got) != len(envelopes) {
			b.Fatalf("got %d envelopes, want %d", len(got), len(envelopes))
		}
	}
}

func BenchmarkLoadDocumentationMaterializationFacts(b *testing.B) {
	envelopes := benchFactloadCorpusEnvelopes(100)
	loader := &stubFactLoader{envelopes: envelopes}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := loadDocumentationMaterializationFacts(ctx, loader, "scope", "gen")
		if err != nil {
			b.Fatalf("error = %v", err)
		}
		if len(got) != len(envelopes) {
			b.Fatalf("got %d envelopes, want %d", len(got), len(envelopes))
		}
	}
}

func BenchmarkLoadRationaleMaterializationFacts(b *testing.B) {
	envelopes := benchFactloadCorpusEnvelopes(100)
	loader := &stubFactLoader{envelopes: envelopes}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := loadRationaleMaterializationFacts(ctx, loader, "scope", "gen")
		if err != nil {
			b.Fatalf("error = %v", err)
		}
		if len(got) != len(envelopes) {
			b.Fatalf("got %d envelopes, want %d", len(got), len(envelopes))
		}
	}
}

func BenchmarkLoadShellExecMaterializationFacts(b *testing.B) {
	envelopes := benchFactloadCorpusEnvelopes(100)
	loader := &stubFactLoader{envelopes: envelopes}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := loadShellExecMaterializationFacts(ctx, loader, "scope", "gen")
		if err != nil {
			b.Fatalf("error = %v", err)
		}
		if len(got) != len(envelopes) {
			b.Fatalf("got %d envelopes, want %d", len(got), len(envelopes))
		}
	}
}

func BenchmarkLoadSubmodulePinMaterializationFacts(b *testing.B) {
	envelopes := benchFactloadCorpusEnvelopes(100)
	loader := &stubFactLoader{envelopes: envelopes}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := loadSubmodulePinMaterializationFacts(ctx, loader, "scope", "gen")
		if err != nil {
			b.Fatalf("error = %v", err)
		}
		if len(got) != len(envelopes) {
			b.Fatalf("got %d envelopes, want %d", len(got), len(envelopes))
		}
	}
}
