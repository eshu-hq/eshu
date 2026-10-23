// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOversizedServiceStoryShipsFiftyOfSixtyRows pins how the size fixture
// relates to what the story ships. The fixture holds 60 rows in each family,
// but BuildServiceStoryResponse caps every list to serviceStoryItemLimit (50);
// the 60 only drives the pre-cap totals in raw_context_limits and
// result_limits. So the byte sizes measured over this fixture are already the
// 50-row ship size of a story with every family past its cap, not a bound above
// it (#8798).
func TestOversizedServiceStoryShipsFiftyOfSixtyRows(t *testing.T) {
	t.Parallel()

	const heldRows = 60
	context := testutil.OversizedServiceDossierContext()
	story := BuildServiceStoryResponse("checkout-service", context)
	evidence := querycontract.MapValue(story, "deployment_evidence")

	for name, shipped := range map[string]int{
		"upstream_dependencies":                 len(querycontract.MapSliceValue(story, "upstream_dependencies")),
		"hostnames":                             len(querycontract.MapSliceValue(story, "hostnames")),
		"entrypoints":                           len(querycontract.MapSliceValue(story, "entrypoints")),
		"dependents":                            len(querycontract.MapSliceValue(story, "dependents")),
		"consumer_repositories":                 len(querycontract.MapSliceValue(story, "consumer_repositories")),
		"provisioning_source_chains":            len(querycontract.MapSliceValue(story, "provisioning_source_chains")),
		"cloud_resources":                       len(querycontract.MapSliceValue(story, "cloud_resources")),
		"deployment_evidence.artifacts":         len(querycontract.MapSliceValue(evidence, "artifacts")),
		"deployment_evidence.delivery_paths":    len(querycontract.MapSliceValue(evidence, "delivery_paths")),
		"api_surface.endpoints":                 len(querycontract.MapSliceValue(querycontract.MapValue(story, "api_surface"), "endpoints")),
		"evidence_graph.edges":                  len(querycontract.MapSliceValue(querycontract.MapValue(story, "evidence_graph"), "edges")),
		"investigation.repositories_considered": len(querycontract.MapSliceValue(querycontract.MapValue(story, "investigation"), "repositories_considered")) - 1,
	} {
		if shipped != serviceStoryItemLimit {
			t.Errorf("%s ships %d rows, want the %d-row cap", name, shipped, serviceStoryItemLimit)
		}
	}
	for _, key := range []string{"hostnames", "entrypoints", "dependents", "consumer_repositories", "provisioning_source_chains", "cloud_resources"} {
		if held := len(querycontract.MapSliceValue(context, key)); held != heldRows {
			t.Errorf("fixture holds %d %s rows, want %d past the cap", held, key, heldRows)
		}
		limit := querycontract.MapValue(querycontract.MapValue(story, "raw_context_limits"), key)
		if got := querycontract.IntVal(limit, "count"); got != heldRows {
			t.Errorf("raw_context_limits.%s.count = %d, want the %d rows the story received", key, got, heldRows)
		}
	}
}
