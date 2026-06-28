// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"strings"
	"testing"
)

// minimalLive is a small live-surface set used across the surface tests.
func minimalLive() LiveSurfaces {
	return LiveSurfaces{Surfaces: map[SurfaceCategory][]string{
		SurfaceCommand:       {"eshu", "api"},
		SurfaceCollector:     {"git", "kubernetes_live"},
		SurfaceReducerDomain: {"workload_identity"},
		SurfaceAPIRoute:      {"GET /api/v0/capabilities"},
		SurfaceMCPTool:       {"find_code"},
		SurfaceConsolePage:   {"CapabilityMatrixPage"},
	}, CollectorFactKinds: map[string][]string{
		"git":             {"documentation_document", "documentation_section"},
		"kubernetes_live": {"kubernetes_live.pod_template"},
	}}
}

func cleanOverlay() SurfaceOverlay {
	return SurfaceOverlay{Version: "v1", Surfaces: []SurfaceOverlayRecord{
		{
			Category:  SurfaceCollector,
			Name:      "git",
			Readiness: ReadinessImplemented,
			Owner:     "internal/collector",
			Proof:     "docs/public/reference/collector-reducer-readiness.md#promotion-proof",
			CollectorContract: CollectorContract{
				FactKinds:          []string{"documentation_document", "documentation_section"},
				ProjectionSurfaces: []string{"content_projection", "documentation_evidence"},
				ReadSurfaces:       []string{"GET /api/v0/documentation/facts", "search_documentation"},
				ProofGates:         []string{"go test ./internal/collector ./internal/query -count=1"},
				FixtureRefs:        []string{"go/internal/collector/testdata"},
				TruthProfile:       "deterministic",
			},
		},
		{
			Category:  SurfaceCollector,
			Name:      "kubernetes_live",
			Readiness: ReadinessFoundationOnly,
			Owner:     "internal/collector/kuberneteslive",
			CollectorContract: CollectorContract{
				FactKinds:          []string{"kubernetes_live.pod_template"},
				ProjectionSurfaces: []string{"kubernetes_correlation"},
				ReadSurfaces:       []string{"list_kubernetes_correlations"},
				ProofGates:         []string{"go test ./internal/collector/kuberneteslive ./internal/reducer -count=1"},
				FixtureRefs:        []string{"go/internal/collector/kuberneteslive/testdata"},
				TruthProfile:       "deterministic",
			},
		},
	}}
}

func TestBuildSurfaceInventoryCleanHasNoFindings(t *testing.T) {
	t.Parallel()
	inv, findings := BuildSurfaceInventory(minimalLive(), cleanOverlay())
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d: %+v", len(findings), findings)
	}
	// Every live surface becomes exactly one record.
	wantCount := 2 + 2 + 1 + 1 + 1 + 1
	if len(inv.Surfaces) != wantCount {
		t.Fatalf("inventory record count = %d, want %d", len(inv.Surfaces), wantCount)
	}
	// Records are sorted by category then name for determinism.
	for i := 1; i < len(inv.Surfaces); i++ {
		prev, cur := inv.Surfaces[i-1], inv.Surfaces[i]
		if prev.Category > cur.Category || (prev.Category == cur.Category && prev.Name > cur.Name) {
			t.Fatalf("records not sorted at %d: %v then %v", i, prev, cur)
		}
	}
}

func TestBuildSurfaceInventoryAppliesOverlayLane(t *testing.T) {
	t.Parallel()
	inv, _ := BuildSurfaceInventory(minimalLive(), cleanOverlay())
	k8s := findRecord(t, inv, SurfaceCollector, "kubernetes_live")
	if k8s.Readiness != ReadinessFoundationOnly {
		t.Errorf("kubernetes_live readiness = %q, want foundation_only", k8s.Readiness)
	}
}

func TestBuildSurfaceInventoryCarriesCollectorContract(t *testing.T) {
	t.Parallel()
	inv, findings := BuildSurfaceInventory(minimalLive(), cleanOverlay())
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	git := findRecord(t, inv, SurfaceCollector, "git")
	if git.CollectorContract == nil {
		t.Fatal("git CollectorContract = nil, want manifest contract")
	}
	if got, want := strings.Join(git.CollectorContract.FactKinds, ","), "documentation_document,documentation_section"; got != want {
		t.Fatalf("git fact kinds = %q, want %q", got, want)
	}
	if got, want := git.CollectorContract.TruthProfile, "deterministic"; got != want {
		t.Fatalf("git truth profile = %q, want %q", got, want)
	}
}

func TestBuildSurfaceInventoryDefaultsNonCollectorToImplemented(t *testing.T) {
	t.Parallel()
	inv, findings := BuildSurfaceInventory(minimalLive(), cleanOverlay())
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	cmd := findRecord(t, inv, SurfaceCommand, "eshu")
	if cmd.Readiness != ReadinessImplemented {
		t.Errorf("command default readiness = %q, want implemented", cmd.Readiness)
	}
}

func TestBuildSurfaceInventoryFlagsUnclassifiedCollector(t *testing.T) {
	t.Parallel()
	// Overlay omits kubernetes_live, so it has no declared lane.
	overlay := SurfaceOverlay{Version: "v1", Surfaces: []SurfaceOverlayRecord{
		{Category: SurfaceCollector, Name: "git", Readiness: ReadinessImplemented, Proof: "x"},
	}}
	_, findings := BuildSurfaceInventory(minimalLive(), overlay)
	if !hasFinding(findings, FindingUnclassifiedCollector, "kubernetes_live") {
		t.Fatalf("expected unclassified_collector finding for kubernetes_live, got %+v", findings)
	}
}

func TestBuildSurfaceInventoryFlagsStaleOverlay(t *testing.T) {
	t.Parallel()
	overlay := cleanOverlay()
	overlay.Surfaces = append(overlay.Surfaces, SurfaceOverlayRecord{
		Category: SurfaceCollector, Name: "ghost_collector", Readiness: ReadinessImplemented, Proof: "x",
	})
	_, findings := BuildSurfaceInventory(minimalLive(), overlay)
	if !hasFinding(findings, FindingStaleSurfaceOverlay, "ghost_collector") {
		t.Fatalf("expected stale_surface_overlay finding for ghost_collector, got %+v", findings)
	}
}

func TestBuildSurfaceInventoryFlagsImplementedCollectorWithoutProof(t *testing.T) {
	t.Parallel()
	overlay := SurfaceOverlay{Version: "v1", Surfaces: []SurfaceOverlayRecord{
		{Category: SurfaceCollector, Name: "git", Readiness: ReadinessImplemented}, // no Proof
		{Category: SurfaceCollector, Name: "kubernetes_live", Readiness: ReadinessFoundationOnly},
	}}
	_, findings := BuildSurfaceInventory(minimalLive(), overlay)
	if !hasFinding(findings, FindingImplementedWithoutProof, "git") {
		t.Fatalf("expected implemented_without_proof finding for git, got %+v", findings)
	}
}

func TestBuildSurfaceInventoryFlagsInvalidLane(t *testing.T) {
	t.Parallel()
	overlay := cleanOverlay()
	overlay.Surfaces[0].Readiness = "production_ready"
	_, findings := BuildSurfaceInventory(minimalLive(), overlay)
	if !hasFinding(findings, FindingInvalidReadinessLane, "git") {
		t.Fatalf("expected invalid_readiness_lane finding for git, got %+v", findings)
	}
}

func TestBuildSurfaceInventoryFlagsDuplicateOverlayRow(t *testing.T) {
	t.Parallel()
	overlay := cleanOverlay()
	// A second row for git with a different (downgraded) lane must be flagged
	// rather than silently winning.
	overlay.Surfaces = append(overlay.Surfaces, SurfaceOverlayRecord{
		Category: SurfaceCollector, Name: "git", Readiness: ReadinessFoundationOnly,
	})
	_, findings := BuildSurfaceInventory(minimalLive(), overlay)
	if !hasFinding(findings, FindingDuplicateOverlayRow, "git") {
		t.Fatalf("expected duplicate_overlay_row finding for git, got %+v", findings)
	}
}

func TestBuildSurfaceInventoryFlagsUnmappedCollectorFactKind(t *testing.T) {
	t.Parallel()
	live := minimalLive()
	live.CollectorFactKinds["git"] = append(live.CollectorFactKinds["git"], "documentation_new_fact")

	_, findings := BuildSurfaceInventory(live, cleanOverlay())
	if !hasFinding(findings, FindingCollectorFactKindUnmapped, "git:documentation_new_fact") {
		t.Fatalf("expected collector_fact_kind_unmapped finding for new git fact kind, got %+v", findings)
	}
}

func TestMarshalSurfaceInventoryDeterministic(t *testing.T) {
	t.Parallel()
	inv, _ := BuildSurfaceInventory(minimalLive(), cleanOverlay())
	a, err := MarshalSurfaceInventory(inv)
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalSurfaceInventory(inv)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("MarshalSurfaceInventory output not deterministic")
	}
	if !strings.HasSuffix(string(a), "\n") {
		t.Error("artifact should end with a trailing newline")
	}
}

func findRecord(t *testing.T, inv SurfaceInventory, cat SurfaceCategory, name string) SurfaceRecord {
	t.Helper()
	for _, r := range inv.Surfaces {
		if r.Category == cat && r.Name == name {
			return r
		}
	}
	t.Fatalf("record %s/%s not found", cat, name)
	return SurfaceRecord{}
}

func hasFinding(findings []Finding, kind FindingKind, subject string) bool {
	for _, f := range findings {
		if f.Kind == kind && f.Subject == subject {
			return true
		}
	}
	return false
}
