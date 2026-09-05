// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchFactloadCorpusEnvelopes returns a synthetic generation covering every
// fact kind the five hoisted wrappers request, so the wall-clock cost of the
// wrapper + forwarder path can be measured without a live store (issue
// #6359). The stubFactLoader test double serves as the store fixture: it
// implements only the base FactLoader port, exercising the same fallback
// branch a non-push-down store takes in production.
func benchFactloadCorpusEnvelopes(n int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, 5*n+1)
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
	for name, wrapper := range wrappers {
		got, err := wrapper(ctx, loader, "scope", "gen")
		if err != nil {
			t.Errorf("%s: error = %v", name, err)
			continue
		}
		if len(got) != len(envelopes) {
			t.Errorf("%s: got %d envelopes, want %d (fallback returns whole generation)", name, len(got), len(envelopes))
		}
	}
}

// The five benchmarks below supply the measured wall-clock half issue #6359
// requires next to the committed `-gcflags=-m=2` inlinability cost figures
// (cost 77-94 against the inline budget of 80). Each measures one hoisted
// wrapper over the shared in-memory corpus; run with e.g.:
// go test ./internal/reducer/ -run '^$' -bench 'MaterializationFacts' -benchmem

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
