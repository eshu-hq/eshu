// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

const (
	// DocumentationSourceFactKind identifies one documentation source.
	DocumentationSourceFactKind = "documentation_source"
	// DocumentationDocumentFactKind identifies one documentation document.
	DocumentationDocumentFactKind = "documentation_document"
	// DocumentationSectionFactKind identifies one section in a document revision.
	DocumentationSectionFactKind = "documentation_section"
	// DocumentationLinkFactKind identifies one link observed in documentation.
	DocumentationLinkFactKind = "documentation_link"
	// DocumentationEntityMentionFactKind identifies one entity mention in documentation.
	DocumentationEntityMentionFactKind = "documentation_entity_mention"
	// DocumentationClaimCandidateFactKind identifies one non-authoritative documentation claim candidate.
	DocumentationClaimCandidateFactKind = "documentation_claim_candidate"
	// DocumentationFindingFactKind identifies one read-only documentation truth finding.
	DocumentationFindingFactKind = "documentation_finding"
	// DocumentationEvidencePacketFactKind identifies one immutable documentation evidence packet.
	DocumentationEvidencePacketFactKind = "documentation_evidence_packet"

	// DocumentationFactSchemaVersion is the first documentation fact schema for
	// documentation fact kinds that have not introduced kind-specific schema
	// versions.
	DocumentationFactSchemaVersion = "1.0.0"
	// DocumentationSectionFactSchemaVersion is the documentation section schema
	// version that adds source-native content fields for updater diffing.
	DocumentationSectionFactSchemaVersion = "1.1.0"
)

// DocumentationFactKinds returns every documentation fact kind owned by Eshu
// core. It is the single source for the documentation family so the core
// fact-kind registry and the schema-version registry cannot drift.
func DocumentationFactKinds() []string {
	return []string{
		DocumentationSourceFactKind,
		DocumentationDocumentFactKind,
		DocumentationSectionFactKind,
		DocumentationLinkFactKind,
		DocumentationEntityMentionFactKind,
		DocumentationClaimCandidateFactKind,
		DocumentationFindingFactKind,
		DocumentationEvidencePacketFactKind,
	}
}

// DocumentationSchemaVersion returns the schema version a core consumer supports
// for a documentation fact kind. The section kind carries its own version; the
// remaining documentation kinds share the base documentation schema version.
func DocumentationSchemaVersion(factKind string) (string, bool) {
	switch factKind {
	case DocumentationSectionFactKind:
		return DocumentationSectionFactSchemaVersion, true
	case DocumentationSourceFactKind,
		DocumentationDocumentFactKind,
		DocumentationLinkFactKind,
		DocumentationEntityMentionFactKind,
		DocumentationClaimCandidateFactKind,
		DocumentationFindingFactKind,
		DocumentationEvidencePacketFactKind:
		return DocumentationFactSchemaVersion, true
	default:
		return "", false
	}
}

const (
	// DocumentationMentionResolutionExact means the mention resolved to one entity.
	DocumentationMentionResolutionExact = "exact"
	// DocumentationMentionResolutionAmbiguous means the mention has multiple candidate entities.
	DocumentationMentionResolutionAmbiguous = "ambiguous"
	// DocumentationMentionResolutionUnmatched means the mention did not resolve to an entity.
	DocumentationMentionResolutionUnmatched = "unmatched"

	// DocumentationClaimAuthorityDocumentEvidence marks claims as evidence about document text only.
	DocumentationClaimAuthorityDocumentEvidence = "document_evidence"
)

// DocumentationOwnerRef identifies an owner reference reported by a documentation source.
type DocumentationOwnerRef struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	SourceURI   string `json:"source_uri,omitempty"`
}

// DocumentationACLSummary records bounded access metadata reported by a documentation source.
type DocumentationACLSummary struct {
	Visibility   string   `json:"visibility"`
	ReaderGroups []string `json:"reader_groups,omitempty"`
	WriterGroups []string `json:"writer_groups,omitempty"`
	ReaderUsers  []string `json:"reader_users,omitempty"`
	WriterUsers  []string `json:"writer_users,omitempty"`
	HasInherited bool     `json:"has_inherited,omitempty"`
	IsPartial    bool     `json:"is_partial,omitempty"`
	// SourceACLState is the bounded, additive source-ACL-state observation for
	// this documentation content/evidence fact. It uses the
	// allowed|denied|partial|missing|stale vocabulary (see the
	// SourceACLState* constants). It is set only when the collector observes a
	// real access-posture signal at the origin, and is omitted entirely when no
	// ACL signal was observed (absence means "no ACL claim"). A denied,
	// partial, missing, or stale observation is never upgraded to allowed.
	SourceACLState string `json:"source_acl_state,omitempty"`
	PartialReason  string `json:"partial_reason,omitempty"`
}

// DocumentationEvidenceRef references evidence used by a documentation payload.
type DocumentationEvidenceRef struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	URI        string `json:"uri,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

// DocumentationSourcePayload describes a documentation source such as Confluence or Git Markdown.
type DocumentationSourcePayload struct {
	SourceID       string                   `json:"source_id"`
	SourceSystem   string                   `json:"source_system"`
	ExternalID     string                   `json:"external_id"`
	DisplayName    string                   `json:"display_name,omitempty"`
	BaseURI        string                   `json:"base_uri,omitempty"`
	SourceType     string                   `json:"source_type,omitempty"`
	Labels         []string                 `json:"labels,omitempty"`
	OwnerRefs      []DocumentationOwnerRef  `json:"owner_refs,omitempty"`
	ACLSummary     *DocumentationACLSummary `json:"acl_summary,omitempty"`
	SourceMetadata map[string]string        `json:"source_metadata,omitempty"`
}

// DocumentationDocumentPayload describes one source-neutral documentation document revision.
type DocumentationDocumentPayload struct {
	SourceID          string                   `json:"source_id"`
	DocumentID        string                   `json:"document_id"`
	ExternalID        string                   `json:"external_id"`
	RevisionID        string                   `json:"revision_id"`
	CanonicalURI      string                   `json:"canonical_uri,omitempty"`
	Title             string                   `json:"title,omitempty"`
	ParentDocumentID  string                   `json:"parent_document_id,omitempty"`
	DocumentType      string                   `json:"document_type,omitempty"`
	Format            string                   `json:"format,omitempty"`
	Language          string                   `json:"language,omitempty"`
	Labels            []string                 `json:"labels,omitempty"`
	OwnerRefs         []DocumentationOwnerRef  `json:"owner_refs,omitempty"`
	ACLSummary        *DocumentationACLSummary `json:"acl_summary,omitempty"`
	SourceMetadata    map[string]string        `json:"source_metadata,omitempty"`
	ContentHash       string                   `json:"content_hash,omitempty"`
	DocumentCreatedAt string                   `json:"document_created_at,omitempty"`
	DocumentUpdatedAt string                   `json:"document_updated_at,omitempty"`
}

// DocumentationSectionPayload describes one bounded section in a document revision.
type DocumentationSectionPayload struct {
	DocumentID       string            `json:"document_id"`
	RevisionID       string            `json:"revision_id"`
	SectionID        string            `json:"section_id"`
	ParentSectionID  string            `json:"parent_section_id,omitempty"`
	SectionAnchor    string            `json:"section_anchor,omitempty"`
	HeadingText      string            `json:"heading_text,omitempty"`
	OrdinalPath      []int             `json:"ordinal_path,omitempty"`
	Content          string            `json:"content,omitempty"`
	ContentFormat    string            `json:"content_format,omitempty"`
	TextHash         string            `json:"text_hash,omitempty"`
	ExcerptHash      string            `json:"excerpt_hash,omitempty"`
	SourceStartRef   string            `json:"source_start_ref,omitempty"`
	SourceEndRef     string            `json:"source_end_ref,omitempty"`
	SourceMetadata   map[string]string `json:"source_metadata,omitempty"`
	ContainsWarnings bool              `json:"contains_warnings,omitempty"`
}

// DocumentationLinkPayload describes one link observed in a document section.
type DocumentationLinkPayload struct {
	DocumentID     string            `json:"document_id"`
	RevisionID     string            `json:"revision_id"`
	SectionID      string            `json:"section_id,omitempty"`
	LinkID         string            `json:"link_id"`
	TargetURI      string            `json:"target_uri"`
	TargetKind     string            `json:"target_kind,omitempty"`
	AnchorTextHash string            `json:"anchor_text_hash,omitempty"`
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}

// DocumentationEntityMentionPayload describes one possible entity mention in documentation.
type DocumentationEntityMentionPayload struct {
	DocumentID       string                     `json:"document_id"`
	RevisionID       string                     `json:"revision_id,omitempty"`
	SectionID        string                     `json:"section_id"`
	MentionID        string                     `json:"mention_id"`
	MentionText      string                     `json:"mention_text"`
	MentionKind      string                     `json:"mention_kind"`
	ResolutionStatus string                     `json:"resolution_status"`
	CandidateRefs    []DocumentationEvidenceRef `json:"candidate_refs,omitempty"`
	ExcerptHash      string                     `json:"excerpt_hash,omitempty"`
	// ACLSummary carries the bounded source access posture observed for the
	// document this mention was extracted from, propagated verbatim from the
	// owning source/document fact (see DocumentationACLSummary.SourceACLState).
	// It is additive evidence metadata: a mention inherits its document's
	// observed source_acl_state so the docs-evidence projection and readbacks
	// carry the posture end-to-end. It is omitted when the document asserted no
	// bounded ACL claim (absence means "no ACL claim"); a denied, partial,
	// missing, or stale observation is never upgraded to allowed. It is
	// factual propagation only and never decides disclosure or enforcement.
	ACLSummary     *DocumentationACLSummary `json:"acl_summary,omitempty"`
	SourceMetadata map[string]string        `json:"source_metadata,omitempty"`
}

// DocumentationClaimCandidatePayload describes a non-authoritative claim found in documentation.
type DocumentationClaimCandidatePayload struct {
	DocumentID       string                     `json:"document_id"`
	RevisionID       string                     `json:"revision_id,omitempty"`
	SectionID        string                     `json:"section_id"`
	ClaimID          string                     `json:"claim_id"`
	ClaimType        string                     `json:"claim_type"`
	ClaimText        string                     `json:"claim_text"`
	ClaimHash        string                     `json:"claim_hash"`
	ExcerptHash      string                     `json:"excerpt_hash,omitempty"`
	SubjectMentionID string                     `json:"subject_mention_id,omitempty"`
	ObjectMentionIDs []string                   `json:"object_mention_ids,omitempty"`
	EvidenceRefs     []DocumentationEvidenceRef `json:"evidence_refs,omitempty"`
	Authority        string                     `json:"authority"`
	// ACLSummary carries the bounded source access posture observed for the
	// document this claim candidate was extracted from, propagated verbatim
	// from the owning source/document fact (see
	// DocumentationACLSummary.SourceACLState). It is additive evidence
	// metadata: a claim inherits its document's observed source_acl_state so
	// the docs-evidence projection and readbacks carry the posture end-to-end.
	// It is omitted when the document asserted no bounded ACL claim (absence
	// means "no ACL claim"); a denied, partial, missing, or stale observation
	// is never upgraded to allowed. It is factual propagation only and never
	// decides disclosure or enforcement.
	ACLSummary     *DocumentationACLSummary `json:"acl_summary,omitempty"`
	SourceMetadata map[string]string        `json:"source_metadata,omitempty"`
}

// DocumentationSourceStableID returns a stable ID for a documentation source.
func DocumentationSourceStableID(payload DocumentationSourcePayload) string {
	return StableID(DocumentationSourceFactKind, map[string]any{
		"source_id":     payload.SourceID,
		"source_system": payload.SourceSystem,
		"external_id":   payload.ExternalID,
	})
}

// DocumentationDocumentStableID returns a stable ID for a documentation document revision.
func DocumentationDocumentStableID(payload DocumentationDocumentPayload) string {
	return StableID(DocumentationDocumentFactKind, map[string]any{
		"source_id":   payload.SourceID,
		"document_id": payload.DocumentID,
		"external_id": payload.ExternalID,
		"revision_id": payload.RevisionID,
	})
}

// DocumentationSectionStableID returns a stable ID for a documentation section revision.
func DocumentationSectionStableID(payload DocumentationSectionPayload) string {
	return StableID(DocumentationSectionFactKind, map[string]any{
		"document_id":    payload.DocumentID,
		"revision_id":    payload.RevisionID,
		"section_id":     payload.SectionID,
		"section_anchor": payload.SectionAnchor,
		"ordinal_path":   payload.OrdinalPath,
		"text_hash":      payload.TextHash,
		"excerpt_hash":   payload.ExcerptHash,
	})
}

// DocumentationLinkStableID returns a stable ID for one documentation link.
func DocumentationLinkStableID(payload DocumentationLinkPayload) string {
	return StableID(DocumentationLinkFactKind, map[string]any{
		"document_id": payload.DocumentID,
		"revision_id": payload.RevisionID,
		"section_id":  payload.SectionID,
		"link_id":     payload.LinkID,
		"target_uri":  payload.TargetURI,
	})
}

// DocumentationEntityMentionStableID returns a stable ID for one entity mention.
func DocumentationEntityMentionStableID(payload DocumentationEntityMentionPayload) string {
	return StableID(DocumentationEntityMentionFactKind, map[string]any{
		"document_id":  payload.DocumentID,
		"revision_id":  payload.RevisionID,
		"section_id":   payload.SectionID,
		"mention_id":   payload.MentionID,
		"excerpt_hash": payload.ExcerptHash,
	})
}

// DocumentationClaimCandidateStableID returns a stable ID for one documentation claim candidate.
func DocumentationClaimCandidateStableID(payload DocumentationClaimCandidatePayload) string {
	return StableID(DocumentationClaimCandidateFactKind, map[string]any{
		"document_id":  payload.DocumentID,
		"revision_id":  payload.RevisionID,
		"section_id":   payload.SectionID,
		"claim_id":     payload.ClaimID,
		"claim_hash":   payload.ClaimHash,
		"excerpt_hash": payload.ExcerptHash,
	})
}

// DocumentationFindingStableID returns a stable ID for one documentation finding.
func DocumentationFindingStableID(findingID, findingVersion string) string {
	return StableID(DocumentationFindingFactKind, map[string]any{
		"finding_id":      findingID,
		"finding_version": findingVersion,
	})
}

// DocumentationEvidencePacketStableID returns a stable ID for one evidence packet.
func DocumentationEvidencePacketStableID(packetID, packetVersion string) string {
	return StableID(DocumentationEvidencePacketFactKind, map[string]any{
		"packet_id":      packetID,
		"packet_version": packetVersion,
	})
}
