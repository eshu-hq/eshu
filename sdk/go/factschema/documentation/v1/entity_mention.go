// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// EntityMention is the schema-version-1 typed payload for the
// "documentation_entity_mention" fact kind: one possible entity mention
// observed in documentation.
//
// This is the ONLY documentation kind with a real reducer identity read
// today: ExtractDocumentationEdgeRows
// (go/internal/reducer/documentation_edge_materialization.go) requires
// ResolutionStatus, DocumentID, and SectionID to be present (and non-blank
// after TrimSpace) to project a DOCUMENTS edge, and reads exactly one entry
// out of CandidateRefs when len(CandidateRefs) == 1. MentionID and
// ExcerptHash additionally form part of facts.DocumentationEntityMentionStableID
// but are not read by the edge extractor itself, so they stay optional here —
// this mirrors the sbom_attestation family's rule that a struct's required
// set is what the READING handler treats as mandatory identity, not every
// field that happens to feed a stable-ID hash. A mention missing
// ResolutionStatus, DocumentID, or SectionID dead-letters as input_invalid
// instead of the pre-typing behavior of silently skipping the fact with no
// operator signal (payloadStr returning "" and the handler's early
// continue).
type EntityMention struct {
	// DocumentID is the owning document's identifier. Required — the edge
	// extractor's join key from a mention to its owning
	// DocumentationSection node (documentationSectionNodeUID).
	DocumentID string `json:"document_id"`

	// RevisionID is the owning document revision's identifier. Optional:
	// part of facts.DocumentationEntityMentionStableID when present, but not
	// read by the edge extractor.
	RevisionID *string `json:"revision_id,omitempty"`

	// SectionID is the owning section's identifier. Required — the edge
	// extractor's join key from a mention to its owning
	// DocumentationSection node.
	SectionID string `json:"section_id"`

	// MentionID is the mention's identifier within the section. Optional:
	// part of facts.DocumentationEntityMentionStableID when present, but not
	// read by the edge extractor.
	MentionID *string `json:"mention_id,omitempty"`

	// MentionText is the mention's observed text. Optional.
	MentionText *string `json:"mention_text,omitempty"`

	// MentionKind classifies the mention (for example "service",
	// "function"). Optional: read by the edge extractor to populate the
	// projected edge's mention_kind field, but a blank value still projects
	// (only ResolutionStatus/DocumentID/SectionID gate the edge).
	MentionKind *string `json:"mention_kind,omitempty"`

	// ResolutionStatus is the mention's resolution outcome
	// (facts.DocumentationMentionResolutionExact/Ambiguous/Unmatched).
	// Required — the edge extractor only projects an edge when this equals
	// "exact"; ambiguous, unmatched, and any other value are valid decodes
	// that simply produce no edge, preserving correlation truth.
	ResolutionStatus string `json:"resolution_status"`

	// CandidateRefs lists the mention's candidate entity references. The
	// edge extractor only projects an edge when exactly one entry is
	// present; more or fewer is a valid decode that produces no edge (an
	// ambiguous or unresolved mention). Optional.
	CandidateRefs []EvidenceRef `json:"candidate_refs,omitempty"`

	// ExcerptHash is a fingerprint for a bounded excerpt around the mention.
	// Optional: part of facts.DocumentationEntityMentionStableID when
	// present, but not read by the edge extractor.
	ExcerptHash *string `json:"excerpt_hash,omitempty"`

	// ACLSummary carries the bounded source access posture propagated from
	// the owning document/source fact. Optional.
	ACLSummary *ACLSummary `json:"acl_summary,omitempty"`

	// SourceMetadata carries source-native metadata as a flat string map.
	// Optional.
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}

// ClaimCandidate is the schema-version-1 typed payload for the
// "documentation_claim_candidate" fact kind: one non-authoritative claim
// found in documentation.
//
// This kind is TYPED-BUT-NOT-YET-CONSUMED by any reducer decode site: the
// query read model and go/internal/doctruth's extractor reference it only by
// fact_kind column / stable-ID construction, never a structured field decode.
// DocumentID, SectionID, ClaimID, ClaimText, ClaimHash, and Authority form
// facts.DocumentationClaimCandidateStableID or are always emitted by the
// collector (go/internal/facts.DocumentationClaimCandidatePayload has no
// omitempty on ClaimType/ClaimText/ClaimHash/Authority), so they are modeled
// required ahead of a future consumer, mirroring the family's typed-but-
// deferred precedent.
type ClaimCandidate struct {
	// DocumentID is the owning document's identifier. Required — part of
	// facts.DocumentationClaimCandidateStableID.
	DocumentID string `json:"document_id"`

	// RevisionID is the owning document revision's identifier. Optional:
	// part of facts.DocumentationClaimCandidateStableID when present.
	RevisionID *string `json:"revision_id,omitempty"`

	// SectionID is the owning section's identifier. Required — part of
	// facts.DocumentationClaimCandidateStableID.
	SectionID string `json:"section_id"`

	// ClaimID is the claim's identifier within the section. Required — part
	// of facts.DocumentationClaimCandidateStableID.
	ClaimID string `json:"claim_id"`

	// ClaimType classifies the claim. Required — always emitted by the
	// collector (go/internal/facts.DocumentationClaimCandidatePayload has no
	// omitempty on this field).
	ClaimType string `json:"claim_type"`

	// ClaimText is the claim's observed text. Required — always emitted by
	// the collector.
	ClaimText string `json:"claim_text"`

	// ClaimHash is a fingerprint for the claim text. Required — part of
	// facts.DocumentationClaimCandidateStableID.
	ClaimHash string `json:"claim_hash"`

	// ExcerptHash is a fingerprint for a bounded excerpt around the claim.
	// Optional: part of facts.DocumentationClaimCandidateStableID when
	// present.
	ExcerptHash *string `json:"excerpt_hash,omitempty"`

	// SubjectMentionID references the entity mention this claim is about.
	// Optional.
	SubjectMentionID *string `json:"subject_mention_id,omitempty"`

	// ObjectMentionIDs references the entity mentions this claim's object
	// refers to. Optional.
	ObjectMentionIDs []string `json:"object_mention_ids,omitempty"`

	// EvidenceRefs lists evidence supporting the claim. Optional.
	EvidenceRefs []EvidenceRef `json:"evidence_refs,omitempty"`

	// Authority classifies the claim's evidentiary authority (for example
	// facts.DocumentationClaimAuthorityDocumentEvidence). Required — always
	// emitted by the collector.
	Authority string `json:"authority"`

	// ACLSummary carries the bounded source access posture propagated from
	// the owning document/source fact. Optional.
	ACLSummary *ACLSummary `json:"acl_summary,omitempty"`

	// SourceMetadata carries source-native metadata as a flat string map.
	// Optional.
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}
