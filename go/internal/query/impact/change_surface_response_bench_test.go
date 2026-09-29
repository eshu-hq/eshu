// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

var changeSurfaceResponseBenchmarkResult map[string]any

// BenchmarkChangeSurfaceResponseCoverage measures the bounded response shaper.
func BenchmarkChangeSurfaceResponseCoverage(b *testing.B) {
	cases := []struct {
		name     string
		offset   int
		coverage map[string]any
	}{
		{
			name:     "filled_100",
			coverage: map[string]any{"state": "supported"},
		},
		{
			name:   "empty_offset_unknown",
			offset: 5000,
			coverage: map[string]any{
				"state":                 "partial",
				"candidate_pool_status": "unknown_empty_page",
			},
		},
	}
	handler := &Handler{}
	for _, testCase := range cases {
		b.Run(testCase.name, func(b *testing.B) {
			req := ChangeSurfaceInvestigationRequest{
				RepoID: "repo-bench", Topic: "change", Limit: 100, Offset: testCase.offset,
			}
			symbols := make([]map[string]any, 0, 100)
			if testCase.offset == 0 {
				for range 100 {
					symbols = append(symbols, map[string]any{
						"entity_type": "Function", "name": "Change", "relative_path": "src/change.go",
					})
				}
			}
			codeSurface := map[string]any{
				"touched_symbols": symbols,
				"symbol_count":    len(symbols),
				"coverage":        testCase.coverage,
			}
			resolution := changeSurfaceNoTargetResolution(req)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				changeSurfaceResponseBenchmarkResult = handler.changeSurfaceResponse(
					req, resolution, codeSurface, nil, false,
				)
			}
		})
	}
}
