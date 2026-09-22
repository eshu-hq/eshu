// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts/encode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"
)

// EncodeSource maps the internal documentation source identity
// payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeSource(payload SourcePayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationSource(documentationv1.Source{
		SourceID:       payload.SourceID,
		SourceSystem:   payload.SourceSystem,
		ExternalID:     payload.ExternalID,
		DisplayName:    encode.StringPtr(payload.DisplayName),
		BaseURI:        encode.StringPtr(payload.BaseURI),
		SourceType:     encode.StringPtr(payload.SourceType),
		Labels:         payload.Labels,
		OwnerRefs:      encodeOwnerRefs(payload.OwnerRefs),
		ACLSummary:     EncodeACLSummary(payload.ACLSummary),
		SourceMetadata: payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeDocument maps the internal documentation document
// identity payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeDocument(payload DocumentPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationDocument(documentationv1.Document{
		DocumentID:        payload.DocumentID,
		SourceID:          encode.StringPtr(payload.SourceID),
		ExternalID:        encode.StringPtr(payload.ExternalID),
		RevisionID:        encode.StringPtr(payload.RevisionID),
		CanonicalURI:      encode.StringPtr(payload.CanonicalURI),
		Title:             encode.StringPtr(payload.Title),
		ParentDocumentID:  encode.StringPtr(payload.ParentDocumentID),
		DocumentType:      encode.StringPtr(payload.DocumentType),
		Format:            encode.StringPtr(payload.Format),
		Language:          encode.StringPtr(payload.Language),
		Labels:            payload.Labels,
		OwnerRefs:         encodeOwnerRefs(payload.OwnerRefs),
		ACLSummary:        EncodeACLSummary(payload.ACLSummary),
		SourceMetadata:    payload.SourceMetadata,
		ContentHash:       encode.StringPtr(payload.ContentHash),
		DocumentCreatedAt: encode.StringPtr(payload.DocumentCreatedAt),
		DocumentUpdatedAt: encode.StringPtr(payload.DocumentUpdatedAt),
	})
	return jsonShapePayload(encoded, err)
}

// EncodeSection maps the internal documentation section identity
// payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeSection(payload SectionPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationSection(documentationv1.Section{
		DocumentID:       payload.DocumentID,
		RevisionID:       payload.RevisionID,
		SectionID:        payload.SectionID,
		ParentSectionID:  encode.StringPtr(payload.ParentSectionID),
		SectionAnchor:    encode.StringPtr(payload.SectionAnchor),
		HeadingText:      encode.StringPtr(payload.HeadingText),
		OrdinalPath:      payload.OrdinalPath,
		Content:          encode.StringPtr(payload.Content),
		ContentFormat:    encode.StringPtr(payload.ContentFormat),
		TextHash:         encode.StringPtr(payload.TextHash),
		ExcerptHash:      encode.StringPtr(payload.ExcerptHash),
		SourceStartRef:   encode.StringPtr(payload.SourceStartRef),
		SourceEndRef:     encode.StringPtr(payload.SourceEndRef),
		SourceMetadata:   payload.SourceMetadata,
		ContainsWarnings: encode.BoolPtr(payload.ContainsWarnings),
	})
	return jsonShapePayload(encoded, err)
}

// EncodeLink maps the internal documentation link identity
// payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeLink(payload LinkPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationLink(documentationv1.Link{
		DocumentID:     payload.DocumentID,
		RevisionID:     encode.StringPtr(payload.RevisionID),
		SectionID:      encode.StringPtr(payload.SectionID),
		LinkID:         payload.LinkID,
		TargetURI:      payload.TargetURI,
		TargetKind:     encode.StringPtr(payload.TargetKind),
		AnchorTextHash: encode.StringPtr(payload.AnchorTextHash),
		SourceMetadata: payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeEntityMention maps the internal documentation entity
// mention identity payload to the SDK factschema encoder used for emitted fact
// payloads.
func EncodeEntityMention(payload EntityMentionPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationEntityMention(documentationv1.EntityMention{
		DocumentID:       payload.DocumentID,
		RevisionID:       encode.StringPtr(payload.RevisionID),
		SectionID:        payload.SectionID,
		MentionID:        encode.StringPtr(payload.MentionID),
		MentionText:      encode.StringPtr(payload.MentionText),
		MentionKind:      encode.StringPtr(payload.MentionKind),
		ResolutionStatus: payload.ResolutionStatus,
		CandidateRefs:    EncodeEvidenceRefs(payload.CandidateRefs),
		ExcerptHash:      encode.StringPtr(payload.ExcerptHash),
		ACLSummary:       EncodeACLSummary(payload.ACLSummary),
		SourceMetadata:   payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeClaimCandidate maps the internal documentation claim
// candidate identity payload to the SDK factschema encoder used for emitted
// fact payloads.
func EncodeClaimCandidate(payload ClaimCandidatePayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationClaimCandidate(documentationv1.ClaimCandidate{
		DocumentID:       payload.DocumentID,
		RevisionID:       encode.StringPtr(payload.RevisionID),
		SectionID:        payload.SectionID,
		ClaimID:          payload.ClaimID,
		ClaimType:        payload.ClaimType,
		ClaimText:        payload.ClaimText,
		ClaimHash:        payload.ClaimHash,
		ExcerptHash:      encode.StringPtr(payload.ExcerptHash),
		SubjectMentionID: encode.StringPtr(payload.SubjectMentionID),
		ObjectMentionIDs: payload.ObjectMentionIDs,
		EvidenceRefs:     EncodeEvidenceRefs(payload.EvidenceRefs),
		Authority:        payload.Authority,
		ACLSummary:       EncodeACLSummary(payload.ACLSummary),
		SourceMetadata:   payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeFinding maps the verifier's documentation finding map
// through the SDK factschema encoder and preserves verifier-owned extension
// fields that the typed contract intentionally leaves open.
func EncodeFinding(payload map[string]any) (map[string]any, error) {
	finding := documentationv1.Finding{
		FindingID:        encode.StringValue(payload, "finding_id"),
		FindingVersion:   encode.StringValue(payload, "finding_version"),
		FindingType:      encode.StringPtrFromMap(payload, "finding_type"),
		Status:           encode.StringPtrFromMap(payload, "status"),
		TruthLevel:       encode.StringPtrFromMap(payload, "truth_level"),
		FreshnessState:   encode.StringPtrFromMap(payload, "freshness_state"),
		SourceID:         encode.StringPtrFromMap(payload, "source_id"),
		DocumentID:       encode.StringPtrFromMap(payload, "document_id"),
		SectionID:        encode.StringPtrFromMap(payload, "section_id"),
		ClaimID:          encode.StringPtrFromMap(payload, "claim_id"),
		ClaimType:        encode.StringPtrFromMap(payload, "claim_type"),
		ClaimText:        encode.StringPtrFromMap(payload, "claim_text"),
		NormalizedClaim:  encode.StringPtrFromMap(payload, "normalized_claim"),
		Summary:          encode.StringPtrFromMap(payload, "summary"),
		EvidencePacketID: encode.StringPtrFromMap(payload, "evidence_packet_id"),
		ClaimByteOffset:  encode.IntPtrFromMap(payload, "claim_byte_offset"),
		ClaimByteLength:  encode.IntPtrFromMap(payload, "claim_byte_length"),
	}
	encoded, err := factschema.EncodeDocumentationFinding(finding)
	if err != nil {
		return nil, fmt.Errorf("encode documentation finding payload: %w", err)
	}
	copyOpenFields(encoded, payload, "evidence_packet_url", "permissions", "states")
	return encode.JSONShapeMap(encoded), nil
}

// EncodeEvidencePacket maps the verifier's documentation evidence
// packet map through the SDK factschema encoder and preserves verifier-owned
// extension fields that the typed contract intentionally leaves open.
func EncodeEvidencePacket(payload map[string]any) (map[string]any, error) {
	packet := documentationv1.EvidencePacket{
		PacketID:       encode.StringValue(payload, "packet_id"),
		PacketVersion:  encode.StringPtrFromMap(payload, "packet_version"),
		GeneratedAt:    encode.StringPtrFromMap(payload, "generated_at"),
		FindingID:      encode.StringValue(payload, "finding_id"),
		LinkedEntities: linkedEntityRefsFromMap(payload, "linked_entities"),
	}
	encoded, err := factschema.EncodeDocumentationEvidencePacket(packet)
	if err != nil {
		return nil, fmt.Errorf("encode documentation evidence packet payload: %w", err)
	}
	copyOpenFields(
		encoded,
		payload,
		"finding",
		"unified_evidence",
		"document",
		"section",
		"bounded_excerpt",
		"current_truth",
		"evidence_refs",
		"truth",
		"permissions",
		"states",
	)
	return encode.JSONShapeMap(encoded), nil
}

func encodeOwnerRefs(values []OwnerRef) []documentationv1.OwnerRef {
	if values == nil {
		return nil
	}
	out := make([]documentationv1.OwnerRef, 0, len(values))
	for _, value := range values {
		out = append(out, documentationv1.OwnerRef{
			Kind:        encode.StringPtr(value.Kind),
			ID:          encode.StringPtr(value.ID),
			DisplayName: encode.StringPtr(value.DisplayName),
			SourceURI:   encode.StringPtr(value.SourceURI),
		})
	}
	return out
}

func EncodeACLSummary(value *ACLSummary) *documentationv1.ACLSummary {
	if value == nil {
		return nil
	}
	return &documentationv1.ACLSummary{
		Visibility:     encode.StringPtr(value.Visibility),
		ReaderGroups:   value.ReaderGroups,
		WriterGroups:   value.WriterGroups,
		ReaderUsers:    value.ReaderUsers,
		WriterUsers:    value.WriterUsers,
		HasInherited:   encode.BoolPtr(value.HasInherited),
		IsPartial:      encode.BoolPtr(value.IsPartial),
		SourceACLState: encode.StringPtr(value.SourceACLState),
		PartialReason:  encode.StringPtr(value.PartialReason),
	}
}

func EncodeEvidenceRefs(values []EvidenceRef) []documentationv1.EvidenceRef {
	if values == nil {
		return nil
	}
	out := make([]documentationv1.EvidenceRef, 0, len(values))
	for _, value := range values {
		out = append(out, documentationv1.EvidenceRef{
			Kind:       encode.StringPtr(value.Kind),
			ID:         encode.StringPtr(value.ID),
			URI:        encode.StringPtr(value.URI),
			Confidence: encode.StringPtr(value.Confidence),
		})
	}
	return out
}

func copyOpenFields(dst map[string]any, src map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func linkedEntityRefsFromMap(payload map[string]any, key string) []documentationv1.LinkedEntityRef {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]documentationv1.LinkedEntityRef, 0, len(values))
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, documentationv1.LinkedEntityRef{
			EntityType: encode.StringPtrFromMap(entry, "entity_type"),
			EntityID:   encode.StringPtrFromMap(entry, "entity_id"),
		})
	}
	return out
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
