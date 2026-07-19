// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// routePathParamPlaceholder is the literal path segment substituted for every
// "{param}"-style template segment when a fact-kind-registry read_surface
// literal is turned into a synthetic request path. Its value is irrelevant --
// *http.ServeMux matches any non-empty segment against a wildcard -- only
// non-emptiness matters.
const routePathParamPlaceholder = "x"

// syntheticRequestForRouteSurface builds a synthetic, unsent *http.Request
// for a "METHOD /path" read_surface literal (for example
// "GET /api/v0/incidents/{incident_id}/context"), substituting
// routePathParamPlaceholder for every "{param}" segment so the request has a
// concrete path *http.ServeMux can match against a registered wildcard
// pattern.
func syntheticRequestForRouteSurface(surface string) (*http.Request, error) {
	method, segments, ok := mcp.SplitAPIRouteSurface(surface)
	if !ok {
		return nil, fmt.Errorf("read_surface %q is not a well-formed \"METHOD /path\" literal", surface)
	}
	concrete := make([]string, len(segments))
	for i, seg := range segments {
		if mcp.IsRoutePathParamSegment(seg) {
			concrete[i] = routePathParamPlaceholder
			continue
		}
		concrete[i] = seg
	}
	path := "/" + strings.Join(concrete, "/")
	return httptest.NewRequest(method, path, nil), nil
}

// factKindReadSurfaceMounted reports whether a fact-kind-registry
// read_surface literal resolves to a route actually registered on mux --
// not merely documented in the OpenAPI-derived surface inventory
// (capabilitycatalog.LoadSurfaceInventory,
// TestFactKindRegistryReadSurfacesResolveToLiveRoutes's denominator in
// go/internal/mcp/read_surface_consumer_existence_test.go), the two being
// capable of drifting apart: a route can be declared in an
// openapi_paths_*.go source file (and so appear in the served OpenAPI spec)
// without its handler's Mount ever being called by the production wiring
// that builds mux, in which case a caller following the documented route
// gets a live 404.
//
// mux.Handler(req) is the stdlib's own route-resolution entrypoint: for a
// request matching a registered pattern it returns that exact pattern
// string; for anything else (including a syntactically valid but
// unregistered path) it returns the empty string alongside the internal
// NotFoundHandler. An empty returned pattern is therefore conclusive
// evidence the route is not mounted on mux, regardless of what any
// documentation claims.
func factKindReadSurfaceMounted(mux *http.ServeMux, surface string) (mounted bool, matchedPattern string, err error) {
	req, err := syntheticRequestForRouteSurface(surface)
	if err != nil {
		return false, "", err
	}
	_, pattern := mux.Handler(req)
	return pattern != "", pattern, nil
}
