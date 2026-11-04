// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainimpacttools

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// TestInventoryRouteForwardsGroupedDimensionsAndProfile pins the #9873
// mapping: the three grouped dimensions reach the HTTP route verbatim, the
// profile passes through, and a composition filter (group_by=environment with
// service_id) keeps both keys.
func TestInventoryRouteForwardsGroupedDimensionsAndProfile(t *testing.T) {
	t.Parallel()

	for _, dimension := range []string{"service_id", "workload_id", "environment"} {
		for _, profile := range []string{"", "precise", "comprehensive"} {
			request, handled := Route("get_supply_chain_impact_inventory", routecontract.Arguments{
				"group_by":   dimension,
				"profile":    profile,
				"service_id": "service:example-api",
			})
			if !handled {
				t.Fatalf("Route(get_supply_chain_impact_inventory) handled = false")
			}
			if request.Method != "GET" || request.Path != "/api/v0/supply-chain/impact/inventory" {
				t.Fatalf("request = %s %s, want GET /api/v0/supply-chain/impact/inventory", request.Method, request.Path)
			}
			if got := request.Query["group_by"]; got != dimension {
				t.Errorf("group_by = %q, want %q", got, dimension)
			}
			if got := request.Query["profile"]; got != profile {
				t.Errorf("%s profile = %q, want %q passed through", dimension, got, profile)
			}
			if got := request.Query["service_id"]; got != "service:example-api" {
				t.Errorf("%s service_id = %q, want the composition filter forwarded", dimension, got)
			}
		}
	}
}
