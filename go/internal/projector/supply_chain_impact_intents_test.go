// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesSupplyChainImpactForVulnerabilityEvidence(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "vuln-intel://osv/maven/log4j-core",
		SourceSystem: "vulnerability_intelligence",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-1",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{{
		FactID:        "fact-cve",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.VulnerabilityCVEFactKind,
		SchemaVersion: facts.VulnerabilityIntelligenceSchemaVersionV1,
		SourceRef: facts.Ref{
			SourceSystem: "vulnerability_intelligence",
		},
	}})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSupplyChainImpactIntent(t, projection.reducerIntents)
	if intent.ScopeID != scopeValue.ScopeID {
		t.Fatalf("ScopeID = %q, want %q", intent.ScopeID, scopeValue.ScopeID)
	}
	if intent.GenerationID != generation.GenerationID {
		t.Fatalf("GenerationID = %q, want %q", intent.GenerationID, generation.GenerationID)
	}
	if intent.FactID != "fact-cve" {
		t.Fatalf("FactID = %q, want fact-cve", intent.FactID)
	}
	if intent.SourceSystem != "vulnerability_intelligence" {
		t.Fatalf("SourceSystem = %q, want vulnerability_intelligence", intent.SourceSystem)
	}
}

func TestBuildProjectionQueuesSupplyChainImpactForPackageIdentityEvidence(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "package-registry:npm:vite",
		SourceSystem: "package_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-1",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		packageIdentityEnvelope("fact-package-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSupplyChainImpactIntent(t, projection.reducerIntents)
	if got, want := intent.FactID, "fact-package-1"; got != want {
		t.Fatalf("FactID = %q, want %q", got, want)
	}
	if got, want := intent.Reason, "package registry identity observed"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "package_registry"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesSupplyChainImpactForSBOMComponentEvidence(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "sbom://registry.example.com/team/api",
		SourceSystem: "sbom_attestation",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-sbom",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{{
		FactID:        "fact-sbom-component",
		ScopeID:       scopeValue.ScopeID,
		GenerationID:  generation.GenerationID,
		FactKind:      facts.SBOMComponentFactKind,
		SchemaVersion: facts.SBOMAttestationSchemaVersionV1,
		Payload: map[string]any{
			"document_id": "doc-1",
			"purl":        "pkg:npm/example@1.2.3",
		},
		SourceRef: facts.Ref{
			SourceSystem: "sbom_attestation",
		},
	}})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSupplyChainImpactIntent(t, projection.reducerIntents)
	if got, want := intent.FactID, "fact-sbom-component"; got != want {
		t.Fatalf("FactID = %q, want %q", got, want)
	}
	if got, want := intent.Reason, "SBOM package evidence observed"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "sbom_attestation"; got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesSupplyChainImpactForOCIReferrerEvidence(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "oci-registry://registry.example.com/team/api",
		SourceSystem: "oci_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-oci",
	}
	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{
		ociRegistryReferrerEnvelope("fact-oci-referrer-1", scopeValue.ScopeID, generation.GenerationID),
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	intent := requireSupplyChainImpactIntent(t, projection.reducerIntents)
	if got, want := intent.FactID, "fact-oci-referrer-1"; got != want {
		t.Fatalf("FactID = %q, want %q", got, want)
	}
	if got, want := intent.Reason, "OCI image subject evidence observed"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func TestBuildSupplyChainImpactReducerIntentSkipsSnapshotOnlyEvidence(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{ScopeID: "vuln-intel://first/epss"}
	generation := scope.ScopeGeneration{ScopeID: scopeValue.ScopeID, GenerationID: "generation-1"}
	_, ok := buildSupplyChainImpactReducerIntent(scopeValue, generation, []facts.Envelope{{
		FactID:       "snapshot",
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		FactKind:     facts.VulnerabilitySourceSnapshotFactKind,
	}})
	if ok {
		t.Fatal("buildSupplyChainImpactReducerIntent() ok = true, want false for source snapshot only")
	}
}

func requireSupplyChainImpactIntent(t *testing.T, intents []ReducerIntent) ReducerIntent {
	t.Helper()
	for _, intent := range intents {
		if intent.Domain == reducer.DomainSupplyChainImpact {
			return intent
		}
	}
	t.Fatalf("supply_chain_impact intent missing from %#v", intents)
	return ReducerIntent{}
}
