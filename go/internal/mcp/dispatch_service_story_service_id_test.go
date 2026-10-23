// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestResolveRouteServiceStoryAcceptsServiceIDAlone proves the root dispatch
// chain, not only the child selector, accepts service_id as the sole selector.
func TestResolveRouteServiceStoryAcceptsServiceIDAlone(t *testing.T) {
	t.Parallel()

	const fullID = "workload:repository:r_sample:checkout-service"
	route, err := resolveRoute("get_service_story", map[string]any{"service_id": fullID})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Path, "/api/v0/services/"+fullID+"/story"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	if got := route.Query["service_id"]; got != fullID {
		t.Fatalf("route.Query[service_id] = %#v, want %q", got, fullID)
	}
}
