// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkPreScanSelectedLanguages(b *testing.B) {
	engine, err := DefaultEngine()
	if err != nil {
		b.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, tc := range parseBenchCases {
		if !benchmarkPreScanLanguage(tc.language) {
			continue
		}
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
				if _, err := engine.PreScanRepositoryPaths(repoRoot, []string{filePath}); err != nil {
					b.Fatalf("PreScanRepositoryPaths(%s) error = %v, want nil", tc.language, err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(loc), "LOC")
		})
	}
}

func benchmarkPreScanLanguage(language string) bool {
	switch language {
	case "javascript", "typescript", "php", "python":
		return true
	default:
		return false
	}
}
