// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

func testFactKindEntry(kind, readSurface, payloadSchema string) facts.FactKindRegistryEntry {
	return facts.FactKindRegistryEntry{
		Kind:                   kind,
		LifecycleOwner:         "go/internal/facts",
		ReducerDomain:          "test_domain",
		ProjectionHook:         "test_hook",
		AdmissionHook:          "facts.ValidateSchemaVersion",
		ReadSurface:            readSurface,
		TruthProfile:           facts.FactKindTruthProviderGated,
		PayloadSchema:          payloadSchema,
		ProviderKeyIndependent: false,
	}
}

func testSnapshot() goldengate.Snapshot {
	return goldengate.Snapshot{
		SchemaVersion: "test",
		Graph: goldengate.GraphSnapshot{
			RequiredCorrelations: []goldengate.RequiredCorrelation{
				{
					ID:           "rc-19",
					Relationship: "DEPLOYS_FROM",
					FromLabel:    "Repository",
					ToLabel:      "Repository",
					MinimumCount: 1,
				},
				{
					ID:            "rc-29",
					Relationship:  "DEPLOYS_FROM",
					FromLabel:     "Repository",
					ToLabel:       "Repository",
					MinimumCount:  1,
					EvidenceKinds: []string{"KUSTOMIZE_RESOURCE_REFERENCE"},
				},
			},
		},
	}
}

func testReplayManifest() replaycoverage.Manifest {
	return replaycoverage.Manifest{
		Version: "v1",
		Coverage: []replaycoverage.CoverageEntry{
			{
				Surface:      "read_surface:GET /api/v0/cloud/inventory",
				Scenario:     replaycoverage.ScenarioAPIMCPGolden,
				ScenarioType: replaycoverage.ScenarioTypeBaseline,
				Ref:          "GET /api/v0/cloud/inventory",
				ProofGate:    "golden-corpus-gate",
			},
		},
	}
}

func TestDeriveSchemaLessKindIsRegistryOnly(t *testing.T) {
	t.Parallel()

	entries := []facts.FactKindRegistryEntry{
		testFactKindEntry("aws_tag_observation", "GET /api/v0/cloud/inventory", ""),
	}
	got, err := Derive(entries, testSnapshot(), testReplayManifest())
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(got.Kinds) != 1 {
		t.Fatalf("Kinds = %d, want 1", len(got.Kinds))
	}
	ke := got.Kinds[0]
	if !ke.RegistryOnly {
		t.Errorf("RegistryOnly = false, want true for a schema-less kind")
	}
	if ke.PayloadSchema != "" {
		t.Errorf("PayloadSchema = %q, want empty", ke.PayloadSchema)
	}
}

func TestDeriveSchemaBackedKindCarriesPayloadSchema(t *testing.T) {
	t.Parallel()

	const schema = "sdk/go/factschema/schema/aws_resource.v1.schema.json"
	entries := []facts.FactKindRegistryEntry{
		testFactKindEntry("aws_resource", "GET /api/v0/cloud/inventory", schema),
	}
	got, err := Derive(entries, testSnapshot(), testReplayManifest())
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	ke := got.Kinds[0]
	if ke.RegistryOnly {
		t.Errorf("RegistryOnly = true, want false for a schema-backed kind")
	}
	if ke.PayloadSchema != schema {
		t.Errorf("PayloadSchema = %q, want %q", ke.PayloadSchema, schema)
	}
}

func TestDeriveResolvesQueryRefsFromReplayManifest(t *testing.T) {
	t.Parallel()

	entries := []facts.FactKindRegistryEntry{
		testFactKindEntry("aws_resource", "GET /api/v0/cloud/inventory", "sdk/go/factschema/schema/aws_resource.v1.schema.json"),
	}
	got, err := Derive(entries, testSnapshot(), testReplayManifest())
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	ke := got.Kinds[0]
	if len(ke.QueryRefs) != 1 {
		t.Fatalf("QueryRefs = %d, want 1", len(ke.QueryRefs))
	}
	qr := ke.QueryRefs[0]
	if qr.Scenario != replaycoverage.ScenarioAPIMCPGolden || qr.Ref != "GET /api/v0/cloud/inventory" || qr.ProofGate != "golden-corpus-gate" {
		t.Errorf("QueryRef = %+v, want the replay manifest's read_surface row", qr)
	}
}

func TestDeriveKindWithNoMatchingReplayRowHasNoQueryRefs(t *testing.T) {
	t.Parallel()

	entries := []facts.FactKindRegistryEntry{
		testFactKindEntry("orphan_kind", "GET /api/v0/does-not-exist", ""),
	}
	got, err := Derive(entries, testSnapshot(), testReplayManifest())
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(got.Kinds[0].QueryRefs) != 0 {
		t.Errorf("QueryRefs = %v, want empty for a read_surface with no replay manifest row", got.Kinds[0].QueryRefs)
	}
}

func TestDeriveNoneOrBlankReadSurfaceIsExcludedFromQueryDerivation(t *testing.T) {
	t.Parallel()

	entries := []facts.FactKindRegistryEntry{
		testFactKindEntry("kind_a", "", ""),
		testFactKindEntry("kind_b", "none", ""),
		testFactKindEntry("kind_c", "  none  ", ""),
	}
	got, err := Derive(entries, testSnapshot(), testReplayManifest())
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	for _, ke := range got.Kinds {
		if ke.ReadSurface != "" {
			t.Errorf("kind %q ReadSurface = %q, want normalized to empty", ke.Kind, ke.ReadSurface)
		}
		if len(ke.QueryRefs) != 0 {
			t.Errorf("kind %q QueryRefs = %v, want empty", ke.Kind, ke.QueryRefs)
		}
	}
}

func TestDeriveNarrowedCorrelationsOnlyIncludeEvidenceKindRows(t *testing.T) {
	t.Parallel()

	got, err := Derive(nil, testSnapshot(), testReplayManifest())
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(got.NarrowedCorrelations) != 1 {
		t.Fatalf("NarrowedCorrelations = %d, want 1", len(got.NarrowedCorrelations))
	}
	if got.NarrowedCorrelations[0].ID != "rc-29" {
		t.Errorf("NarrowedCorrelations[0].ID = %q, want rc-29 (rc-19 has no evidence_kinds and must be excluded)", got.NarrowedCorrelations[0].ID)
	}
}

func TestDeriveIsDeterministicallySorted(t *testing.T) {
	t.Parallel()

	entries := []facts.FactKindRegistryEntry{
		testFactKindEntry("zzz_kind", "", ""),
		testFactKindEntry("aaa_kind", "", ""),
		testFactKindEntry("mmm_kind", "", ""),
	}
	got, err := Derive(entries, testSnapshot(), testReplayManifest())
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(got.Kinds) != 3 {
		t.Fatalf("Kinds = %d, want 3", len(got.Kinds))
	}
	if got.Kinds[0].Kind != "aaa_kind" || got.Kinds[1].Kind != "mmm_kind" || got.Kinds[2].Kind != "zzz_kind" {
		t.Errorf("Kinds not sorted: %v", []string{got.Kinds[0].Kind, got.Kinds[1].Kind, got.Kinds[2].Kind})
	}
}
