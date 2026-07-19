// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// factKindReadSurfaceNone is the literal sentinel specs/fact-kind-registry.v1.yaml
// uses for a family with no read surface at all (reducer-internal families
// that never leave the graph as a versioned read response). It is not a
// claim, so the #5335 gate skips it rather than trying to resolve it.
const factKindReadSurfaceNone = "none"

// factKindRegistryFamiliesFile is the minimal shape read from
// specs/fact-kind-registry.v1.yaml for the #5335 gate: only the family-level
// read_surface field. read_surface_overrides (per-kind route substitutions,
// including at least one MCP-tool-shaped override) is intentionally out of
// scope for v1 -- see the doc comment on
// TestFactKindRegistryReadSurfacesResolveToLiveRoutes.
type factKindRegistryFamiliesFile struct {
	Families map[string]struct {
		ReadSurface string `yaml:"read_surface"`
	} `yaml:"families"`
}

// loadFactKindRegistryReadSurfaces reads the family-level read_surface field
// from specs/fact-kind-registry.v1.yaml, keyed by family name. Families with
// no read_surface or the "none" sentinel are omitted. A missing or malformed
// file is an error: this is the fact-kind side of the #5335 read-surface
// consumer-existence gate's denominator, so a silent empty read would falsely
// report every family as covered.
func loadFactKindRegistryReadSurfaces(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured fact-kind registry under specs/, not external input
	if err != nil {
		return nil, fmt.Errorf("read fact-kind registry %s: %w", path, err)
	}
	var parsed factKindRegistryFamiliesFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse fact-kind registry %s: %w", path, err)
	}
	if len(parsed.Families) == 0 {
		return nil, fmt.Errorf("fact-kind registry %s: no families found", path)
	}
	out := make(map[string]string, len(parsed.Families))
	for family, entry := range parsed.Families {
		surface := strings.TrimSpace(entry.ReadSurface)
		if surface == "" || surface == factKindReadSurfaceNone {
			continue
		}
		out[family] = surface
	}
	return out, nil
}

// splitAPIRouteSurface splits a "METHOD /path" surface name into an
// uppercase HTTP method and slash-separated path segments with the trailing
// slash stripped, for positional route-template matching. ok is false for a
// malformed surface (no method/path separator).
func splitAPIRouteSurface(surface string) (method string, segments []string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(surface), " ", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", nil, false
	}
	method = strings.ToUpper(parts[0])
	path := strings.TrimSuffix(parts[1], "/")
	segments = strings.Split(strings.Trim(path, "/"), "/")
	return method, segments, true
}

// isRoutePathParamSegment reports whether segment is a "{param}"-style
// template placeholder.
func isRoutePathParamSegment(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && len(segment) > 2
}

// apiRouteSurfaceMatches reports whether claimed (a fact-kind-registry
// read_surface literal) and live (a served route from the live inventory)
// name the same route: same method, same segment count, and every literal
// (non-{param}) segment equal at the same position. A {param} segment on
// either side matches positionally regardless of its name, so
// "GET /api/v0/incidents/{id}/context" and
// "GET /api/v0/incidents/{incident_id}/context" are the same route.
func apiRouteSurfaceMatches(claimed, live string) bool {
	claimedMethod, claimedSegments, ok := splitAPIRouteSurface(claimed)
	if !ok {
		return false
	}
	liveMethod, liveSegments, ok := splitAPIRouteSurface(live)
	if !ok {
		return false
	}
	if claimedMethod != liveMethod || len(claimedSegments) != len(liveSegments) {
		return false
	}
	for i := range claimedSegments {
		if isRoutePathParamSegment(claimedSegments[i]) || isRoutePathParamSegment(liveSegments[i]) {
			continue
		}
		if claimedSegments[i] != liveSegments[i] {
			return false
		}
	}
	return true
}

// resolveFactKindReadSurface reports whether a family's literal read_surface
// route matches any route in the live inventory.
func resolveFactKindReadSurface(family, surface string, liveRoutes []string) (ok bool, reason string) {
	for _, live := range liveRoutes {
		if apiRouteSurfaceMatches(surface, live) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("family %q read_surface %q does not match any live API route", family, surface)
}

// factKindRowDigest hashes a family's exact read_surface string so the
// grandfather ledger can detect an edit.
func factKindRowDigest(readSurface string) string {
	sum := sha256.Sum256([]byte(readSurface))
	return fmt.Sprintf("%x", sum)
}

// liveImplementedAPIRoutes returns every "METHOD /path" surface name from the
// committed, generated surface inventory (capabilitycatalog.LoadSurfaceInventory)
// whose category is api_route and readiness is implemented -- the actual
// served route set the OpenAPI spec promises callers today. Mirrors
// implementedAPIRouteSurfaces in go/internal/query/auth_scoped_routes_completeness_test.go.
func liveImplementedAPIRoutes() ([]string, error) {
	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		return nil, fmt.Errorf("capabilitycatalog.LoadSurfaceInventory: %w", err)
	}
	var routes []string
	for _, surface := range inventory.Surfaces {
		if surface.Category != capabilitycatalog.SurfaceAPIRoute || surface.Readiness != capabilitycatalog.ReadinessImplemented {
			continue
		}
		routes = append(routes, surface.Name)
	}
	return routes, nil
}
