// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSBOMAttestationAttachmentQuarantinesMissingDocumentID is the flagship
// regression test for the sbom/attestation family's typed-decode migration
// (Contract System v1 §3.2, mirroring
// TestGCPResourceMaterializationQuarantinesMissingFullResourceName). It proves
// the accuracy guarantee the migration exists to protect AND the per-fact
// isolation contract: an sbom.document fact missing its required document_id
// key is QUARANTINED as a visible input_invalid dead-letter — never producing
// a wrong-identity attachment decision — while every VALID document in the
// same batch still produces an attachment decision.
//
// Before the migration, the old sbomDocumentFromEnvelope keyed the document by
// firstNonBlank(payloadString(document_id), envelope.FactID): an absent
// document_id silently fell back to the fact's own id and produced a real,
// FactID-keyed attachment decision under a WRONG identity — one that could
// write bad graph identity downstream — with no operator signal. The typed
// decode now dead-letters that fact instead. (The empty-key collapse the
// component test below describes applied to sbom.component/warning/verification,
// which had no FactID fallback; sbom.document/attestation.statement fell back
// to FactID.)
func TestSBOMAttestationAttachmentQuarantinesMissingDocumentID(t *testing.T) {
	t.Parallel()

	// A document fact whose required document_id key is ABSENT (not merely
	// empty). Everything else is present so the ONLY reason to quarantine the
	// fact is the missing required field.
	malformed := facts.Envelope{
		FactID:   "malformed-doc",
		FactKind: facts.SBOMDocumentFactKind,
		Payload: map[string]any{
			// "document_id" intentionally absent.
			"document_digest":     "sha256:9999999999999999999999999999999999999999999999999999999999999999",
			"subject_digest":      testSBOMSubjectDigest,
			"parse_status":        "parsed",
			"verification_status": "verified",
			"format":              "cyclonedx",
			"spec_version":        "1.6",
		},
	}
	// A fully valid, independent document that must still produce an
	// attachment decision despite the malformed fact sharing the batch.
	valid := sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified")

	writer := &recordingSBOMAttestationAttachmentWriter{}
	handler := SBOMAttestationAttachmentHandler{
		FactLoader: &stubSBOMAttestationAttachmentFactLoader{scopeFacts: []facts.Envelope{malformed, valid}},
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sbom-quarantine",
		ScopeID:      "sbom://oci/" + testSBOMSubjectDigest,
		GenerationID: "generation-sbom",
		SourceSystem: "sbom_attestation",
		Domain:       DomainSBOMAttestationAttachment,
		Cause:        "sbom attachment observed",
	})
	// Per-fact isolation: the malformed fact does NOT fail the whole intent.
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed sbom.document fact must be quarantined per-fact, not fail the whole intent", err)
	}

	// The malformed fact must be counted as an input_invalid quarantine in
	// the Result SubSignals so the operator sees it on the per-intent signal
	// (each quarantined fact is also on the
	// eshu_dp_reducer_input_invalid_facts_total counter and a structured
	// error log).
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-document_id fact must be recorded as one input_invalid quarantine", got)
	}

	// The batch's VALID document must still produce an attachment decision:
	// isolation means a poisoned sibling never suppresses valid evidence.
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	found := false
	for _, decision := range writer.write.Decisions {
		if decision.DocumentID == "doc-verified" {
			found = true
		}
		if decision.DocumentID == "" {
			t.Fatalf("a decision was produced under the empty-string document id; the quarantined fact must never surface graph identity: %+v", decision)
		}
	}
	if !found {
		t.Fatalf("no decision produced for the valid sibling document doc-verified; got %+v", writer.write.Decisions)
	}
}

// TestSBOMAttestationAttachmentComponentQuarantinesMissingDocumentID proves
// the same accuracy/isolation contract for sbom.component: a component fact
// missing its required document_id join key is quarantined per-fact rather
// than silently failing to join its owning document, while a valid sibling
// component still contributes ComponentCount evidence to its document's
// decision.
func TestSBOMAttestationAttachmentComponentQuarantinesMissingDocumentID(t *testing.T) {
	t.Parallel()

	malformedComponent := facts.Envelope{
		FactID:   "malformed-component",
		FactKind: facts.SBOMComponentFactKind,
		Payload: map[string]any{
			// "document_id" intentionally absent.
			"component_id": "bad-component",
			"purl":         "pkg:npm/bad-lib@1.0.0",
		},
	}
	doc := sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified")
	validComponent := sbomComponentFact("component-verified", "doc-verified", "pkg:npm/example@1.2.3")

	writer := &recordingSBOMAttestationAttachmentWriter{}
	handler := SBOMAttestationAttachmentHandler{
		FactLoader: &stubSBOMAttestationAttachmentFactLoader{scopeFacts: []facts.Envelope{doc, malformedComponent, validComponent}},
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sbom-component-quarantine",
		ScopeID:      "sbom://oci/" + testSBOMSubjectDigest,
		GenerationID: "generation-sbom",
		SourceSystem: "sbom_attestation",
		Domain:       DomainSBOMAttestationAttachment,
		Cause:        "sbom attachment observed",
	})
	if err != nil {
		t.Fatalf("Handle returned error %v; a single malformed sbom.component fact must be quarantined per-fact, not fail the whole intent", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1; the missing-document_id component must be recorded as one input_invalid quarantine", got)
	}
	if writer.calls != 1 {
		t.Fatalf("writer.calls = %d, want 1", writer.calls)
	}
	var decision *SBOMAttestationAttachmentDecision
	for i := range writer.write.Decisions {
		if writer.write.Decisions[i].DocumentID == "doc-verified" {
			decision = &writer.write.Decisions[i]
		}
	}
	if decision == nil {
		t.Fatalf("no decision produced for doc-verified; got %+v", writer.write.Decisions)
	}
	if decision.ComponentCount != 1 {
		t.Fatalf("ComponentCount = %d, want 1; the valid sibling component must still be counted despite the quarantined component", decision.ComponentCount)
	}
}

// TestSBOMAttestationAttachmentQuarantineReplayIsIdempotent proves replaying
// the exact same batch (including the quarantined malformed sbom.document
// fact) through Handle twice converges on the same quarantine count and the
// same decision for the valid sibling document each time — the typed-decode
// migration introduces no new source of nondeterminism into the reducer's
// at-least-once delivery / idempotent-convergence contract
// (docs/internal/design/contract-system-v1.md §3.4). The decode outcome for
// a given payload is pure: a fact missing its required document_id key is
// quarantined identically on every replay, never intermittently, and never
// escalates from a per-fact quarantine into a whole-intent failure.
func TestSBOMAttestationAttachmentQuarantineReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-doc",
		FactKind: facts.SBOMDocumentFactKind,
		Payload: map[string]any{
			// "document_id" intentionally absent.
			"subject_digest": testSBOMSubjectDigest,
			"parse_status":   "parsed",
			"format":         "cyclonedx",
		},
	}
	valid := sbomDocumentFact("doc-verified", "doc-verified", testSBOMSubjectDigest, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "parsed", "verified")

	intent := Intent{
		IntentID:     "intent-sbom-replay",
		ScopeID:      "sbom://oci/" + testSBOMSubjectDigest,
		GenerationID: "generation-sbom",
		SourceSystem: "sbom_attestation",
		Domain:       DomainSBOMAttestationAttachment,
		Cause:        "sbom attachment observed",
	}

	var results []Result
	for i := 0; i < 2; i++ {
		writer := &recordingSBOMAttestationAttachmentWriter{}
		handler := SBOMAttestationAttachmentHandler{
			FactLoader: &stubSBOMAttestationAttachmentFactLoader{scopeFacts: []facts.Envelope{malformed, valid}},
			Writer:     writer,
		}
		result, err := handler.Handle(context.Background(), intent)
		if err != nil {
			t.Fatalf("replay %d: Handle returned error %v, want nil (per-fact quarantine, never a whole-intent failure)", i, err)
		}
		results = append(results, result)
		if len(writer.write.Decisions) != 1 {
			t.Fatalf("replay %d: len(Decisions) = %d, want 1", i, len(writer.write.Decisions))
		}
	}

	if results[0].SubSignals["input_invalid_facts"] != results[1].SubSignals["input_invalid_facts"] {
		t.Fatalf("input_invalid_facts differs across replays: %v vs %v; the quarantine decision must be deterministic",
			results[0].SubSignals["input_invalid_facts"], results[1].SubSignals["input_invalid_facts"])
	}
	if results[0].CanonicalWrites != results[1].CanonicalWrites {
		t.Fatalf("CanonicalWrites differs across replays: %d vs %d", results[0].CanonicalWrites, results[1].CanonicalWrites)
	}
	if results[0].Status != ResultStatusSucceeded || results[1].Status != ResultStatusSucceeded {
		t.Fatalf("both replays must succeed despite the quarantined fact: got %v and %v", results[0].Status, results[1].Status)
	}
}
