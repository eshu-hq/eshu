// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	stdjson "encoding/json"
	"testing"
)

// Benchmark Evidence: issue #5329. Quantifies the one-time O(file) newline-
// scan (buildNewlineIndex) plus per-key O(log n) binary search (lineAt) this
// fix adds so JSON producers can report real source line_numbers instead of a
// fabricated per-section counter. Fixture: testdata/large-package-lock.json,
// the same real 277KB package-lock.json (609 top-level "packages" entries)
// BenchmarkOrderedWalk/BenchmarkKeyOrderOnly already use for the #4873
// lockfile-performance baseline, so all three benchmarks are directly
// comparable on the same input shape.
//
// BenchmarkNewlineIndexBuild isolates the added one-time scan cost.
// BenchmarkLockfileSectionLines is the real end-to-end NEW path lockfile
// producers call (package_lock.go's real-line replacement for the old
// lineNumber++ counter): newline-index build + jsonObjectExtractKey +
// jsonObjectKeyLines for the "packages" section. Comparing it against
// BenchmarkKeyOrderOnly (the #4873 baseline decode-only pass over the same
// file) shows the added real-line-tracking cost is small relative to the
// json.Decoder pass every JSON producer already pays, and both are far below
// the ordered-walk cost this package already accepted for
// package.json/composer.json/tsconfig*.json.
func BenchmarkNewlineIndexBuild(b *testing.B) {
	data := loadBenchmarkFixture(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildNewlineIndex(data)
	}
}

// BenchmarkLockfileSectionLines is the NEW real-line path: one newline-index
// build over the whole file plus a targeted "packages" section key/line scan.
// This replaces the OLD fabricated `lineNumber := 1; lineNumber++` counter,
// which was O(1) per key but wrong (issue #5329) -- there is no equivalent OLD
// benchmark because the old path did no source-position work at all. The
// accuracy fix is justified by this being a bounded, one-pass-per-file cost,
// not by matching the old (free but wrong) counter's speed.
func BenchmarkLockfileSectionLines(b *testing.B) {
	data := loadBenchmarkFixture(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lines := lockfileSectionLines(data, "packages")
		if len(lines) == 0 {
			b.Fatalf("lockfileSectionLines() returned no entries, want > 0")
		}
	}
}

// BenchmarkLineAtLookup isolates the per-lookup binary-search cost once an
// index is built, using a byte offset near the end of the fixture (the
// worst-case search depth for a 277KB file).
func BenchmarkLineAtLookup(b *testing.B) {
	data := loadBenchmarkFixture(b)
	idx := buildNewlineIndex(data)
	offset := int64(len(data) - 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.lineAt(offset)
	}
}

// BenchmarkStdlibUnmarshalMap is the pre-existing, unavoidable baseline every
// JSON file already pays regardless of this fix: language.go's Parse always
// runs stdjson.Unmarshal(normalizedBytes, &document) into a map[string]any
// once, for every JSON file, before dispatching to any filename-specific
// branch. This anchors BenchmarkLockfileSectionLines's added cost against the
// cost Parse already spends on the same fixture, so the comparison is
// old-total vs new-total, not new-work vs nothing.
func BenchmarkStdlibUnmarshalMap(b *testing.B) {
	data := loadBenchmarkFixture(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var document any
		if err := stdjson.Unmarshal(data, &document); err != nil {
			b.Fatalf("json.Unmarshal() error = %v", err)
		}
	}
}
