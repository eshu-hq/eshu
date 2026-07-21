// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// factKindRegistryFullFamily holds the family-level fields needed by the D1
// route-serves-data gate and the D2 per-kind consumer existence gate.
type factKindRegistryFullFamily struct {
	ReadSurface   string   `yaml:"read_surface"`
	ReducerDomain string   `yaml:"reducer_domain"`
	Kinds         []string `yaml:"kinds"`
}

// factKindRegistryFullFile is the complete family-level shape from
// specs/fact-kind-registry.v1.yaml — richer than factKindRegistryFamiliesFile
// because it includes reducer_domain and kinds, not only read_surface.
type factKindRegistryFullFile struct {
	Families map[string]factKindRegistryFullFamily `yaml:"families"`
}

// loadFactKindRegistryFull reads the full registry — every family's
// read_surface, reducer_domain, and kinds list — from the committed YAML file.
func loadFactKindRegistryFull(path string) (map[string]factKindRegistryFullFamily, error) {
	raw, err := os.ReadFile(path) // #nosec G304 — path is the committed specs/ file
	if err != nil {
		return nil, fmt.Errorf("read fact-kind registry %s: %w", path, err)
	}
	var parsed factKindRegistryFullFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse fact-kind registry %s: %w", path, err)
	}
	if len(parsed.Families) == 0 {
		return nil, fmt.Errorf("fact-kind registry %s: no families found", path)
	}
	out := make(map[string]factKindRegistryFullFamily, len(parsed.Families))
	for name, entry := range parsed.Families {
		out[name] = entry
	}
	return out, nil
}

// TestFactKindRegistryReadSurfacesServeConsistentData is the #5474 D1
// route-serves-data gate. It walks every family in
// specs/fact-kind-registry.v1.yaml and asserts that the route named by
// read_surface actually serves data from that family's reducer_domain —
// not merely that the route exists and is mounted (the #5359 gate), but
// that it serves the RIGHT data. This closes the #5480 defect class:
// a route can be live and mounted while serving data from an entirely
// different reducer domain (the kubernetes_live→cloud/resources mis-mapping).
//
// Scope: only the family-level read_surface field. read_surface_overrides
// are out of scope for v1; a family that uses overrides passes as long as
// its family-level route is consistent.
func TestFactKindRegistryReadSurfacesServeConsistentData(t *testing.T) {
	registry, err := loadFactKindRegistryFull(
		filepath.Join(readSurfaceGateSpecsDir(t), "fact-kind-registry.v1.yaml"),
	)
	if err != nil {
		t.Fatalf("loadFactKindRegistryFull: %v", err)
	}

	families := make([]string, 0, len(registry))
	for name := range registry {
		families = append(families, name)
	}
	sort.Strings(families)

	for _, name := range families {
		entry := registry[name]
		rs := strings.TrimSpace(entry.ReadSurface)
		rd := strings.TrimSpace(entry.ReducerDomain)
		if rs == "" || rs == factKindReadSurfaceNone {
			continue // no read surface to check
		}
		if rd == "" {
			t.Errorf("family %q has read_surface %q but no reducer_domain", name, rs)
			continue
		}
		ok, reason := resolveRouteServesData(name, rd, rs)
		if !ok {
			t.Errorf("%s", reason)
		}
	}
}

// TestRouteServesDataBITES_KubernetesLiveCloudResourcesMismatch is the #5474
// D1 BITES proof. It reproduces the #5480 defect class on purpose:
//
//  1. Baseline-green: kubernetes_live's real route
//     ("GET /api/v0/kubernetes/correlations") serves kubernetes_correlation
//     data — the gate PASSES.
//  2. Seeded-RED: re-point kubernetes_live at "GET /api/v0/cloud/resources"
//     — a live, mounted route that serves CloudResource nodes — and assert
//     the gate goes RED with a message naming BOTH fix paths.
//  3. Confirm the production backing map still includes
//     kubernetes_correlation (GREEN stays GREEN).
//
// Follows the baseline-green-then-break pattern from the #5359 BITES
// precedent (go/cmd/api/fact_kind_mounted_route_gate_test.go:59-91).
func TestRouteServesDataBITES_KubernetesLiveCloudResourcesMismatch(t *testing.T) {
	t.Parallel()

	const family = "kubernetes_live"
	const correctRoute = "GET /api/v0/kubernetes/correlations"
	const misroutedRoute = "GET /api/v0/cloud/resources"
	const reducerDomain = "kubernetes_correlation"

	// Phase 1: baseline-green — the real mapping is consistent.
	t.Run("baseline_green", func(t *testing.T) {
		ok, reason := resolveRouteServesData(family, reducerDomain, correctRoute)
		if !ok {
			t.Fatalf("BASELINE BROKEN: %s", reason)
		}
	})

	// Phase 2: seeded-RED — re-point kubernetes_live at cloud/resources.
	t.Run("seeded_red", func(t *testing.T) {
		ok, reason := resolveRouteServesData(family, reducerDomain, misroutedRoute)
		if ok {
			t.Fatalf("BITES FAILED: resolveRouteServesData(%q, %q, %q) returned true — the route serves CloudResource nodes, not kubernetes_correlation facts", family, reducerDomain, misroutedRoute)
		}
		if !substrIn(reason, "read_surface") || !substrIn(reason, "backing map") {
			t.Errorf("RED message does not name both fix paths — got: %s", reason)
		}
	})

	// Phase 3: production stays GREEN — the backing map's actual
	// kubernetes/correlations entry includes kubernetes_correlation.
	t.Run("production_green", func(t *testing.T) {
		backing, known := routeServesDataBackingMap[correctRoute]
		if !known {
			t.Fatalf("routeServesDataBackingMap is missing %q", correctRoute)
		}
		found := false
		for _, d := range backing.ServedDomains {
			if d == reducerDomain {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("routeServesDataBackingMap[%q].ServedDomains does not include %q", correctRoute, reducerDomain)
		}
	})
}

// TestRouteServesDataBackingMapStaleness asserts the forward and reverse
// coverage of routeServesDataBackingMap against the registry.
// Forward: every route used by a family must be in the backing map (fail closed).
// Reverse: every backing map entry must be used by at least one family (no stale entries).
func TestRouteServesDataBackingMapStaleness(t *testing.T) {
	registry, err := loadFactKindRegistryFull(
		filepath.Join(readSurfaceGateSpecsDir(t), "fact-kind-registry.v1.yaml"),
	)
	if err != nil {
		t.Fatalf("loadFactKindRegistryFull: %v", err)
	}

	usedRoutes := map[string]int{}
	for _, entry := range registry {
		rs := strings.TrimSpace(entry.ReadSurface)
		if rs == "" || rs == factKindReadSurfaceNone {
			continue
		}
		usedRoutes[rs]++
	}

	// Forward: every used route must be in the backing map.
	for route := range usedRoutes {
		if _, known := routeServesDataBackingMap[route]; !known {
			t.Errorf("route %q is used by %d families but is not in routeServesDataBackingMap — add it (fail closed)", route, usedRoutes[route])
		}
	}

	// Reverse: every backing-map entry must be used.
	for route := range routeServesDataBackingMap {
		if usedRoutes[route] == 0 {
			t.Errorf("routeServesDataBackingMap has a stale entry for route %q — no family uses this read_surface; remove it", route)
		}
	}
}

func substrIn(s, sub string) bool {
	return len(s) >= len(sub) && indexSubstr(s, sub) >= 0
}

func indexSubstr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestSubstrIn exercises substrIn against the same cases strings.Contains
// documents, since every BITES/RED-message assertion in this package
// (TestKindConsumerExistenceBITES_TeethProof, TestRouteServesDataBITES_*)
// depends on substrIn correctly finding (or correctly failing to find) a
// substring in a failure reason string.
func TestSubstrIn(t *testing.T) {
	tests := []struct {
		name string
		s    string
		sub  string
		want bool
	}{
		{name: "substring_present_start", s: "hello world", sub: "hello", want: true},
		{name: "substring_present_middle", s: "hello world", sub: "lo wo", want: true},
		{name: "substring_present_end", s: "hello world", sub: "world", want: true},
		{name: "substring_absent", s: "hello world", sub: "xyz", want: false},
		{name: "substring_equals_string", s: "hello", sub: "hello", want: true},
		{name: "substring_longer_than_string", s: "hi", sub: "hello", want: false},
		{name: "empty_substring_always_found", s: "hello", sub: "", want: true},
		{name: "both_empty", s: "", sub: "", want: true},
		{name: "empty_string_nonempty_substring", s: "", sub: "x", want: false},
		{name: "case_sensitive_mismatch", s: "Hello World", sub: "hello", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := substrIn(tc.s, tc.sub); got != tc.want {
				t.Errorf("substrIn(%q, %q) = %v, want %v", tc.s, tc.sub, got, tc.want)
			}
		})
	}
}
