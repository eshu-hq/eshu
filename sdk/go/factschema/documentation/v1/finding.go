// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Finding is the schema-version-1 typed payload for the
// "documentation_finding" fact kind: one read-only documentation truth
// finding.
//
// Unlike every other kind in this family, documentation_finding is emitted by
// go/internal/doctruth (the verifier), not a documentation collector, and has
// no reducer decode site at all — go/internal/reducer never loads this fact
// kind. The query read model (go/internal/query/documentation_read_model.go)
// reads it exclusively through raw fact_records.payload->>'field' SQL
// predicates/aggregates and Go map helpers over the decoded JSON map, never
// through this typed struct; per the DECODE-SITE CAVEAT that raw SQL/map
// read path is out of scope for this migration (mirroring the incident
// family's SQL-projected-fields precedent — those fields must be declared
// here, but the query layer itself is not converted). FindingID and
// FindingVersion form facts.DocumentationFindingStableID and are always set
// by doctruth's findingPayload; the remaining fields are the top-level keys
// the query SQL layer filters, aggregates, or reads
// (buildDocumentationFindingsSQL, documentation_finding_aggregates.go).
// permissions/states/evidence_packet_url are nested/computed nested objects
// the query layer builds and reads via generic map helpers rather than a
// fixed sub-shape, so they stay out of this struct rather than being
// half-modeled.
type Finding struct {
	// FindingID is the finding's identifier. Required — part of
	// facts.DocumentationFindingStableID; always set by doctruth's
	// findingPayload.
	FindingID string `json:"finding_id"`

	// FindingVersion is the finding's version. Required — part of
	// facts.DocumentationFindingStableID; always set by doctruth's
	// findingPayload.
	FindingVersion string `json:"finding_version"`

	// FindingType classifies the finding. Optional: read by the query SQL
	// layer's filter and aggregate (documentation_finding_aggregates.go).
	FindingType *string `json:"finding_type,omitempty"`

	// Status is the finding's status. Optional: read by the query SQL
	// layer's filter and aggregate.
	Status *string `json:"status,omitempty"`

	// TruthLevel is the finding's truth level. Optional: read by the query
	// SQL layer's filter and aggregate.
	TruthLevel *string `json:"truth_level,omitempty"`

	// FreshnessState is the finding's freshness state. Optional: read by the
	// query SQL layer's filter and aggregate.
	FreshnessState *string `json:"freshness_state,omitempty"`

	// SourceID is the documentation source the finding was extracted from.
	// Optional: read by the query SQL layer's filter.
	SourceID *string `json:"source_id,omitempty"`

	// DocumentID is the document the finding was extracted from. Optional:
	// read by the query SQL layer's filter.
	DocumentID *string `json:"document_id,omitempty"`

	// SectionID is the section the finding was extracted from. Optional.
	SectionID *string `json:"section_id,omitempty"`

	// ClaimID is the claim candidate the finding verifies. Optional.
	ClaimID *string `json:"claim_id,omitempty"`

	// ClaimType classifies the verified claim. Optional.
	ClaimType *string `json:"claim_type,omitempty"`

	// ClaimText is the verified claim's text. Optional.
	ClaimText *string `json:"claim_text,omitempty"`

	// NormalizedClaim is the claim text in normalized form. Optional.
	NormalizedClaim *string `json:"normalized_claim,omitempty"`

	// Summary is a human-readable summary of the finding. Optional.
	Summary *string `json:"summary,omitempty"`

	// EvidencePacketID references the finding's evidence packet. Optional.
	EvidencePacketID *string `json:"evidence_packet_id,omitempty"`

	// ClaimByteOffset is the document-absolute byte offset of the claim
	// text, when determined during extraction. Optional.
	ClaimByteOffset *int `json:"claim_byte_offset,omitempty"`

	// ClaimByteLength is the byte length of the claim text at
	// ClaimByteOffset, when determined during extraction. Optional.
	ClaimByteLength *int `json:"claim_byte_length,omitempty"`
}

// EvidencePacket is the schema-version-1 typed payload for the
// "documentation_evidence_packet" fact kind: one immutable documentation
// evidence packet.
//
// Like Finding, this kind is emitted by go/internal/doctruth (the verifier),
// not a documentation collector, and has no reducer decode site — only the
// query read model reads it, exclusively through raw
// fact_records.payload->>'field' SQL predicates
// (go/internal/query/documentation_packet_read_model.go) and JSONB
// containment/target-match reads
// (documentation_target_read_model.go's documentationPayloadMatchesTargetRef,
// documentation_authz.go, documentation_source_only.go). PacketID and
// FindingID together form facts.DocumentationEvidencePacketStableID and are
// the two fields the query SQL layer's WHERE clauses filter on
// (buildDocumentationEvidencePacketByFindingSQL,
// buildDocumentationEvidencePacketByPacketSQL); the packet's
// unified_evidence/finding sub-objects are deeply nested, verifier-internal
// shapes the query layer reads through generic map helpers rather than a
// fixed sub-shape, so they stay out of this struct rather than being
// half-modeled (mirroring how Finding excludes permissions/states).
type EvidencePacket struct {
	// PacketID is the evidence packet's identifier. Required — part of
	// facts.DocumentationEvidencePacketStableID; the query SQL layer's
	// by-packet lookup key.
	PacketID string `json:"packet_id"`

	// PacketVersion is the packet's version. Optional.
	PacketVersion *string `json:"packet_version,omitempty"`

	// GeneratedAt is the packet's generation timestamp, source-native string
	// form. Optional.
	GeneratedAt *string `json:"generated_at,omitempty"`

	// FindingID references the finding this packet supports. Required —
	// part of facts.DocumentationEvidencePacketStableID; the query SQL
	// layer's by-finding lookup key
	// (COALESCE(payload->>'finding_id', payload->'finding'->>'finding_id')).
	FindingID string `json:"finding_id"`

	// LinkedEntities lists the entity refs the packet's evidence links to.
	// Optional: doctruth's evidencePacketPayload always sets this key, but
	// always to an empty slice today (no verifier path populates it yet),
	// so a present empty array is the only observed value. Read by the
	// query layer's target-match helper
	// (documentationRefListMatchesTarget(payload["linked_entities"], ref,
	// "entity_type", "entity_id")) and JSONB containment/authorization
	// predicates. Declared so a future schema change cannot silently drop
	// a field the query layer depends on, mirroring the incident family's
	// SQL-projected-fields precedent.
	LinkedEntities []LinkedEntityRef `json:"linked_entities,omitempty"`
}

// LinkedEntityRef is one entity reference inside an EvidencePacket's
// LinkedEntities list. It uses the entity_type/entity_id key pair the query
// layer's target-match helper reads
// (documentation_target_read_model.go:199,285) — distinct from EvidenceRef's
// kind/id key pair, which candidate_refs/evidence_refs use on the other
// documentation kinds.
type LinkedEntityRef struct {
	// EntityType classifies the linked entity (for example "service",
	// "repository"). Optional.
	EntityType *string `json:"entity_type,omitempty"`

	// EntityID is the linked entity's identifier. Optional.
	EntityID *string `json:"entity_id,omitempty"`
}
