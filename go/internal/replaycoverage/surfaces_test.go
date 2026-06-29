// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testInputs() ([]SupportedSurface, map[string]SupportedSurface) {
	inv := capabilitycatalog.SurfaceInventory{
		Version: "v1",
		Surfaces: []capabilitycatalog.SurfaceRecord{
			{Category: capabilitycatalog.SurfaceCollector, Name: "aws", Readiness: capabilitycatalog.ReadinessImplemented},
			// gated collector must NOT be enumerated: only the implemented lane is required.
			{Category: capabilitycatalog.SurfaceCollector, Name: "azure", Readiness: capabilitycatalog.ReadinessGated},
			// non-collector implemented surfaces are not collector replay targets.
			{Category: capabilitycatalog.SurfaceAPIRoute, Name: "GET /api/v0/repos", Readiness: capabilitycatalog.ReadinessImplemented},
		},
	}
	factKinds := []facts.FactKindRegistryEntry{
		{Kind: "aws_resource", ReadSurface: "GET /api/v0/cloud/inventory"},
		{Kind: "azure_cloud_resource", ReadSurface: "GET /api/v0/cloud/inventory"}, // duplicate read_surface deduped
		{Kind: "ci.run", ReadSurface: "GET /api/v0/ci-cd/run-correlations"},
		{Kind: "blank_surface", ReadSurface: ""}, // blank read_surface skipped
	}
	ledger := ParserLedger{Version: 1, Parsers: []ParserLedgerEntry{{Parser: "hcl"}, {Parser: "dockerfile"}}}
	matrix := capabilitycatalog.Matrix{Capabilities: []capabilitycatalog.MatrixCapability{
		{Capability: "cap.supported", Profiles: map[string]capabilitycatalog.MatrixProfile{
			"local": {Status: "supported"},
		}},
		{Capability: "cap.unsupported_only", Profiles: map[string]capabilitycatalog.MatrixProfile{
			"local": {Status: "unsupported"},
			"full":  {Status: "not_implemented"},
		}},
		{Capability: "cap.experimental", Profiles: map[string]capabilitycatalog.MatrixProfile{
			"local": {Status: "experimental"},
		}},
		// Truth-ceiling-only row: a blank status with a non-unsupported ceiling
		// resolves to supported under the canonical resolver, so it IS a claim that
		// needs a scenario. A naive raw-status check would silently drop it.
		{Capability: "cap.ceiling_only", Profiles: map[string]capabilitycatalog.MatrixProfile{
			"local": {MaxTruthLevel: "exact"},
		}},
	}}

	got := EnumerateSupported(inv, factKinds, ledger, matrix)
	byKey := map[string]SupportedSurface{}
	for _, s := range got {
		byKey[s.Key] = s
	}
	return got, byKey
}

func TestEnumerateSupportedKeys(t *testing.T) {
	_, byKey := testInputs()

	wantPresent := map[string]Registry{
		"collector:aws": RegistrySurfaceInventory,
		"read_surface:GET /api/v0/cloud/inventory":        RegistryFactKind,
		"read_surface:GET /api/v0/ci-cd/run-correlations": RegistryFactKind,
		"parser:hcl":                  RegistryParserLedger,
		"parser:dockerfile":           RegistryParserLedger,
		"capability:cap.supported":    RegistryCapabilityMatrix,
		"capability:cap.experimental": RegistryCapabilityMatrix,
		"capability:cap.ceiling_only": RegistryCapabilityMatrix,
	}
	for key, reg := range wantPresent {
		s, ok := byKey[key]
		if !ok {
			t.Errorf("missing required surface %q", key)
			continue
		}
		if s.Registry != reg {
			t.Errorf("%q registry = %q, want %q", key, s.Registry, reg)
		}
	}

	wantAbsent := []string{
		"collector:azure",                 // gated lane, not implemented
		"read_surface:",                   // blank read_surface skipped
		"capability:cap.unsupported_only", // no positive profile claim
	}
	for _, key := range wantAbsent {
		if _, ok := byKey[key]; ok {
			t.Errorf("surface %q must not be enumerated", key)
		}
	}
}

func TestEnumerateSupportedDeterministicAndDeduped(t *testing.T) {
	got, _ := testInputs()
	// One read_surface appears twice in the fact registry but must enumerate once.
	count := 0
	for _, s := range got {
		if s.Key == "read_surface:GET /api/v0/cloud/inventory" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("deduped read_surface count = %d, want 1", count)
	}
	// Output is sorted by registry then key for deterministic gate output.
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1], got[i]
		if prev.Registry > cur.Registry || (prev.Registry == cur.Registry && prev.Key > cur.Key) {
			t.Errorf("output not sorted at %d: %q/%q before %q/%q", i, prev.Registry, prev.Key, cur.Registry, cur.Key)
		}
	}
}
