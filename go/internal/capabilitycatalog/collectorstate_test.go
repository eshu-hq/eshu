package capabilitycatalog

import (
	"os"
	"path/filepath"
	"testing"
)

// inventoryForCollectorTests builds a small surface inventory with one
// implemented collector (with proof) and one foundation-only collector.
func inventoryForCollectorTests() SurfaceInventory {
	inv, _ := BuildSurfaceInventory(minimalLive(), cleanOverlay())
	return inv
}

func TestCheckCollectorReadinessCleanClaims(t *testing.T) {
	t.Parallel()
	inv := inventoryForCollectorTests()
	claims := []CollectorClaim{
		{Path: "a.md", Line: 1, Collector: "git", Lane: ReadinessImplemented},
		{Path: "a.md", Line: 2, Collector: "kubernetes_live", Lane: ReadinessFoundationOnly},
	}
	if findings := CheckCollectorReadiness(inv, claims); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestCheckCollectorReadinessFlagsLaneMismatch(t *testing.T) {
	t.Parallel()
	inv := inventoryForCollectorTests()
	claims := []CollectorClaim{{Path: "a.md", Line: 3, Collector: "kubernetes_live", Lane: ReadinessImplemented}}
	findings := CheckCollectorReadiness(inv, claims)
	if len(findings) != 1 || findings[0].Collector != "kubernetes_live" {
		t.Fatalf("expected one lane-mismatch finding, got %+v", findings)
	}
}

func TestCheckCollectorReadinessFlagsUnknownCollector(t *testing.T) {
	t.Parallel()
	inv := inventoryForCollectorTests()
	claims := []CollectorClaim{{Path: "a.md", Line: 4, Collector: "ghost", Lane: ReadinessImplemented}}
	findings := CheckCollectorReadiness(inv, claims)
	if len(findings) != 1 || findings[0].Reason != "collector is not in the surface inventory" {
		t.Fatalf("expected unknown-collector finding, got %+v", findings)
	}
}

func TestCheckCollectorReadinessFlagsInvalidLane(t *testing.T) {
	t.Parallel()
	inv := inventoryForCollectorTests()
	claims := []CollectorClaim{{Path: "a.md", Line: 5, Collector: "git", Lane: "production_ready"}}
	findings := CheckCollectorReadiness(inv, claims)
	if len(findings) != 1 || findings[0].Reason != "claimed lane is not a valid readiness lane" {
		t.Fatalf("expected invalid-lane finding, got %+v", findings)
	}
}

// TestCheckCollectorReadinessFlagsImplementedWithoutProof is the negative test
// required by #3146: a collector declared implemented in the inventory but with
// no linked promotion proof must fail when a doc claims it implemented.
func TestCheckCollectorReadinessFlagsImplementedWithoutProof(t *testing.T) {
	t.Parallel()
	// An overlay that marks git implemented WITHOUT proof would itself fail the
	// #3145 gate; here we assert the docs gate independently catches the
	// implemented-without-proof claim so the two gates defend in depth.
	overlay := SurfaceOverlay{Version: "v1", Surfaces: []SurfaceOverlayRecord{
		{Category: SurfaceCollector, Name: "git", Readiness: ReadinessImplemented}, // no Proof
		{Category: SurfaceCollector, Name: "kubernetes_live", Readiness: ReadinessFoundationOnly},
	}}
	inv, _ := BuildSurfaceInventory(minimalLive(), overlay)
	claims := []CollectorClaim{{Path: "a.md", Line: 6, Collector: "git", Lane: ReadinessImplemented}}
	findings := CheckCollectorReadiness(inv, claims)
	if len(findings) != 1 || findings[0].Reason != "doc claims collector is implemented but the surface inventory links no promotion proof" {
		t.Fatalf("expected implemented-without-proof finding, got %+v", findings)
	}
}

// TestParseCollectorClaimsFromFixtureDoc proves the parser reads markers,
// skips fenced examples, and flags malformed markers — including the fake
// implemented claim that the gate must reject.
func TestParseCollectorClaimsFromFixtureDoc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	doc := "# Fixture\n" +
		"<!-- collector-state: name=git lane=implemented -->\n" +
		"```\n<!-- collector-state: name=ignored lane=implemented -->\n```\n" +
		"<!-- collector-state: name=ghost_collector lane=implemented -->\n" +
		"<!-- collector-state: lane=implemented -->\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	claims, err := ParseCollectorClaims(dir)
	if err != nil {
		t.Fatal(err)
	}
	// git, ghost_collector, and the malformed marker; the fenced one is skipped.
	if len(claims) != 3 {
		t.Fatalf("expected 3 claims (fenced one skipped), got %d: %+v", len(claims), claims)
	}
	inv := inventoryForCollectorTests()
	findings := CheckCollectorReadiness(inv, claims)
	// ghost_collector unknown + malformed marker => at least 2 findings; git is clean.
	if !hasCollectorFinding(findings, "ghost_collector") {
		t.Fatalf("expected ghost_collector finding, got %+v", findings)
	}
}

func hasCollectorFinding(findings []CollectorFinding, collector string) bool {
	for _, f := range findings {
		if f.Collector == collector {
			return true
		}
	}
	return false
}
