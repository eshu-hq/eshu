// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"os"
	"path/filepath"
	"testing"
)

// Benchmark Evidence: issue #4873. Compares the ordered top-level walk this
// package used unconditionally before the fix (unmarshalOrderedJSONObject,
// which captures a json.RawMessage copy per top-level value) against the
// key-order-only scan (topLevelJSONKeyOrder) that Parse now uses for
// filenames whose dispatch never reads nested ordered entries -- the
// dedicated lockfile parsers (package-lock.json and siblings), CloudFormation
// templates, and dbt manifests. See docs/internal/agent-guide.md
// "Mandatory Prove-The-Theory-First" and go/internal/parser/json/AGENTS.md.
//
// Fixture: testdata/large-package-lock.json, a real 277KB package-lock.json
// (this repository's own root lockfile) -- the issue's motivating "large
// lockfile" shape.
func loadBenchmarkFixture(b *testing.B) []byte {
	b.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "large-package-lock.json"))
	if err != nil {
		b.Fatalf("os.ReadFile() error = %v, want nil", err)
	}
	return data
}

// BenchmarkOrderedWalk is the OLD path Parse used for every JSON file,
// including files whose dispatch never reads topLevelEntries.
func BenchmarkOrderedWalk(b *testing.B) {
	data := loadBenchmarkFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := unmarshalOrderedJSONObject(data); err != nil {
			b.Fatalf("unmarshalOrderedJSONObject() error = %v", err)
		}
	}
}

// BenchmarkKeyOrderOnly is the NEW path Parse uses for filenames where
// jsonFilenameNeedsOrderedEntries is false (package-lock.json and its four
// sibling lockfile formats, CloudFormation templates, dbt manifests, and any
// other JSON file that is not package.json/composer.json/tsconfig*.json).
func BenchmarkKeyOrderOnly(b *testing.B) {
	data := loadBenchmarkFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := topLevelJSONKeyOrder(data); err != nil {
			b.Fatalf("topLevelJSONKeyOrder() error = %v", err)
		}
	}
}
