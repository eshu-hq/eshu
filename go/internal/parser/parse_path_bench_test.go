// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkParsePath reports whole-call ns/op, B/op, and allocs/op for
// Engine.ParsePath across every tree-sitter language in parseBenchCases. Unlike
// BenchmarkParse (parse_bench_test.go), which shares the same table, this
// benchmark's b.SetBytes and per-op cost include ParsePath's own
// content-metadata inference pass, so it is the intended before/after
// measurement point for the #4515 single-physical-read dedup: the same fixture
// corpus, the same >= 10K-LOC padding, run through the full ParsePath call the
// front-half collector partitions actually invoke.
func BenchmarkParsePath(b *testing.B) {
	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, tc := range parseBenchCases {
		b.Run(tc.language, func(b *testing.B) {
			source, loc := loadCaseSource(b, tc)
			repoRoot := b.TempDir()
			filePath := filepath.Join(repoRoot, "input"+tc.ext)
			if err := os.WriteFile(filePath, source, 0o644); err != nil {
				b.Fatalf("write %s: %v", filePath, err)
			}

			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := engine.ParsePath(repoRoot, filePath, false, Options{}); err != nil {
					b.Fatalf("ParsePath(%s) error = %v, want nil", tc.language, err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(loc), "LOC")
		})
	}
}
