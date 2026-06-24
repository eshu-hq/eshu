// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticdocs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestEmitterBuildsValidDocumentationObservationEnvelope(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-test",
			ProviderKind:      ProviderKindMock,
			ModelID:           "mock-doc-model",
		},
		ObservedAt: fixedObservationTime,
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v, want nil", err)
	}

	envelopes, err := emitter.Emit(context.Background(), semanticSectionFixture(), []MockObservation{{
		ObservationType:     "runbook_step",
		ObservationText:     "Payment service deploys through the production Helm release.",
		Confidence:          facts.SemanticConfidenceHigh,
		ConfidenceRationale: "bounded runbook section names one service and one release",
		AdmissionState:      facts.SemanticAdmissionDocumentationFindingCandidate,
	}})
	if err != nil {
		t.Fatalf("Emit() error = %v, want nil", err)
	}
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("len(envelopes) = %d, want %d", got, want)
	}

	envelope := envelopes[0]
	if got, want := envelope.FactKind, facts.SemanticDocumentationObservationFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := envelope.SchemaVersion, facts.SemanticFactSchemaVersion; got != want {
		t.Fatalf("SchemaVersion = %q, want %q", got, want)
	}
	if got, want := envelope.CollectorKind, CollectorKind; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := envelope.SourceConfidence, facts.SourceConfidenceDerived; got != want {
		t.Fatalf("SourceConfidence = %q, want %q", got, want)
	}
	if got, want := envelope.ScopeID, "scope:docs"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := envelope.GenerationID, "generation:1"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if envelope.FactID == "" || envelope.StableFactKey == "" {
		t.Fatalf("FactID and StableFactKey must be set: %#v", envelope)
	}
	if got, want := envelope.SourceRef.SourceSystem, "git"; got != want {
		t.Fatalf("SourceRef.SourceSystem = %q, want %q", got, want)
	}
	if got, want := envelope.SourceRef.SourceURI, "git://repo/docs/runbook.md#deploy"; got != want {
		t.Fatalf("SourceRef.SourceURI = %q, want %q", got, want)
	}
	if got, want := envelope.SourceRef.SourceRecordID, "section:deploy"; got != want {
		t.Fatalf("SourceRef.SourceRecordID = %q, want %q", got, want)
	}

	payload := semanticPayloadFromEnvelope(t, envelope)
	if err := facts.ValidateSemanticDocumentationObservationPayload(payload); err != nil {
		t.Fatalf("ValidateSemanticDocumentationObservationPayload() error = %v, want nil", err)
	}
	if got, want := payload.Source.SourceClass, facts.SemanticSourceClassDocumentation; got != want {
		t.Fatalf("Source.SourceClass = %q, want %q", got, want)
	}
	if got, want := payload.Source.DocumentID, "doc:runbook"; got != want {
		t.Fatalf("Source.DocumentID = %q, want %q", got, want)
	}
	if got, want := payload.Source.SectionID, "section:deploy"; got != want {
		t.Fatalf("Source.SectionID = %q, want %q", got, want)
	}
	if got, want := payload.Chunk.SourceHash, "sha256:source-v1"; got != want {
		t.Fatalf("Chunk.SourceHash = %q, want %q", got, want)
	}
	if got, want := payload.Chunk.ChunkHash, "sha256:excerpt-v1"; got != want {
		t.Fatalf("Chunk.ChunkHash = %q, want %q", got, want)
	}
	if got, want := payload.Chunk.PromptVersion, DefaultPromptVersion; got != want {
		t.Fatalf("Chunk.PromptVersion = %q, want %q", got, want)
	}
	if got, want := payload.Chunk.RedactionVersion, DefaultRedactionVersion; got != want {
		t.Fatalf("Chunk.RedactionVersion = %q, want %q", got, want)
	}
	if got, want := payload.Chunk.ExtractorVersion, DefaultExtractorVersion; got != want {
		t.Fatalf("Chunk.ExtractorVersion = %q, want %q", got, want)
	}
	if got, want := payload.Chunk.ExtractionMode, facts.SemanticExtractionModeAssistant; got != want {
		t.Fatalf("Chunk.ExtractionMode = %q, want %q", got, want)
	}
	if got, want := payload.Provider.ProviderProfileID, "semantic-docs-test"; got != want {
		t.Fatalf("Provider.ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := payload.PolicyState, facts.SemanticPolicyAllowed; got != want {
		t.Fatalf("PolicyState = %q, want %q", got, want)
	}
	if got, want := payload.RedactionState, facts.SemanticRedactionSkippedNoSensitiveContent; got != want {
		t.Fatalf("RedactionState = %q, want %q", got, want)
	}
	if got, want := payload.FreshnessState, facts.SemanticFreshnessFresh; got != want {
		t.Fatalf("FreshnessState = %q, want %q", got, want)
	}
	if got, want := payload.ObservedAt, "2026-06-08T15:04:05Z"; got != want {
		t.Fatalf("ObservedAt = %q, want %q", got, want)
	}
	if got, want := payload.EvidenceRefs, 1; len(got) != want {
		t.Fatalf("len(EvidenceRefs) = %d, want %d", len(got), want)
	}
	if got, want := payload.EvidenceRefs[0].Kind, facts.DocumentationSectionFactKind; got != want {
		t.Fatalf("EvidenceRefs[0].Kind = %q, want %q", got, want)
	}
}

func TestEmitterUnsafeRedactionForcesProvenanceOnlyAndDropsObservationText(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-test",
			ProviderKind:      ProviderKindMock,
		},
		RedactionState: facts.SemanticRedactionUnsafePayload,
		ObservedAt:     fixedObservationTime,
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v, want nil", err)
	}

	envelopes, err := emitter.Emit(context.Background(), semanticSectionFixture(), []MockObservation{{
		ObservationType:     "unsafe-type-text",
		ObservationText:     "unsafe-observation-text",
		ObservationHash:     "sha256:unsafe-model-supplied-hash",
		Confidence:          facts.SemanticConfidenceHigh,
		ConfidenceRationale: "unsafe-rationale-text",
		MissingEvidence:     []string{"unsafe-missing-evidence"},
		UnsupportedReason:   "unsafe-unsupported-reason",
		AdmissionState:      facts.SemanticAdmissionDocumentationFindingCandidate,
	}})
	if err != nil {
		t.Fatalf("Emit() error = %v, want nil", err)
	}
	payload := semanticPayloadFromEnvelope(t, envelopes[0])
	if got, want := payload.AdmissionState, facts.SemanticAdmissionProvenanceOnly; got != want {
		t.Fatalf("AdmissionState = %q, want %q", got, want)
	}
	if got, want := payload.RedactionState, facts.SemanticRedactionUnsafePayload; got != want {
		t.Fatalf("RedactionState = %q, want %q", got, want)
	}
	if got, want := payload.ObservationType, facts.SemanticRedactionUnsafePayload; got != want {
		t.Fatalf("ObservationType = %q, want %q", got, want)
	}
	if payload.ObservationText != "" {
		t.Fatalf("ObservationText = %q, want empty for unsafe payload", payload.ObservationText)
	}
	if payload.Confidence != "" {
		t.Fatalf("Confidence = %q, want empty for unsafe payload", payload.Confidence)
	}
	if payload.ConfidenceRationale != "" {
		t.Fatalf("ConfidenceRationale = %q, want empty for unsafe payload", payload.ConfidenceRationale)
	}
	if len(payload.MissingEvidence) != 0 {
		t.Fatalf("MissingEvidence = %#v, want empty for unsafe payload", payload.MissingEvidence)
	}
	if got, want := payload.UnsupportedReason, facts.SemanticRedactionUnsafePayload; got != want {
		t.Fatalf("UnsupportedReason = %q, want %q", got, want)
	}
	if payload.ObservationHash == "sha256:unsafe-model-supplied-hash" {
		t.Fatal("ObservationHash kept model-supplied unsafe hash, want emitter-owned hash")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	for _, forbidden := range []string{
		"unsafe-observation-text",
		"unsafe-type-text",
		"unsafe-model-supplied-hash",
		"unsafe-rationale-text",
		"unsafe-missing-evidence",
		"unsafe-unsupported-reason",
	} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("payload JSON leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestEmitterDefaultsObservedAtToSectionTime(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: " semantic-docs-test ",
			ProviderKind:      " mock ",
		},
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v, want nil", err)
	}

	envelopes, err := emitter.Emit(context.Background(), semanticSectionFixture(), []MockObservation{{
		ObservationType: "summary",
	}})
	if err != nil {
		t.Fatalf("Emit() error = %v, want nil", err)
	}
	payload := semanticPayloadFromEnvelope(t, envelopes[0])
	if got, want := envelopes[0].ObservedAt.UTC().Format(time.RFC3339), "2026-06-08T15:04:05Z"; got != want {
		t.Fatalf("Envelope.ObservedAt = %q, want %q", got, want)
	}
	if got, want := payload.ObservedAt, "2026-06-08T15:04:05Z"; got != want {
		t.Fatalf("payload.ObservedAt = %q, want %q", got, want)
	}
	if got, want := payload.Provider.ProviderProfileID, "semantic-docs-test"; got != want {
		t.Fatalf("ProviderProfileID = %q, want trimmed %q", got, want)
	}
	if got, want := payload.Provider.ProviderKind, ProviderKindMock; got != want {
		t.Fatalf("ProviderKind = %q, want trimmed %q", got, want)
	}
}

func TestEmitterStableIDChangesOnlyForReplayProvenance(t *testing.T) {
	t.Parallel()

	emitter, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-test",
			ProviderKind:      ProviderKindMock,
		},
		ObservedAt: fixedObservationTime,
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v, want nil", err)
	}

	first := emitOneStableFactKey(t, emitter, semanticSectionFixture(), "first display text")
	second := emitOneStableFactKey(t, emitter, semanticSectionFixture(), "updated display text")
	if first != second {
		t.Fatalf("stable fact key changed after observation display text changed: first=%q second=%q", first, second)
	}

	changedSection := semanticSectionFixture()
	changedSection.ExcerptHash = "sha256:excerpt-v2"
	changedChunk := emitOneStableFactKey(t, emitter, changedSection, "first display text")
	if first == changedChunk {
		t.Fatalf("stable fact key did not change after chunk hash changed: %q", first)
	}

	changedProvider, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-other",
			ProviderKind:      ProviderKindMock,
		},
		ObservedAt: fixedObservationTime,
	})
	if err != nil {
		t.Fatalf("NewEmitter(changed provider) error = %v, want nil", err)
	}
	providerKey := emitOneStableFactKey(t, changedProvider, semanticSectionFixture(), "first display text")
	if first == providerKey {
		t.Fatalf("stable fact key did not change after provider profile changed: %q", first)
	}
}

func TestEmitterRejectsMissingProviderAndReplayProvenance(t *testing.T) {
	t.Parallel()

	if _, err := NewEmitter(Config{}); err == nil {
		t.Fatal("NewEmitter() error = nil, want missing provider error")
	}
	if _, err := NewEmitter(Config{Provider: ProviderProfile{ProviderProfileID: "semantic-docs-test"}}); err == nil {
		t.Fatal("NewEmitter() error = nil, want missing provider kind error")
	}
	if _, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-test",
			ProviderKind:      ProviderKindMock,
		},
		ExtractionMode: "unchecked_mode",
	}); err == nil {
		t.Fatal("NewEmitter() error = nil, want invalid extraction mode error")
	}

	emitter, err := NewEmitter(Config{
		Provider: ProviderProfile{
			ProviderProfileID: "semantic-docs-test",
			ProviderKind:      ProviderKindMock,
		},
		ObservedAt: fixedObservationTime,
	})
	if err != nil {
		t.Fatalf("NewEmitter() error = %v, want nil", err)
	}

	section := semanticSectionFixture()
	section.ExcerptHash = ""
	if _, err := emitter.Emit(context.Background(), section, []MockObservation{{ObservationType: "summary"}}); err == nil {
		t.Fatal("Emit() error = nil, want missing chunk hash error")
	}

	section = semanticSectionFixture()
	section.RevisionID = ""
	if _, err := emitter.Emit(context.Background(), section, []MockObservation{{ObservationType: "summary"}}); err == nil {
		t.Fatal("Emit() error = nil, want missing source hash error")
	}

	section = semanticSectionFixture()
	section.CanonicalURI = ""
	if _, err := emitter.Emit(context.Background(), section, []MockObservation{{ObservationType: "summary"}}); err == nil {
		t.Fatal("Emit() error = nil, want missing canonical uri error")
	}

	if _, err := emitter.Emit(context.Background(), semanticSectionFixture(), []MockObservation{{
		ObservationType: "summary",
		AdmissionState:  "canonical_truth",
	}}); err == nil {
		t.Fatal("Emit() error = nil, want unsupported admission state error")
	}
	if _, err := emitter.Emit(context.Background(), semanticSectionFixture(), []MockObservation{{
		ObservationType: "summary",
		AdmissionState:  facts.SemanticAdmissionExact,
	}}); err == nil {
		t.Fatal("Emit() error = nil, want reducer admission state rejected before reducer")
	}
}

func semanticPayloadFromEnvelope(t *testing.T, envelope facts.Envelope) facts.SemanticDocumentationObservationPayload {
	t.Helper()

	encoded, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload map) error = %v, want nil", err)
	}
	var payload facts.SemanticDocumentationObservationPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v, want nil; json=%s", err, encoded)
	}
	return payload
}

func emitOneStableFactKey(t *testing.T, emitter *Emitter, section doctruth.SectionInput, text string) string {
	t.Helper()

	envelopes, err := emitter.Emit(context.Background(), section, []MockObservation{{
		ObservationType: "summary",
		ObservationText: text,
		AdmissionState:  facts.SemanticAdmissionDocumentationFindingCandidate,
	}})
	if err != nil {
		t.Fatalf("Emit() error = %v, want nil", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("len(envelopes) = %d, want 1", len(envelopes))
	}
	return envelopes[0].StableFactKey
}

func semanticSectionFixture() doctruth.SectionInput {
	return doctruth.SectionInput{
		ScopeID:        "scope:docs",
		GenerationID:   "generation:1",
		SourceSystem:   "git",
		DocumentID:     "doc:runbook",
		RevisionID:     "sha256:source-v1",
		SectionID:      "section:deploy",
		CanonicalURI:   "git://repo/docs/runbook.md#deploy",
		ExcerptHash:    "sha256:excerpt-v1",
		SourceStartRef: "line:10",
		SourceEndRef:   "line:18",
		Text:           "Deploy the payment service through the production Helm release.",
		ObservedAt:     fixedObservationTime(),
	}
}

func fixedObservationTime() time.Time {
	return time.Date(2026, time.June, 8, 15, 4, 5, 0, time.UTC)
}
