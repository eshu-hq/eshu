// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

// BenchmarkGrantInlineCapExceeded measures the #5408 signal's cost on the read
// path, because it is not free: grantInlineCapExceeded calls
// scopeGrantInlineScalars, which builds and sorts the grant-id union. The infra
// search path already built that union three times per request; this adds a
// fourth.
//
// Sized at the shapes that actually occur: a typical scoped token (8 grants),
// a large one just under the cap (128), and a pathological one over it, where
// the union work is largest and the truncation actually fires.
func BenchmarkGrantInlineCapExceeded(b *testing.B) {
	ids := func(prefix string, n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("%s-%08d", prefix, i))
		}
		return out
	}

	for _, bc := range []struct {
		name   string
		filter repositoryAccessFilter
	}{
		{
			name:   "all_scopes",
			filter: repositoryAccessFilter{allScopes: true},
		},
		{
			name:   "typical_8_grants",
			filter: repositoryAccessFilter{allowedRepositoryIDs: ids("repo", 8)},
		},
		{
			name:   "at_cap_128_grants",
			filter: repositoryAccessFilter{allowedRepositoryIDs: ids("repo", maxScopeGrantInlineTerms)},
		},
		{
			name: "over_cap_512_grants",
			filter: repositoryAccessFilter{
				allowedRepositoryIDs: ids("repo", 384),
				allowedScopeIDs:      ids("scope", 128),
			},
		},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if bc.filter.grantInlineCapExceeded() {
					continue
				}
			}
		})
	}
}
