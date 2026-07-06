// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchDocumentationMentionEnvelopes returns n synthetic
// documentation_entity_mention envelopes, each resolving exactly to one
// candidate entity, so the typed-decode path (Contract System v1 Wave 4e)
// can be benchmarked at repo-scale corpus size against the pre-typing raw
// payloadStr/mapSlice baseline.
func benchDocumentationMentionEnvelopes(n int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, n)
	for i := 0; i < n; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactKind: facts.DocumentationEntityMentionFactKind,
			FactID:   fmt.Sprintf("fact-mention-%d", i),
			Payload: map[string]any{
				"document_id":       fmt.Sprintf("doc:git:repo-%d:README.md", i),
				"section_id":        fmt.Sprintf("sec-%d", i),
				"mention_kind":      "code_symbol",
				"resolution_status": facts.DocumentationMentionResolutionExact,
				"candidate_refs": []any{
					map[string]any{"kind": "entity", "id": fmt.Sprintf("uid:func-%d", i)},
				},
			},
		})
	}
	return envelopes
}

// BenchmarkExtractDocumentationEdgeRowsWithQuarantine measures the typed
// decode path for documentation_entity_mention extraction (Contract System
// v1 Wave 4e), the no-regression proof required alongside the accuracy
// migration. See go/internal/reducer/AGENTS.md's Evidence notes for the
// before/after numbers measured against this benchmark.
func BenchmarkExtractDocumentationEdgeRowsWithQuarantine(b *testing.B) {
	envelopes := benchDocumentationMentionEnvelopes(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, quarantined, err := ExtractDocumentationEdgeRowsWithQuarantine(envelopes, "bench-scope")
		if err != nil {
			b.Fatalf("ExtractDocumentationEdgeRowsWithQuarantine() error = %v", err)
		}
		if len(quarantined) != 0 {
			b.Fatalf("quarantined = %d, want 0", len(quarantined))
		}
		if len(rows) != 5000 {
			b.Fatalf("rows = %d, want 5000", len(rows))
		}
	}
}

// benchDocumentationDeltaEnvelopes returns one repository delta fact
// declaring n changed relative paths plus n matching documentation_document
// facts, so buildDocumentationDeltaScopeWithQuarantine's typed decode of
// documentation_document envelopes can be benchmarked at repo-scale corpus
// size.
func benchDocumentationDeltaEnvelopes(n int) []facts.Envelope {
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		paths = append(paths, fmt.Sprintf("docs/doc-%d.md", i))
	}
	envelopes := make([]facts.Envelope, 0, n+1)
	envelopes = append(envelopes, facts.Envelope{
		FactKind: factKindRepository,
		Payload: map[string]any{
			"repo_id":                      "bench-repo",
			"local_path":                   "/repo",
			"delta_generation":             true,
			"delta_relative_paths":         paths,
			"delta_deleted_relative_paths": []string{},
		},
	})
	for i := 0; i < n; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactKind: facts.DocumentationDocumentFactKind,
			FactID:   fmt.Sprintf("fact-doc-%d", i),
			Payload: map[string]any{
				"document_id": fmt.Sprintf("doc:git:bench-repo:docs/doc-%d.md", i),
				"source_metadata": map[string]any{
					"path":    fmt.Sprintf("docs/doc-%d.md", i),
					"repo_id": "bench-repo",
				},
			},
		})
	}
	return envelopes
}

// BenchmarkBuildDocumentationDeltaScopeWithQuarantine measures the typed
// decode path for documentation_document delta-scope building (Contract
// System v1 Wave 4e), the no-regression proof required alongside the
// accuracy migration for the second (documentation_document) decode site.
func BenchmarkBuildDocumentationDeltaScopeWithQuarantine(b *testing.B) {
	envelopes := benchDocumentationDeltaEnvelopes(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope, quarantined, err := buildDocumentationDeltaScopeWithQuarantine(envelopes, "bench-scope")
		if err != nil {
			b.Fatalf("buildDocumentationDeltaScopeWithQuarantine() error = %v", err)
		}
		if len(quarantined) != 0 {
			b.Fatalf("quarantined = %d, want 0", len(quarantined))
		}
		if len(scope.documentIDs) != 5000 {
			b.Fatalf("documentIDs = %d, want 5000", len(scope.documentIDs))
		}
	}
}
