// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func attestationSLSAProvenanceFact(
	factID string,
	statementID string,
	predicateType string,
	builderID string,
) facts.Envelope {
	payload := map[string]any{
		"statement_id":   statementID,
		"predicate_type": predicateType,
	}
	if builderID != "" {
		payload["builder_id"] = builderID
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AttestationSLSAProvenanceFactKind,
		Payload:  payload,
	}
}

func TestBuildSBOMAttestationAttachmentDecisionsSurfacesSLSAProvenance(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		attestationStatementFact("statement-slsa", "stmt-slsa", testSBOMSubjectDigest, "sha256:8888888888888888888888888888888888888888888888888888888888888888", "parsed", "verified"),
		attestationSLSAProvenanceFact("provenance-slsa", "stmt-slsa", "https://slsa.dev/provenance/v1", "https://github.com/actions/runner/v1"),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	decision, ok := got["stmt-slsa"]
	if !ok {
		t.Fatalf("no decision for stmt-slsa: %#v", got)
	}
	if decision.SLSAProvenancePredicateType != "https://slsa.dev/provenance/v1" {
		t.Fatalf("SLSAProvenancePredicateType = %q, want https://slsa.dev/provenance/v1", decision.SLSAProvenancePredicateType)
	}
	if decision.SLSAProvenanceBuilderID != "https://github.com/actions/runner/v1" {
		t.Fatalf("SLSAProvenanceBuilderID = %q, want https://github.com/actions/runner/v1", decision.SLSAProvenanceBuilderID)
	}
	found := false
	for _, factID := range decision.EvidenceFactIDs {
		if factID == "provenance-slsa" {
			found = true
		}
	}
	if !found {
		t.Fatalf("EvidenceFactIDs = %#v, want to include provenance-slsa", decision.EvidenceFactIDs)
	}
}

func TestBuildSBOMAttestationAttachmentDecisionsSLSAProvenanceAbsentLeavesFieldsEmpty(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		attestationStatementFact("statement-no-slsa", "stmt-no-slsa", testSBOMSubjectDigest, "sha256:9999999999999999999999999999999999999999999999999999999999999999", "parsed", "verified"),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	decision, ok := got["stmt-no-slsa"]
	if !ok {
		t.Fatalf("no decision for stmt-no-slsa: %#v", got)
	}
	if decision.SLSAProvenancePredicateType != "" {
		t.Fatalf("SLSAProvenancePredicateType = %q, want empty when no attestation.slsa_provenance fact joins", decision.SLSAProvenancePredicateType)
	}
	if decision.SLSAProvenanceBuilderID != "" {
		t.Fatalf("SLSAProvenanceBuilderID = %q, want empty when no attestation.slsa_provenance fact joins", decision.SLSAProvenanceBuilderID)
	}
}

func TestBuildSBOMAttestationAttachmentDecisionsSLSAProvenanceDuplicateStatementIDKeepsSmallestFactID(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		attestationStatementFact("statement-dup", "stmt-dup", testSBOMSubjectDigest, "sha256:0000000000000000000000000000000000000000000000000000000000000000", "parsed", "verified"),
		attestationSLSAProvenanceFact("provenance-zzz", "stmt-dup", "https://slsa.dev/provenance/v1", "https://example.com/builder-zzz"),
		attestationSLSAProvenanceFact("provenance-aaa", "stmt-dup", "https://slsa.dev/provenance/v0.2", "https://example.com/builder-aaa"),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	decision, ok := got["stmt-dup"]
	if !ok {
		t.Fatalf("no decision for stmt-dup: %#v", got)
	}
	if decision.SLSAProvenanceBuilderID != "https://example.com/builder-aaa" {
		t.Fatalf("SLSAProvenanceBuilderID = %q, want the lexicographically smallest factID's builder_id (provenance-aaa)", decision.SLSAProvenanceBuilderID)
	}
	if decision.SLSAProvenancePredicateType != "https://slsa.dev/provenance/v0.2" {
		t.Fatalf("SLSAProvenancePredicateType = %q, want the lexicographically smallest factID's predicate_type (provenance-aaa)", decision.SLSAProvenancePredicateType)
	}
}

// attestationSLSAProvenanceFactWithMaterials extends attestationSLSAProvenanceFact
// with the #5456 materials/config_source fields, using the same raw wire-shape
// keys the SBOM runtime collector emits (go/internal/collector/sbomruntime/attestation.go).
func attestationSLSAProvenanceFactWithMaterials(
	factID string,
	statementID string,
	predicateType string,
	builderID string,
	materials []map[string]any,
	configSource map[string]any,
) facts.Envelope {
	payload := map[string]any{
		"statement_id":   statementID,
		"predicate_type": predicateType,
	}
	if builderID != "" {
		payload["builder_id"] = builderID
	}
	if len(materials) > 0 {
		payload["materials"] = materials
	}
	if configSource != nil {
		payload["config_source"] = configSource
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AttestationSLSAProvenanceFactKind,
		Payload:  payload,
	}
}

// TestBuildSBOMAttestationAttachmentDecisionsSurfacesSLSAMaterialsAndConfigSource
// is the #5456 regression: the reducer must decode and surface the retained
// materials[]/config_source predicate fields on the attachment decision, not
// only predicate_type/builder_id.
func TestBuildSBOMAttestationAttachmentDecisionsSurfacesSLSAMaterialsAndConfigSource(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		attestationStatementFact("statement-slsa-materials", "stmt-slsa-materials", testSBOMSubjectDigest, "sha256:7777777777777777777777777777777777777777777777777777777777777777", "parsed", "verified"),
		attestationSLSAProvenanceFactWithMaterials(
			"provenance-slsa-materials",
			"stmt-slsa-materials",
			"https://slsa.dev/provenance/v1",
			"https://github.com/actions/runner/v1",
			[]map[string]any{
				{"uri": "git+https://github.com/acme/app@refs/heads/main", "digest": map[string]string{"sha1": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
			},
			map[string]any{
				"uri":         "git+https://github.com/acme/app@refs/heads/main",
				"digest":      map[string]string{"sha1": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
				"entry_point": ".github/workflows/release.yml",
			},
		),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	decision, ok := got["stmt-slsa-materials"]
	if !ok {
		t.Fatalf("no decision for stmt-slsa-materials: %#v", got)
	}
	if len(decision.SLSAProvenanceMaterials) != 1 {
		t.Fatalf("SLSAProvenanceMaterials = %#v, want one row", decision.SLSAProvenanceMaterials)
	}
	if got := decision.SLSAProvenanceMaterials[0]["uri"]; got != "git+https://github.com/acme/app@refs/heads/main" {
		t.Fatalf("materials[0].uri = %v, want git URI", got)
	}
	if decision.SLSAProvenanceMaterialCount != 1 {
		t.Fatalf("SLSAProvenanceMaterialCount = %d, want 1", decision.SLSAProvenanceMaterialCount)
	}
	if decision.SLSAProvenanceMaterialsTruncated {
		t.Fatal("SLSAProvenanceMaterialsTruncated = true, want false")
	}
	if decision.SLSAProvenanceConfigSourceURI != "git+https://github.com/acme/app@refs/heads/main" {
		t.Fatalf("SLSAProvenanceConfigSourceURI = %q", decision.SLSAProvenanceConfigSourceURI)
	}
	if decision.SLSAProvenanceConfigSourceEntryPoint != ".github/workflows/release.yml" {
		t.Fatalf("SLSAProvenanceConfigSourceEntryPoint = %q", decision.SLSAProvenanceConfigSourceEntryPoint)
	}
	if decision.SLSAProvenanceConfigSourceDigest["sha1"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("SLSAProvenanceConfigSourceDigest = %#v, want sha1 digest", decision.SLSAProvenanceConfigSourceDigest)
	}
}

// TestBuildSBOMAttestationAttachmentDecisionsCapsSLSAMaterialRows proves the
// write-time cap: more materials than maxSBOMAttachmentSLSAMaterialRows still
// report the full count via SLSAProvenanceMaterialCount and set
// SLSAProvenanceMaterialsTruncated, mirroring dependencyRelationshipEvidenceRows.
func TestBuildSBOMAttestationAttachmentDecisionsCapsSLSAMaterialRows(t *testing.T) {
	t.Parallel()

	materials := make([]map[string]any, 0, maxSBOMAttachmentSLSAMaterialRows+1)
	for i := 0; i < maxSBOMAttachmentSLSAMaterialRows+1; i++ {
		materials = append(materials, map[string]any{
			"uri": fmt.Sprintf("git+https://github.com/acme/dep-%02d@refs/heads/main", i),
		})
	}

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		attestationStatementFact("statement-slsa-cap", "stmt-slsa-cap", testSBOMSubjectDigest, "sha256:6666666666666666666666666666666666666666666666666666666666666666", "parsed", "verified"),
		attestationSLSAProvenanceFactWithMaterials(
			"provenance-slsa-cap", "stmt-slsa-cap", "https://slsa.dev/provenance/v1", "", materials, nil,
		),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	decision, ok := got["stmt-slsa-cap"]
	if !ok {
		t.Fatalf("no decision for stmt-slsa-cap: %#v", got)
	}
	if decision.SLSAProvenanceMaterialCount != maxSBOMAttachmentSLSAMaterialRows+1 {
		t.Fatalf("SLSAProvenanceMaterialCount = %d, want %d", decision.SLSAProvenanceMaterialCount, maxSBOMAttachmentSLSAMaterialRows+1)
	}
	if len(decision.SLSAProvenanceMaterials) != maxSBOMAttachmentSLSAMaterialRows {
		t.Fatalf("len(SLSAProvenanceMaterials) = %d, want capped %d", len(decision.SLSAProvenanceMaterials), maxSBOMAttachmentSLSAMaterialRows)
	}
	if !decision.SLSAProvenanceMaterialsTruncated {
		t.Fatal("SLSAProvenanceMaterialsTruncated = false, want true")
	}
}

func TestBuildSBOMAttestationAttachmentDecisionsSLSAProvenanceMissingStatementIDQuarantines(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		attestationStatementFact("statement-orphan", "stmt-orphan", testSBOMSubjectDigest, "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "parsed", "verified"),
		{
			FactID:   "provenance-missing-statement-id",
			FactKind: facts.AttestationSLSAProvenanceFactKind,
			Payload: map[string]any{
				"predicate_type": "https://slsa.dev/provenance/v1",
			},
		},
	}
	_, quarantined, err := buildSBOMAttestationAttachmentDecisionsWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("buildSBOMAttestationAttachmentDecisionsWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %#v, want exactly one input_invalid quarantine for the missing statement_id", quarantined)
	}
	if quarantined[0].factID != "provenance-missing-statement-id" {
		t.Fatalf("quarantined fact = %q, want provenance-missing-statement-id", quarantined[0].factID)
	}
}
