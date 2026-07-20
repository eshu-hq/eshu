// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func dependencyRelationshipFact(factID, documentID, from, to, relType, origin string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMDependencyRelationshipFactKind,
		Payload: map[string]any{
			"document_id":         documentID,
			"from_component_id":   from,
			"to_component_id":     to,
			"relationship_type":   relType,
			"relationship_origin": origin,
		},
	}
}

func externalReferenceFact(factID, documentID, componentID, refType, refURL, refLocator string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.SBOMExternalReferenceFactKind,
		Payload: map[string]any{
			"document_id":       documentID,
			"component_id":      componentID,
			"reference_type":    refType,
			"reference_url":     refURL,
			"reference_locator": refLocator,
		},
	}
}

// TestBuildSBOMAttestationAttachmentDecisionsSurfacesDependencyAndExternalReferenceEvidence
// is the accuracy regression for #5370: sbom.dependency_relationship and
// sbom.external_reference facts are queue-routed to this reducer
// (sbomAttestationAttachmentFactKinds) but buildSBOMAttachmentIndex's decode
// switch previously had no case for them, so they were silently dropped and
// never reached the decision or the attachments read surface. This test
// fails on the pre-fix code (DependencyRelationshipCount/Evidence and
// ExternalReferenceCount/Evidence are always zero-value) and passes once the
// index decodes and the decision carries the evidence through.
func TestBuildSBOMAttestationAttachmentDecisionsSurfacesDependencyAndExternalReferenceEvidence(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified"),
		ociImageReferrerFact("referrer-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "application/vnd.cyclonedx+json"),
		dependencyRelationshipFact("dep-1", "doc-verified", "pkg:npm/app@1.0.0", "pkg:npm/lib@2.0.0", "depends_on", "declared"),
		externalReferenceFact("ref-1", "doc-verified", "pkg:npm/lib@2.0.0", "advisory", "https://example.com/advisory/1", ""),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	decision, ok := got["doc-verified"]
	if !ok {
		t.Fatalf("decision for doc-verified missing: %#v", got)
	}
	if got, want := decision.DependencyRelationshipCount, 1; got != want {
		t.Fatalf("DependencyRelationshipCount = %d, want %d", got, want)
	}
	if got, want := len(decision.DependencyRelationshipEvidence), 1; got != want {
		t.Fatalf("len(DependencyRelationshipEvidence) = %d, want %d", got, want)
	}
	depRow := decision.DependencyRelationshipEvidence[0]
	if got, want := depRow["from_component_id"], "pkg:npm/app@1.0.0"; got != want {
		t.Fatalf("from_component_id = %q, want %q", got, want)
	}
	if got, want := depRow["to_component_id"], "pkg:npm/lib@2.0.0"; got != want {
		t.Fatalf("to_component_id = %q, want %q", got, want)
	}
	if got, want := depRow["fact_id"], "dep-1"; got != want {
		t.Fatalf("fact_id = %q, want %q", got, want)
	}
	if got, want := decision.ExternalReferenceCount, 1; got != want {
		t.Fatalf("ExternalReferenceCount = %d, want %d", got, want)
	}
	if got, want := len(decision.ExternalReferenceEvidence), 1; got != want {
		t.Fatalf("len(ExternalReferenceEvidence) = %d, want %d", got, want)
	}
	refRow := decision.ExternalReferenceEvidence[0]
	if got, want := refRow["reference_url"], "https://example.com/advisory/1"; got != want {
		t.Fatalf("reference_url = %q, want %q", got, want)
	}
	if got, want := refRow["fact_id"], "ref-1"; got != want {
		t.Fatalf("fact_id = %q, want %q", got, want)
	}
}

// TestBuildSBOMAttestationAttachmentDecisionsAllowsDanglingComponentIDs proves
// the locked Q4 edge-case decision: a dependency/external-reference row whose
// from/to/component id does not match any indexed sbom.component is surfaced
// as-is (declared-evidence surface, not resolved graph truth) rather than
// dropped or validated against index.components.
func TestBuildSBOMAttestationAttachmentDecisionsAllowsDanglingComponentIDs(t *testing.T) {
	t.Parallel()

	decisions := BuildSBOMAttestationAttachmentDecisions([]facts.Envelope{
		sbomDocumentFact("doc-dangling", "doc-dangling", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified"),
		dependencyRelationshipFact("dep-dangling", "doc-dangling", "pkg:npm/missing-from@1.0.0", "pkg:npm/missing-to@1.0.0", "depends_on", "declared"),
	})

	got := sbomAttachmentDecisionsByDocument(decisions)
	decision := got["doc-dangling"]
	if decision.DependencyRelationshipCount != 1 {
		t.Fatalf("DependencyRelationshipCount = %d, want 1 (dangling ids must still surface)", decision.DependencyRelationshipCount)
	}
}

// TestDependencyRelationshipEvidenceRowsDedupesCapsAndCountsBeforeCap proves
// the locked Q2 bounding contract: distinct-tuple dedupe, a deterministic
// lexicographic sort with fact_id as the final tiebreaker, a persisted count
// computed BEFORE the cap, and a hard cap on the returned row set.
func TestDependencyRelationshipEvidenceRowsDedupesCapsAndCountsBeforeCap(t *testing.T) {
	t.Parallel()

	// Two facts report the identical (from, to, type, origin) tuple with
	// different fact ids: they must dedupe to one row, keeping the
	// lexicographically smaller fact_id.
	dup := []sbomAttachmentDependencyEvidence{
		{factID: "dep-b", fromComponentID: "a", toComponentID: "b", relationshipType: "depends_on", relationshipOrigin: "declared"},
		{factID: "dep-a", fromComponentID: "a", toComponentID: "b", relationshipType: "depends_on", relationshipOrigin: "declared"},
	}
	rows, count := dependencyRelationshipEvidenceRows(dup)
	if count != 1 {
		t.Fatalf("count = %d, want 1 for a duplicate tuple", count)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["fact_id"], "dep-a"; got != want {
		t.Fatalf("fact_id = %q, want %q (smallest fact_id wins the dedupe)", got, want)
	}

	// 150 distinct tuples must report count=150 but cap the row set at
	// maxSBOMAttachmentDependencyRelationshipRows (100).
	var many []sbomAttachmentDependencyEvidence
	for i := 0; i < 150; i++ {
		many = append(many, sbomAttachmentDependencyEvidence{
			factID:             fmt.Sprintf("dep-%03d", i),
			fromComponentID:    fmt.Sprintf("component-%03d", i),
			toComponentID:      "shared-dependency",
			relationshipType:   "depends_on",
			relationshipOrigin: "declared",
		})
	}
	cappedRows, cappedCount := dependencyRelationshipEvidenceRows(many)
	if cappedCount != 150 {
		t.Fatalf("count = %d, want 150 (full distinct-tuple count survives the cap)", cappedCount)
	}
	if len(cappedRows) != maxSBOMAttachmentDependencyRelationshipRows {
		t.Fatalf("len(rows) = %d, want %d", len(cappedRows), maxSBOMAttachmentDependencyRelationshipRows)
	}
	if got, want := cappedRows[0]["from_component_id"], "component-000"; got != want {
		t.Fatalf("rows[0].from_component_id = %q, want %q (deterministic lexicographic sort)", got, want)
	}
}

// TestExternalReferenceEvidenceRowsDedupesCapsAndCountsBeforeCap mirrors
// TestDependencyRelationshipEvidenceRowsDedupesCapsAndCountsBeforeCap for
// sbom.external_reference evidence, proving the 50-row cap.
func TestExternalReferenceEvidenceRowsDedupesCapsAndCountsBeforeCap(t *testing.T) {
	t.Parallel()

	var many []sbomAttachmentExternalReferenceEvidence
	for i := 0; i < 60; i++ {
		many = append(many, sbomAttachmentExternalReferenceEvidence{
			factID:        fmt.Sprintf("ref-%03d", i),
			componentID:   fmt.Sprintf("component-%03d", i),
			referenceType: "advisory",
			referenceURL:  fmt.Sprintf("https://example.com/advisory/%d", i),
		})
	}
	rows, count := externalReferenceEvidenceRows(many)
	if count != 60 {
		t.Fatalf("count = %d, want 60", count)
	}
	if len(rows) != maxSBOMAttachmentExternalReferenceRows {
		t.Fatalf("len(rows) = %d, want %d", len(rows), maxSBOMAttachmentExternalReferenceRows)
	}
}

// TestBuildSBOMAttestationAttachmentDecisionsQuarantinesDependencyMissingDocumentID
// proves the locked Q4/contract-rigor rule: a sbom.dependency_relationship
// fact missing its required document_id decodes to a classified
// input_invalid dead-letter (via partitionDecodeFailures) rather than a
// silent drop, exactly like every other typed kind in this family.
func TestBuildSBOMAttestationAttachmentDecisionsQuarantinesDependencyMissingDocumentID(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactID:   "dep-missing-doc",
		FactKind: facts.SBOMDependencyRelationshipFactKind,
		Payload: map[string]any{
			"from_component_id": "a",
			"to_component_id":   "b",
		},
	}
	_, quarantined, err := buildSBOMAttestationAttachmentDecisionsWithQuarantine([]facts.Envelope{envelope})
	if err != nil {
		t.Fatalf("buildSBOMAttestationAttachmentDecisionsWithQuarantine() error = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined = %d, want 1", len(quarantined))
	}
	if got, want := quarantined[0].factID, "dep-missing-doc"; got != want {
		t.Fatalf("quarantined fact id = %q, want %q", got, want)
	}
}

// TestSBOMAttestationAttachmentHandlerLoadsActiveDependencyAndExternalReferenceEvidence
// is the trap-1 regression: dependency/external-reference facts loaded only
// through the bounded active-evidence expansion (not the initial
// scope/generation load) must still reach the decision. Before the fix, the
// Postgres active-evidence loader's fact_kind allowlist
// (listActiveSBOMAttestationAttachmentFactsQuery) excluded both kinds, so a
// document discovered via an OCI referrer (the same shape as
// TestSBOMAttestationAttachmentHandlerLoadsActiveDocumentEvidenceForReferrer)
// got its components but silently zero dependency/external-reference rows.
func TestSBOMAttestationAttachmentHandlerLoadsActiveDependencyAndExternalReferenceEvidence(t *testing.T) {
	t.Parallel()

	documentDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	loader := &stubSBOMAttestationAttachmentFactLoader{
		scopeFacts: []facts.Envelope{
			ociImageReferrerFact("referrer-verified", testSBOMSubjectDigest, documentDigest, "application/vnd.cyclonedx+json"),
		},
		active: []facts.Envelope{
			sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, documentDigest, "parsed", "verified"),
			sbomComponentFact("component-verified", "doc-verified", "pkg:npm/example@1.2.3"),
			dependencyRelationshipFact("dep-active", "doc-verified", "pkg:npm/example@1.2.3", "pkg:npm/dep@0.1.0", "depends_on", "declared"),
			externalReferenceFact("ref-active", "doc-verified", "pkg:npm/example@1.2.3", "website", "https://example.com", ""),
		},
	}
	writer := &recordingSBOMAttestationAttachmentWriter{}
	handler := SBOMAttestationAttachmentHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sbom-dep-ext",
		ScopeID:      "oci-registry://registry.example.com/team/api",
		GenerationID: "generation-oci",
		SourceSystem: "oci_registry",
		Domain:       DomainSBOMAttestationAttachment,
		Cause:        "OCI referrer subject evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(writer.write.Decisions) != 1 {
		t.Fatalf("len(Decisions) = %d, want 1", len(writer.write.Decisions))
	}
	decision := writer.write.Decisions[0]
	if decision.DependencyRelationshipCount != 1 {
		t.Fatalf("DependencyRelationshipCount = %d, want 1", decision.DependencyRelationshipCount)
	}
	if decision.ExternalReferenceCount != 1 {
		t.Fatalf("ExternalReferenceCount = %d, want 1", decision.ExternalReferenceCount)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
}
