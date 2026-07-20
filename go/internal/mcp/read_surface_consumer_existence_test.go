// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// readSurfaceGateSpecsDir resolves the committed specs directory from this
// test file's location (mcp -> internal -> go -> repo root -> specs).
func readSurfaceGateSpecsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "specs"))
}

// TestLanguageParityReadSurfacesResolveToRealConsumers is the #5335 GATE 1
// (language-parity half) CI-enforced consumer-existence check. It is part of
// `go test ./internal/mcp`, riding the same CI floor as every other Go
// package test (test.yml) -- no separate workflow. For every language row in
// specs/language-feature-parity-ledger.v1.yaml, every read_surfaces label
// must resolve through languageParityReadSurfaceBacking to a live MCP tool
// or Go symbol, or be pinned in grandfatheredLanguageParityReadSurfaces at
// the row's current digest. This is the epic's dominant defect class made
// blocking: a language row that claims "content_relationships" or
// "execute_language_query" but backs a tool/symbol that does not exist (a
// typo, a renamed tool, a removed dispatch case) fails here instead of
// shipping as an unverifiable claim.
func TestLanguageParityReadSurfacesResolveToRealConsumers(t *testing.T) {
	ledger, err := replaycoverage.LoadLanguageLedger(filepath.Join(readSurfaceGateSpecsDir(t), replaycoverage.LanguageLedgerFileName))
	if err != nil {
		t.Fatalf("LoadLanguageLedger: %v", err)
	}
	liveMCPTools := liveMCPToolNameSet()
	goSymbolBackings := query.ReadSurfaceGoSymbolBackings

	used := map[string]bool{}
	for _, entry := range ledger.Languages {
		for _, label := range entry.ReadSurfaces {
			used[label] = true
			if ok, reason := resolveLanguageParityReadSurface(label, languageParityReadSurfaceBacking, liveMCPTools, goSymbolBackings); ok {
				continue
			} else if grandfatheredLanguageParityRowOK(entry.Language, label, entry.ReadSurfaces) {
				continue
			} else {
				t.Errorf(
					"language %q read_surfaces claims %q, which does not resolve to a real consumer: %s "+
						"-- either the label is wrong, the backing map (languageParityReadSurfaceBacking) is stale, "+
						"or the tool/symbol it points at was renamed or removed",
					entry.Language, label, reason,
				)
			}
		}
	}

	assertLanguageParityBackingNotStale(t, used)
}

// TestLanguageParityReadSurfaceNoneSentinel locks in the safety semantics of
// the "none" sentinel (#5334): a row may truthfully declare it has no query
// consumer, but "none" must NOT become a way to silence the #5335 gate. The
// invariant is that resolution is PER-LABEL, so "none" can never swallow a
// co-located real (and unbacked) claim — if a future refactor changed the
// per-row loop to skip the whole row when any label is "none", this test goes
// red.
func TestLanguageParityReadSurfaceNoneSentinel(t *testing.T) {
	liveMCPTools := liveMCPToolNameSet()
	goSymbolBackings := query.ReadSurfaceGoSymbolBackings

	const fakeUnbacked = "totally_fake_unbacked_surface"
	if _, claimed := languageParityReadSurfaceBacking[fakeUnbacked]; claimed {
		t.Fatalf("test premise broken: %q must not be a real backing-map entry", fakeUnbacked)
	}

	t.Run("none resolves true", func(t *testing.T) {
		if ok, reason := resolveLanguageParityReadSurface(languageParityReadSurfaceNone, languageParityReadSurfaceBacking, liveMCPTools, goSymbolBackings); !ok {
			t.Fatalf("resolveLanguageParityReadSurface(%q) = false (%s), want true", languageParityReadSurfaceNone, reason)
		}
	})

	t.Run("unbacked label still bites", func(t *testing.T) {
		if ok, _ := resolveLanguageParityReadSurface(fakeUnbacked, languageParityReadSurfaceBacking, liveMCPTools, goSymbolBackings); ok {
			t.Fatalf("resolveLanguageParityReadSurface(%q) = true, want false — the gate must reject an unbacked claim", fakeUnbacked)
		}
	})

	t.Run("none does not swallow a co-located unbacked label", func(t *testing.T) {
		// Mimic the production per-row loop shape from
		// TestLanguageParityReadSurfacesResolveToRealConsumers: each label is
		// resolved independently, so the unbacked label must still be flagged
		// even though "none" precedes it in the same slice.
		row := []string{languageParityReadSurfaceNone, fakeUnbacked}
		var unbackedFlagged bool
		for _, label := range row {
			if ok, _ := resolveLanguageParityReadSurface(label, languageParityReadSurfaceBacking, liveMCPTools, goSymbolBackings); !ok {
				unbackedFlagged = true
			}
		}
		if !unbackedFlagged {
			t.Fatalf("a row of %v resolved with no failures — \"none\" must not swallow the co-located unbacked label %q", row, fakeUnbacked)
		}
	})

	t.Run("a real backed label still resolves true", func(t *testing.T) {
		if ok, reason := resolveLanguageParityReadSurface("content_relationships", languageParityReadSurfaceBacking, liveMCPTools, goSymbolBackings); !ok {
			t.Fatalf("resolveLanguageParityReadSurface(\"content_relationships\") = false (%s), want true", reason)
		}
	})
}

// grandfatheredLanguageParityRowOK reports whether the unresolved
// (language, label) instance is pinned in grandfatheredLanguageParityReadSurfaces
// at the row's current digest. A pinned entry whose digest no longer matches
// (the row was edited) is NOT grandfathered -- editing a grandfathered row
// un-grandfathers it.
func grandfatheredLanguageParityRowOK(language, label string, readSurfaces []string) bool {
	digest, pinned := grandfatheredLanguageParityReadSurfaces[language+":"+label]
	return pinned && digest == languageParityRowDigest(readSurfaces)
}

// assertLanguageParityBackingNotStale is GATE 1's reverse-direction check for
// the language-parity half: every entry in the closed backing map
// (languageParityReadSurfaceBacking) must be referenced by at least one
// ledger row's read_surfaces. An entry no language row uses anymore is dead
// backing-map weight -- the mirror image of assertLedgerNotStale in
// dispatch_scoped_route_exhaustiveness_test.go. Scope note: this checks
// staleness of the nine-label backing map, not the full universe of
// ReadOnlyTools()/served routes -- see the fact-kind-registry-scoped route
// check and the gate's package doc for what GATE 1 does not cover.
func assertLanguageParityBackingNotStale(t *testing.T, used map[string]bool) {
	t.Helper()
	labels := make([]string, 0, len(languageParityReadSurfaceBacking))
	for label := range languageParityReadSurfaceBacking {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		if !used[label] {
			t.Errorf("languageParityReadSurfaceBacking has a stale entry %q -- no language row's read_surfaces uses this label anymore; remove it", label)
		}
	}
}

// TestFactKindRegistryReadSurfacesResolveToLiveRoutes is the #5335 GATE 1
// (fact-kind half) CI-enforced consumer-existence check. Every distinct
// family-level read_surface literal in specs/fact-kind-registry.v1.yaml must
// positionally match a route in the live API route inventory
// (capabilitycatalog.LoadSurfaceInventory, category api_route, readiness
// implemented), or be pinned in grandfatheredFactKindReadSurfaces at its
// current digest. Scope: only the family-level read_surface field --
// read_surface_overrides (per-kind route substitutions, including a
// non-route MCP-tool-shaped override) is out of scope for v1.
func TestFactKindRegistryReadSurfacesResolveToLiveRoutes(t *testing.T) {
	surfaces, err := LoadFactKindRegistryReadSurfaces(filepath.Join(readSurfaceGateSpecsDir(t), "fact-kind-registry.v1.yaml"))
	if err != nil {
		t.Fatalf("loadFactKindRegistryReadSurfaces: %v", err)
	}
	liveRoutes, err := liveImplementedAPIRoutes()
	if err != nil {
		t.Fatalf("liveImplementedAPIRoutes: %v", err)
	}

	families := make([]string, 0, len(surfaces))
	for family := range surfaces {
		families = append(families, family)
	}
	sort.Strings(families)

	for _, family := range families {
		surface := surfaces[family]
		ok, reason := resolveFactKindReadSurface(family, surface, liveRoutes)
		if ok {
			continue
		}
		digest, pinned := grandfatheredFactKindReadSurfaces[family]
		if pinned && digest == factKindRowDigest(surface) {
			continue
		}
		t.Errorf("%s -- either the route moved, the family's read_surface is wrong, or the live route inventory is stale", reason)
	}
}
