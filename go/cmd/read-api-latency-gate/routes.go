// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// NoArgGetRoutes returns every implemented "GET /path" route in inv that
// takes no path parameters, sorted by path. Deriving the route set from the
// generated surface inventory (rather than a hand-maintained list) means a
// newly added no-arg GET route is swept — and budgeted — automatically
// (issue #6797).
func NoArgGetRoutes(inv capabilitycatalog.SurfaceInventory) []string {
	var routes []string
	for _, rec := range inv.Surfaces {
		if rec.Category != capabilitycatalog.SurfaceAPIRoute {
			continue
		}
		if rec.Readiness != capabilitycatalog.ReadinessImplemented {
			continue
		}
		method, path, err := SplitRoute(rec.Name)
		if err != nil || method != "GET" {
			continue
		}
		if strings.Contains(path, "{") {
			continue
		}
		routes = append(routes, rec.Name)
	}
	sort.Strings(routes)
	return routes
}

// SplitRoute splits a surface inventory route name ("METHOD /path") into its
// method and path.
func SplitRoute(route string) (method, path string, err error) {
	method, path, ok := strings.Cut(route, " ")
	if !ok || method == "" || path == "" {
		return "", "", fmt.Errorf("malformed route %q: want \"METHOD /path\"", route)
	}
	return method, path, nil
}
