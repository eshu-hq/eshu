// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
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
