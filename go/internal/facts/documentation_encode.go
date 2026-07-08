// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"fmt"
	"reflect"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
	documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"
)

// EncodeDocumentationSource maps the internal documentation source identity
// payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeDocumentationSource(payload DocumentationSourcePayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationSource(documentationv1.Source{
		SourceID:       payload.SourceID,
		SourceSystem:   payload.SourceSystem,
		ExternalID:     payload.ExternalID,
		DisplayName:    stringPtr(payload.DisplayName),
		BaseURI:        stringPtr(payload.BaseURI),
		SourceType:     stringPtr(payload.SourceType),
		Labels:         payload.Labels,
		OwnerRefs:      encodeDocumentationOwnerRefs(payload.OwnerRefs),
		ACLSummary:     encodeDocumentationACLSummary(payload.ACLSummary),
		SourceMetadata: payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeDocumentationDocument maps the internal documentation document
// identity payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeDocumentationDocument(payload DocumentationDocumentPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationDocument(documentationv1.Document{
		DocumentID:        payload.DocumentID,
		SourceID:          stringPtr(payload.SourceID),
		ExternalID:        stringPtr(payload.ExternalID),
		RevisionID:        stringPtr(payload.RevisionID),
		CanonicalURI:      stringPtr(payload.CanonicalURI),
		Title:             stringPtr(payload.Title),
		ParentDocumentID:  stringPtr(payload.ParentDocumentID),
		DocumentType:      stringPtr(payload.DocumentType),
		Format:            stringPtr(payload.Format),
		Language:          stringPtr(payload.Language),
		Labels:            payload.Labels,
		OwnerRefs:         encodeDocumentationOwnerRefs(payload.OwnerRefs),
		ACLSummary:        encodeDocumentationACLSummary(payload.ACLSummary),
		SourceMetadata:    payload.SourceMetadata,
		ContentHash:       stringPtr(payload.ContentHash),
		DocumentCreatedAt: stringPtr(payload.DocumentCreatedAt),
		DocumentUpdatedAt: stringPtr(payload.DocumentUpdatedAt),
	})
	return jsonShapePayload(encoded, err)
}

// EncodeDocumentationSection maps the internal documentation section identity
// payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeDocumentationSection(payload DocumentationSectionPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationSection(documentationv1.Section{
		DocumentID:       payload.DocumentID,
		RevisionID:       payload.RevisionID,
		SectionID:        payload.SectionID,
		ParentSectionID:  stringPtr(payload.ParentSectionID),
		SectionAnchor:    stringPtr(payload.SectionAnchor),
		HeadingText:      stringPtr(payload.HeadingText),
		OrdinalPath:      payload.OrdinalPath,
		Content:          stringPtr(payload.Content),
		ContentFormat:    stringPtr(payload.ContentFormat),
		TextHash:         stringPtr(payload.TextHash),
		ExcerptHash:      stringPtr(payload.ExcerptHash),
		SourceStartRef:   stringPtr(payload.SourceStartRef),
		SourceEndRef:     stringPtr(payload.SourceEndRef),
		SourceMetadata:   payload.SourceMetadata,
		ContainsWarnings: boolPtr(payload.ContainsWarnings),
	})
	return jsonShapePayload(encoded, err)
}

// EncodeDocumentationLink maps the internal documentation link identity
// payload to the SDK factschema encoder used for emitted fact payloads.
func EncodeDocumentationLink(payload DocumentationLinkPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationLink(documentationv1.Link{
		DocumentID:     payload.DocumentID,
		RevisionID:     stringPtr(payload.RevisionID),
		SectionID:      stringPtr(payload.SectionID),
		LinkID:         payload.LinkID,
		TargetURI:      payload.TargetURI,
		TargetKind:     stringPtr(payload.TargetKind),
		AnchorTextHash: stringPtr(payload.AnchorTextHash),
		SourceMetadata: payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeDocumentationEntityMention maps the internal documentation entity
// mention identity payload to the SDK factschema encoder used for emitted fact
// payloads.
func EncodeDocumentationEntityMention(payload DocumentationEntityMentionPayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationEntityMention(documentationv1.EntityMention{
		DocumentID:       payload.DocumentID,
		RevisionID:       stringPtr(payload.RevisionID),
		SectionID:        payload.SectionID,
		MentionID:        stringPtr(payload.MentionID),
		MentionText:      stringPtr(payload.MentionText),
		MentionKind:      stringPtr(payload.MentionKind),
		ResolutionStatus: payload.ResolutionStatus,
		CandidateRefs:    encodeDocumentationEvidenceRefs(payload.CandidateRefs),
		ExcerptHash:      stringPtr(payload.ExcerptHash),
		ACLSummary:       encodeDocumentationACLSummary(payload.ACLSummary),
		SourceMetadata:   payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeDocumentationClaimCandidate maps the internal documentation claim
// candidate identity payload to the SDK factschema encoder used for emitted
// fact payloads.
func EncodeDocumentationClaimCandidate(payload DocumentationClaimCandidatePayload) (map[string]any, error) {
	encoded, err := factschema.EncodeDocumentationClaimCandidate(documentationv1.ClaimCandidate{
		DocumentID:       payload.DocumentID,
		RevisionID:       stringPtr(payload.RevisionID),
		SectionID:        payload.SectionID,
		ClaimID:          payload.ClaimID,
		ClaimType:        payload.ClaimType,
		ClaimText:        payload.ClaimText,
		ClaimHash:        payload.ClaimHash,
		ExcerptHash:      stringPtr(payload.ExcerptHash),
		SubjectMentionID: stringPtr(payload.SubjectMentionID),
		ObjectMentionIDs: payload.ObjectMentionIDs,
		EvidenceRefs:     encodeDocumentationEvidenceRefs(payload.EvidenceRefs),
		Authority:        payload.Authority,
		ACLSummary:       encodeDocumentationACLSummary(payload.ACLSummary),
		SourceMetadata:   payload.SourceMetadata,
	})
	return jsonShapePayload(encoded, err)
}

// EncodeDocumentationFinding maps the verifier's documentation finding map
// through the SDK factschema encoder and preserves verifier-owned extension
// fields that the typed contract intentionally leaves open.
func EncodeDocumentationFinding(payload map[string]any) (map[string]any, error) {
	finding := documentationv1.Finding{
		FindingID:        stringValue(payload, "finding_id"),
		FindingVersion:   stringValue(payload, "finding_version"),
		FindingType:      stringPtrFromMap(payload, "finding_type"),
		Status:           stringPtrFromMap(payload, "status"),
		TruthLevel:       stringPtrFromMap(payload, "truth_level"),
		FreshnessState:   stringPtrFromMap(payload, "freshness_state"),
		SourceID:         stringPtrFromMap(payload, "source_id"),
		DocumentID:       stringPtrFromMap(payload, "document_id"),
		SectionID:        stringPtrFromMap(payload, "section_id"),
		ClaimID:          stringPtrFromMap(payload, "claim_id"),
		ClaimType:        stringPtrFromMap(payload, "claim_type"),
		ClaimText:        stringPtrFromMap(payload, "claim_text"),
		NormalizedClaim:  stringPtrFromMap(payload, "normalized_claim"),
		Summary:          stringPtrFromMap(payload, "summary"),
		EvidencePacketID: stringPtrFromMap(payload, "evidence_packet_id"),
		ClaimByteOffset:  intPtrFromMap(payload, "claim_byte_offset"),
		ClaimByteLength:  intPtrFromMap(payload, "claim_byte_length"),
	}
	encoded, err := factschema.EncodeDocumentationFinding(finding)
	if err != nil {
		return nil, fmt.Errorf("encode documentation finding payload: %w", err)
	}
	copyOpenDocumentationFields(encoded, payload, "evidence_packet_url", "permissions", "states")
	return normalizeJSONShapeMap(encoded), nil
}

// EncodeDocumentationEvidencePacket maps the verifier's documentation evidence
// packet map through the SDK factschema encoder and preserves verifier-owned
// extension fields that the typed contract intentionally leaves open.
func EncodeDocumentationEvidencePacket(payload map[string]any) (map[string]any, error) {
	packet := documentationv1.EvidencePacket{
		PacketID:       stringValue(payload, "packet_id"),
		PacketVersion:  stringPtrFromMap(payload, "packet_version"),
		GeneratedAt:    stringPtrFromMap(payload, "generated_at"),
		FindingID:      stringValue(payload, "finding_id"),
		LinkedEntities: linkedEntityRefsFromMap(payload, "linked_entities"),
	}
	encoded, err := factschema.EncodeDocumentationEvidencePacket(packet)
	if err != nil {
		return nil, fmt.Errorf("encode documentation evidence packet payload: %w", err)
	}
	copyOpenDocumentationFields(
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
	return normalizeJSONShapeMap(encoded), nil
}

func encodeDocumentationOwnerRefs(values []DocumentationOwnerRef) []documentationv1.OwnerRef {
	if values == nil {
		return nil
	}
	out := make([]documentationv1.OwnerRef, 0, len(values))
	for _, value := range values {
		out = append(out, documentationv1.OwnerRef{
			Kind:        stringPtr(value.Kind),
			ID:          stringPtr(value.ID),
			DisplayName: stringPtr(value.DisplayName),
			SourceURI:   stringPtr(value.SourceURI),
		})
	}
	return out
}

func encodeDocumentationACLSummary(value *DocumentationACLSummary) *documentationv1.ACLSummary {
	if value == nil {
		return nil
	}
	return &documentationv1.ACLSummary{
		Visibility:     stringPtr(value.Visibility),
		ReaderGroups:   value.ReaderGroups,
		WriterGroups:   value.WriterGroups,
		ReaderUsers:    value.ReaderUsers,
		WriterUsers:    value.WriterUsers,
		HasInherited:   boolPtr(value.HasInherited),
		IsPartial:      boolPtr(value.IsPartial),
		SourceACLState: stringPtr(value.SourceACLState),
		PartialReason:  stringPtr(value.PartialReason),
	}
}

func encodeDocumentationEvidenceRefs(values []DocumentationEvidenceRef) []documentationv1.EvidenceRef {
	if values == nil {
		return nil
	}
	out := make([]documentationv1.EvidenceRef, 0, len(values))
	for _, value := range values {
		out = append(out, documentationv1.EvidenceRef{
			Kind:       stringPtr(value.Kind),
			ID:         stringPtr(value.ID),
			URI:        stringPtr(value.URI),
			Confidence: stringPtr(value.Confidence),
		})
	}
	return out
}

func copyOpenDocumentationFields(dst map[string]any, src map[string]any, keys ...string) {
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
			EntityType: stringPtrFromMap(entry, "entity_type"),
			EntityID:   stringPtrFromMap(entry, "entity_id"),
		})
	}
	return out
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	if !value {
		return nil
	}
	return &value
}

func stringValue(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func stringPtrFromMap(payload map[string]any, key string) *string {
	value := stringValue(payload, key)
	if value == "" {
		return nil
	}
	return &value
}

func intPtrFromMap(payload map[string]any, key string) *int {
	switch value := payload[key].(type) {
	case int:
		if value == 0 {
			return nil
		}
		return &value
	case int64:
		if value == 0 {
			return nil
		}
		converted := int(value)
		return &converted
	case float64:
		if value == 0 {
			return nil
		}
		converted := int(value)
		if float64(converted) != value {
			return nil
		}
		return &converted
	default:
		return nil
	}
}

func jsonShapePayload(payload map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	return normalizeJSONShapeMap(payload), nil
}

func normalizeJSONShapeMap(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = normalizeJSONShapeValue(value)
	}
	return out
}

func normalizeJSONShapeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeJSONShapeMap(typed)
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, normalizeJSONShapeValue(value))
		}
		return out
	case []string:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, value)
		}
		return out
	case []int:
		out := make([]any, 0, len(typed))
		for _, value := range typed {
			out = append(out, float64(value))
		}
		return out
	case int:
		return float64(typed)
	case int8:
		return float64(typed)
	case int16:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint:
		return float64(typed)
	case uint8:
		return float64(typed)
	case uint16:
		return float64(typed)
	case uint32:
		return float64(typed)
	case uint64:
		return float64(typed)
	}
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice {
		return value
	}
	out := make([]any, 0, reflected.Len())
	for i := 0; i < reflected.Len(); i++ {
		out = append(out, normalizeJSONShapeValue(reflected.Index(i).Interface()))
	}
	return out
}
