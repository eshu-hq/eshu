// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// BenchmarkValueFlowLookupBuild measures the cost of building the shared
// entity-lookup map and function UID resolver for a representative entity
// count. This is the cost the caller now pays once instead of 5×.
func BenchmarkValueFlowLookupBuild(b *testing.B) {
	counts := []int{10, 100, 1000, 10000}
	for _, n := range counts {
		b.Run("n="+strconv.Itoa(n), func(b *testing.B) {
			entities := make([]content.EntityRecord, n)
			for i := range entities {
				entities[i] = content.EntityRecord{
					EntityID:   "e-" + strconv.Itoa(i),
					Path:       "src/file" + strconv.Itoa(i%10) + ".go",
					EntityType: "Function",
					EntityName: "func" + strconv.Itoa(i),
					StartLine:  i + 1,
				}
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = buildEntityUIDLookup(entities)
				_ = newFunctionUIDResolver(entities)
			}
		})
	}
}
