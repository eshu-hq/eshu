// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	semanticv1 "github.com/eshu-hq/eshu/sdk/go/factschema/semantic/v1"
)

// EncodeSemanticDocumentationObservation maps the internal semantic
// documentation observation payload to the SDK factschema encoder used for
// emitted fact payloads.
func EncodeSemanticDocumentationObservation(payload SemanticDocumentationObservationPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeSemanticDocumentationObservation(semanticv1.DocumentationObservation{
		ObservationID:       payload.ObservationID,
		ObservationType:     payload.ObservationType,
		ObservationText:     stringPtr(payload.ObservationText),
		ObservationHash:     payload.ObservationHash,
		Source:              encodeSemanticSourceRef(payload.Source),
		Chunk:               encodeSemanticChunkRef(payload.Chunk),
		Provider:            encodeSemanticProviderRef(payload.Provider),
		Confidence:          stringPtr(payload.Confidence),
		ConfidenceRationale: stringPtr(payload.ConfidenceRationale),
		MissingEvidence:     payload.MissingEvidence,
		UnsupportedReason:   stringPtr(payload.UnsupportedReason),
		FreshnessState:      payload.FreshnessState,
		PolicyState:         payload.PolicyState,
		RedactionState:      payload.RedactionState,
		RedactionSummary:    stringPtr(payload.RedactionSummary),
		AdmissionState:      payload.AdmissionState,
		EvidenceRefs:        encodeDocumentationEvidenceRefs(payload.EvidenceRefs),
		ACLSummary:          encodeDocumentationACLSummary(payload.ACLSummary),
		ObservedAt:          stringPtr(payload.ObservedAt),
	})
	return jsonShapePayload(encoded, err)
}

// EncodeSemanticCodeHint maps the internal semantic code hint payload to the
// SDK factschema encoder used for emitted fact payloads.
func EncodeSemanticCodeHint(payload SemanticCodeHintPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeSemanticCodeHint(semanticv1.CodeHint{
		HintID:              payload.HintID,
		HintType:            payload.HintType,
		RelationshipKind:    stringPtr(payload.RelationshipKind),
		HintText:            stringPtr(payload.HintText),
		HintHash:            payload.HintHash,
		Source:              encodeSemanticSourceRef(payload.Source),
		Chunk:               encodeSemanticChunkRef(payload.Chunk),
		Provider:            encodeSemanticProviderRef(payload.Provider),
		Subject:             encodeSemanticCodeEntityRef(payload.Subject),
		ObjectRefs:          encodeSemanticCodeEntityRefs(payload.ObjectRefs),
		Confidence:          stringPtr(payload.Confidence),
		ConfidenceRationale: stringPtr(payload.ConfidenceRationale),
		MissingEvidence:     payload.MissingEvidence,
		UnsupportedReason:   stringPtr(payload.UnsupportedReason),
		CorroborationState:  payload.CorroborationState,
		PromotionPolicy:     payload.PromotionPolicy,
		PolicyState:         payload.PolicyState,
		RedactionState:      payload.RedactionState,
		FreshnessState:      payload.FreshnessState,
		ObservedAt:          stringPtr(payload.ObservedAt),
	})
	return jsonShapePayload(encoded, err)
}

func encodeSemanticSourceRef(value SemanticSourceRef) semanticv1.SourceRef {
	return semanticv1.SourceRef{
		SourceID:       value.SourceID,
		SourceClass:    value.SourceClass,
		SourceHandle:   stringPtr(value.SourceHandle),
		RepositoryID:   stringPtr(value.RepositoryID),
		DocumentID:     stringPtr(value.DocumentID),
		RelativePath:   stringPtr(value.RelativePath),
		ExternalAnchor: stringPtr(value.ExternalAnchor),
		SectionID:      stringPtr(value.SectionID),
		LineStart:      intPtr(value.LineStart),
		LineEnd:        intPtr(value.LineEnd),
		PageStart:      intPtr(value.PageStart),
		PageEnd:        intPtr(value.PageEnd),
	}
}

func encodeSemanticChunkRef(value SemanticChunkRef) semanticv1.ChunkRef {
	return semanticv1.ChunkRef{
		ChunkID:          value.ChunkID,
		ChunkHash:        value.ChunkHash,
		SourceHash:       value.SourceHash,
		PromptVersion:    value.PromptVersion,
		RedactionVersion: value.RedactionVersion,
		ExtractorVersion: value.ExtractorVersion,
		ExtractionMode:   value.ExtractionMode,
	}
}

func encodeSemanticProviderRef(value SemanticProviderRef) semanticv1.ProviderRef {
	return semanticv1.ProviderRef{
		ProviderProfileID: value.ProviderProfileID,
		ProviderKind:      value.ProviderKind,
		ModelID:           stringPtr(value.ModelID),
		EndpointProfileID: stringPtr(value.EndpointProfileID),
	}
}

func encodeSemanticCodeEntityRefs(values []SemanticCodeEntityRef) []semanticv1.CodeEntityRef {
	if values == nil {
		return nil
	}
	out := make([]semanticv1.CodeEntityRef, 0, len(values))
	for _, value := range values {
		out = append(out, encodeSemanticCodeEntityRef(value))
	}
	return out
}

func encodeSemanticCodeEntityRef(value SemanticCodeEntityRef) semanticv1.CodeEntityRef {
	return semanticv1.CodeEntityRef{
		EntityID:     value.EntityID,
		RepositoryID: stringPtr(value.RepositoryID),
		RelativePath: stringPtr(value.RelativePath),
		EntityKind:   stringPtr(value.EntityKind),
		LineStart:    intPtr(value.LineStart),
		LineEnd:      intPtr(value.LineEnd),
	}
}

func intPtr(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}
