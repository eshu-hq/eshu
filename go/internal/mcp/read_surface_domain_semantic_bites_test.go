// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kubernetesLiveCorrectReadSurfaceLine and kubernetesLivePoisonedReadSurfaceLine
// are the exact committed and #5480-defect-shape lines for kubernetes_live's
// family-level read_surface in specs/fact-kind-registry.v1.yaml. Matched as
// whole lines (not a bare substring) so a future registry reformatting that
// changes surrounding whitespace fails this test's setup loudly instead of
// silently mutating zero or the wrong occurrence.
const (
	kubernetesLiveCorrectReadSurfaceLine  = `    read_surface: "GET /api/v0/kubernetes/correlations"`
	kubernetesLivePoisonedReadSurfaceLine = `    read_surface: "GET /api/v0/cloud/resources"`
)

// poisonedFactKindRegistryCopy writes a byte-for-byte copy of the real,
// committed specs/fact-kind-registry.v1.yaml into t.TempDir(), with
// kubernetes_live's read_surface line replaced by
// kubernetesLivePoisonedReadSurfaceLine -- the exact #5398/#5480 historical
// defect shape (kubernetes_live pointed at the live, mounted, but
// semantically wrong "GET /api/v0/cloud/resources" route). Fails the test
// immediately if the target line's occurrence count in the real file is not
// exactly 1, so a future registry edit that changes the line's exact text or
// duplicates it cannot silently turn this BITES proof into a no-op.
func poisonedFactKindRegistryCopy(t *testing.T) string {
	t.Helper()
	realPath := filepath.Join(readSurfaceGateSpecsDir(t), "fact-kind-registry.v1.yaml")
	raw, err := os.ReadFile(realPath) // #nosec G304 -- realPath is the committed repo specs file
	if err != nil {
		t.Fatalf("read %s: %v", realPath, err)
	}
	content := string(raw)
	if got := strings.Count(content, kubernetesLiveCorrectReadSurfaceLine); got != 1 {
		t.Fatalf(
			"test setup invalid: %q occurs %d times in %s, want exactly 1 -- "+
				"the registry's kubernetes_live read_surface line changed shape; update this test's expected line",
			kubernetesLiveCorrectReadSurfaceLine, got, realPath,
		)
	}
	poisoned := strings.Replace(content, kubernetesLiveCorrectReadSurfaceLine, kubernetesLivePoisonedReadSurfaceLine, 1)

	tmpPath := filepath.Join(t.TempDir(), "fact-kind-registry.v1.yaml")
	if err := os.WriteFile(tmpPath, []byte(poisoned), 0o600); err != nil {
		t.Fatalf("write poisoned registry copy %s: %v", tmpPath, err)
	}
	return tmpPath
}

// TestReadSurfaceDomainSemanticGateBITES_RealRegistryFile is the #5480
// end-to-end BITES proof: it reproduces the historical defect by mutating a
// TEMP COPY OF THE REAL, COMMITTED specs/fact-kind-registry.v1.yaml file --
// not by calling resolveFactKindReadSurface/resolveRouteServesData with
// hand-typed literal arguments the way
// TestRouteServesDataBITES_KubernetesLiveCloudResourcesMismatch does. That
// existing BITES test proves the resolver functions themselves are correct in
// isolation; this test additionally proves the full production
// load-the-YAML-then-resolve pipeline (LoadFactKindRegistryReadSurfaces and
// loadFactKindRegistryFull, the exact loaders
// TestFactKindRegistryReadSurfacesResolveToLiveRoutes and
// TestFactKindRegistryReadSurfacesServeConsistentData call) reacts the same
// way when the defect lives in a real file on disk, closing the gap between
// "the resolver is correct" and "the gate that reads the committed registry
// is correct."
func TestReadSurfaceDomainSemanticGateBITES_RealRegistryFile(t *testing.T) {
	const family = "kubernetes_live"
	const reducerDomain = "kubernetes_correlation"
	realPath := filepath.Join(readSurfaceGateSpecsDir(t), "fact-kind-registry.v1.yaml")

	liveRoutes, err := liveImplementedAPIRoutes()
	if err != nil {
		t.Fatalf("liveImplementedAPIRoutes: %v", err)
	}

	t.Run("baseline_green_real_file", func(t *testing.T) {
		oldSurfaces, err := LoadFactKindRegistryReadSurfaces(realPath)
		if err != nil {
			t.Fatalf("LoadFactKindRegistryReadSurfaces(%s): %v", realPath, err)
		}
		if ok, reason := resolveFactKindReadSurface(family, oldSurfaces[family], liveRoutes); !ok {
			t.Fatalf("BASELINE BROKEN (OLD gate, real file): %s", reason)
		}

		newRegistry, err := loadFactKindRegistryFull(realPath)
		if err != nil {
			t.Fatalf("loadFactKindRegistryFull(%s): %v", realPath, err)
		}
		entry, ok := newRegistry[family]
		if !ok {
			t.Fatalf("real registry has no family %q", family)
		}
		if ok, reason := resolveRouteServesData(family, entry.ReducerDomain, entry.ReadSurface); !ok {
			t.Fatalf("BASELINE BROKEN (NEW gate, real file): %s", reason)
		}
	})

	poisonedPath := poisonedFactKindRegistryCopy(t)

	t.Run("old_gate_pipeline_still_passes_on_poisoned_file", func(t *testing.T) {
		// Reproduces the #5480 defect class through the OLD gate's own
		// production loader: a wrong-but-live route must still resolve true,
		// exactly like it silently did before #5398 fixed the registry entry.
		oldSurfaces, err := LoadFactKindRegistryReadSurfaces(poisonedPath)
		if err != nil {
			t.Fatalf("LoadFactKindRegistryReadSurfaces(%s): %v", poisonedPath, err)
		}
		poisonedSurface := oldSurfaces[family]
		if poisonedSurface == "" {
			t.Fatalf("poisoned registry copy: family %q missing from loaded surfaces", family)
		}
		ok, reason := resolveFactKindReadSurface(family, poisonedSurface, liveRoutes)
		if !ok {
			t.Fatalf("BITES SETUP FAILED: OLD gate rejected the poisoned route (%s) -- the seeded route is not actually live, so this is not the #5480 defect shape", reason)
		}
	})

	t.Run("new_gate_pipeline_goes_red_on_poisoned_file", func(t *testing.T) {
		newRegistry, err := loadFactKindRegistryFull(poisonedPath)
		if err != nil {
			t.Fatalf("loadFactKindRegistryFull(%s): %v", poisonedPath, err)
		}
		entry, ok := newRegistry[family]
		if !ok {
			t.Fatalf("poisoned registry copy: family %q missing", family)
		}
		if entry.ReducerDomain != reducerDomain {
			t.Fatalf("poisoned registry copy: family %q reducer_domain = %q, want %q (mutation touched more than the read_surface line)", family, entry.ReducerDomain, reducerDomain)
		}

		gotOK, reason := resolveRouteServesData(family, entry.ReducerDomain, entry.ReadSurface)
		if gotOK {
			t.Fatalf("BITES FAILED: NEW gate resolved true for a route (%s) known to serve a different reducer_domain -- the domain-semantic check did not catch the #5480 misrouting", entry.ReadSurface)
		}
		if !strings.Contains(reason, "read_surface") || !strings.Contains(reason, "backing map") {
			t.Errorf("RED message does not name both fix paths -- got: %s", reason)
		}
	})
}
