// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
	"github.com/eshu-hq/eshu/go/internal/facts/encode"
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
		ObservationText:     encode.StringPtr(payload.ObservationText),
		ObservationHash:     payload.ObservationHash,
		Source:              encodeSemanticSourceRef(payload.Source),
		Chunk:               encodeSemanticChunkRef(payload.Chunk),
		Provider:            encodeSemanticProviderRef(payload.Provider),
		Confidence:          encode.StringPtr(payload.Confidence),
		ConfidenceRationale: encode.StringPtr(payload.ConfidenceRationale),
		MissingEvidence:     payload.MissingEvidence,
		UnsupportedReason:   encode.StringPtr(payload.UnsupportedReason),
		FreshnessState:      payload.FreshnessState,
		PolicyState:         payload.PolicyState,
		RedactionState:      payload.RedactionState,
		RedactionSummary:    encode.StringPtr(payload.RedactionSummary),
		AdmissionState:      payload.AdmissionState,
		EvidenceRefs:        docs.EncodeEvidenceRefs(payload.EvidenceRefs),
		ACLSummary:          docs.EncodeACLSummary(payload.ACLSummary),
		ObservedAt:          encode.StringPtr(payload.ObservedAt),
	})
	return jsonShapePayload(encoded, err)
}

// EncodeSemanticCodeHint maps the internal semantic code hint payload to the
// SDK factschema encoder used for emitted fact payloads.
func EncodeSemanticCodeHint(payload SemanticCodeHintPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeSemanticCodeHint(semanticv1.CodeHint{
		HintID:              payload.HintID,
		HintType:            payload.HintType,
		RelationshipKind:    encode.StringPtr(payload.RelationshipKind),
		HintText:            encode.StringPtr(payload.HintText),
		HintHash:            payload.HintHash,
		Source:              encodeSemanticSourceRef(payload.Source),
		Chunk:               encodeSemanticChunkRef(payload.Chunk),
		Provider:            encodeSemanticProviderRef(payload.Provider),
		Subject:             encodeSemanticCodeEntityRef(payload.Subject),
		ObjectRefs:          encodeSemanticCodeEntityRefs(payload.ObjectRefs),
		Confidence:          encode.StringPtr(payload.Confidence),
		ConfidenceRationale: encode.StringPtr(payload.ConfidenceRationale),
		MissingEvidence:     payload.MissingEvidence,
		UnsupportedReason:   encode.StringPtr(payload.UnsupportedReason),
		CorroborationState:  payload.CorroborationState,
		PromotionPolicy:     payload.PromotionPolicy,
		PolicyState:         payload.PolicyState,
		RedactionState:      payload.RedactionState,
		FreshnessState:      payload.FreshnessState,
		ObservedAt:          encode.StringPtr(payload.ObservedAt),
	})
	return jsonShapePayload(encoded, err)
}

func encodeSemanticSourceRef(value SemanticSourceRef) semanticv1.SourceRef {
	return semanticv1.SourceRef{
		SourceID:       value.SourceID,
		SourceClass:    value.SourceClass,
		SourceHandle:   encode.StringPtr(value.SourceHandle),
		RepositoryID:   encode.StringPtr(value.RepositoryID),
		DocumentID:     encode.StringPtr(value.DocumentID),
		RelativePath:   encode.StringPtr(value.RelativePath),
		ExternalAnchor: encode.StringPtr(value.ExternalAnchor),
		SectionID:      encode.StringPtr(value.SectionID),
		LineStart:      encode.IntPtr(value.LineStart),
		LineEnd:        encode.IntPtr(value.LineEnd),
		PageStart:      encode.IntPtr(value.PageStart),
		PageEnd:        encode.IntPtr(value.PageEnd),
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
		ModelID:           encode.StringPtr(value.ModelID),
		EndpointProfileID: encode.StringPtr(value.EndpointProfileID),
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
		RepositoryID: encode.StringPtr(value.RepositoryID),
		RelativePath: encode.StringPtr(value.RelativePath),
		EntityKind:   encode.StringPtr(value.EntityKind),
		LineStart:    encode.IntPtr(value.LineStart),
		LineEnd:      encode.IntPtr(value.LineEnd),
	}
}

// jsonShapePayload returns the JSON-shaped form of an encoder's payload, or
// the encoder's own error unchanged. It stays here rather than in
// [encode] so the error a caller sees is returned by same-package code and
// keeps its original text; [encode.JSONShapeMap] owns the normalization.
func jsonShapePayload(payload map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	return encode.JSONShapeMap(payload), nil
}
