// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content/shape"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// BenchmarkContentStoreUngoverned measures what the policy costs every
// repository that binds no non-store policy, which is every repository today:
// one registry flag check per snapshot and one nil-map read per emitted file.
func BenchmarkContentStoreUngoverned(b *testing.B) {
	registry := parser.DefaultRegistry()
	files := make([]shape.File, 5000)
	for i := range files {
		files[i] = shape.File{Path: "src/pkg/file.go", Body: strings.Repeat("x", 64)}
	}
	body := []byte(strings.Repeat("x", 4096))
	b.Run("snapshot_stage_5000_files", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if stores := applyContentStoreToShapeFiles(registry, "/repo", files, nil); stores != nil {
				b.Fatal("ungoverned registry returned a policy map")
			}
		}
	})
	b.Run("emit_stage_per_file", func(b *testing.B) {
		var tally contentStoreTally
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = tally.storedBytes(nil, "src/pkg/file.go", body)
		}
	})
}
