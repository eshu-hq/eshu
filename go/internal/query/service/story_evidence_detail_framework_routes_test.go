// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestFrameworkRoutesAreNotInTheContentFallbackEndpointTotal pins the unflagged
// bound the Omission total rules page names for api_surface.endpoints on the
// content fallback: framework routes add endpoint rows but not endpoint_count,
// the story caps the rows at 50, and no flag is set, so the omission total is
// the capped row count, not the 120 routes read (#8798).
func TestFrameworkRoutesAreNotInTheContentFallbackEndpointTotal(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 120)
	for i := range 120 {
		paths = append(paths, fmt.Sprintf("/route/%03d", i))
	}
	surface := buildServiceAPISurface(QueryEvidence{
		FrameworkRoutes: []FrameworkRouteEvidence{{
			Framework:    "hapi",
			RelativePath: "src/routes.js",
			RoutePaths:   paths,
			RouteMethods: []string{"GET"},
		}},
	})
	if got := len(querycontract.MapSliceValue(surface, "endpoints")); got != 120 {
		t.Fatalf("built endpoint rows = %d, want 120", got)
	}
	if got := querycontract.IntVal(surface, "endpoint_count"); got != 0 {
		t.Fatalf("endpoint_count = %d, want 0: framework routes are not in it", got)
	}

	data := map[string]any{"api_surface": buildServiceDossierAPISurface(map[string]any{"api_surface": surface})}
	omissions := querycontract.ApplyStoryEvidenceDetail(data, querycontract.ContextEvidenceDetailHandles)
	for _, omission := range omissions {
		if omission.Section == "api_surface.endpoints" {
			if omission.Total != serviceStoryItemLimit {
				t.Fatalf("api_surface.endpoints omission total = %d, want the capped %d", omission.Total, serviceStoryItemLimit)
			}
			if querycontract.BoolVal(querycontract.MapValue(data, "api_surface"), "detail_truncated") {
				t.Fatal("content fallback sets no detail_truncated flag; the doc names this bound as unflagged")
			}
			return
		}
	}
	t.Fatalf("no api_surface.endpoints omission in %#v", omissions)
}
