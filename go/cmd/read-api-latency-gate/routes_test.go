// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

func TestNoArgGetRoutesFiltersToImplementedNoArgGET(t *testing.T) {
	inv := capabilitycatalog.SurfaceInventory{
		Surfaces: []capabilitycatalog.SurfaceRecord{
			{Category: capabilitycatalog.SurfaceAPIRoute, Name: "GET /api/v0/repositories", Readiness: capabilitycatalog.ReadinessImplemented},
			{Category: capabilitycatalog.SurfaceAPIRoute, Name: "GET /api/v0/auth/admin/idp-group-mappings/{mapping_ref}", Readiness: capabilitycatalog.ReadinessImplemented},
			{Category: capabilitycatalog.SurfaceAPIRoute, Name: "DELETE /api/v0/auth/browser-session", Readiness: capabilitycatalog.ReadinessImplemented},
			{Category: capabilitycatalog.SurfaceAPIRoute, Name: "GET /api/v0/experimental/thing", Readiness: capabilitycatalog.ReadinessPartial},
			{Category: capabilitycatalog.SurfaceMCPTool, Name: "get_repo_context", Readiness: capabilitycatalog.ReadinessImplemented},
		},
	}

	got := NoArgGetRoutes(inv)
	want := []string{"GET /api/v0/repositories"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NoArgGetRoutes = %v, want %v", got, want)
	}
}

func TestNoArgGetRoutesIsSorted(t *testing.T) {
	inv := capabilitycatalog.SurfaceInventory{
		Surfaces: []capabilitycatalog.SurfaceRecord{
			{Category: capabilitycatalog.SurfaceAPIRoute, Name: "GET /api/v0/zzz", Readiness: capabilitycatalog.ReadinessImplemented},
			{Category: capabilitycatalog.SurfaceAPIRoute, Name: "GET /api/v0/aaa", Readiness: capabilitycatalog.ReadinessImplemented},
		},
	}
	got := NoArgGetRoutes(inv)
	want := []string{"GET /api/v0/aaa", "GET /api/v0/zzz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NoArgGetRoutes order = %v, want %v", got, want)
	}
}

func TestRoutePathSplitsMethodAndPath(t *testing.T) {
	method, path, err := SplitRoute("GET /api/v0/repositories")
	if err != nil {
		t.Fatalf("SplitRoute: %v", err)
	}
	if method != "GET" || path != "/api/v0/repositories" {
		t.Fatalf("SplitRoute = (%q, %q), want (GET, /api/v0/repositories)", method, path)
	}
}

func TestRoutePathRejectsMalformed(t *testing.T) {
	if _, _, err := SplitRoute("not-a-route"); err == nil {
		t.Fatalf("expected error for malformed route")
	}
}
