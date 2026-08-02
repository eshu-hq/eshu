// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// importBenchEnvelopes builds a repository generation of the shape the git
// collector emits: one file fact per source file, each carrying the parser's
// imports bucket. importsPerFile of 12 is around the upper end of what real
// source files carry.
func importBenchEnvelopes(files, importsPerFile int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, files+1)
	envelopes = append(envelopes, importRepositoryFact())
	for f := 0; f < files; f++ {
		entries := make([]map[string]any, 0, importsPerFile)
		for i := 0; i < importsPerFile; i++ {
			entries = append(entries, map[string]any{
				"name":        fmt.Sprintf("github.com/acme/pkg-%d/sub", i),
				"line_number": i + 3,
				"lang":        "go",
			})
		}
		envelopes = append(envelopes, fileFactWithImports(
			fmt.Sprintf("f-%d", f), fmt.Sprintf("pkg/dir%d/file%d.go", f%64, f), "go", entries))
	}
	return envelopes
}

// BenchmarkExtractImportsFromFiles measures the import extractor alone, so its
// cost can be read against BenchmarkBuildCanonicalMaterializationWithImports
// below — which is what actually decides whether the extractor is a meaningful
// share of a repository generation's projection.
func BenchmarkExtractImportsFromFiles(b *testing.B) {
	for _, files := range []int{100, 2000} {
		envelopes := importBenchEnvelopes(files, 12)
		b.Run(fmt.Sprintf("files=%d", files), func(b *testing.B) {
			_, parsed, _ := extractFilesWithQuarantine(envelopes, "repo-abc", "/repos/my-project")
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rows, modules := extractImportsFromFiles(parsed)
				if len(rows) == 0 || len(modules) == 0 {
					b.Fatal("extractor produced nothing")
				}
			}
		})
	}
}

// BenchmarkBuildCanonicalMaterializationWithImports measures the whole
// materialization build on the same input, so the extractor's share of a
// repository generation's projection can be read off directly.
func BenchmarkBuildCanonicalMaterializationWithImports(b *testing.B) {
	for _, files := range []int{100, 2000} {
		envelopes := importBenchEnvelopes(files, 12)
		b.Run(fmt.Sprintf("files=%d", files), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				mat, _ := buildCanonicalMaterialization(testScope(), testGeneration(), envelopes)
				if len(mat.Imports) == 0 {
					b.Fatal("no imports materialized")
				}
			}
		})
	}
}
