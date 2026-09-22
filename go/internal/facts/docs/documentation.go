// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import "github.com/eshu-hq/eshu/go/internal/facts/encode"

const (
	// SourceFactKind identifies one documentation source.
	SourceFactKind = "documentation_source"
	// DocumentFactKind identifies one documentation document.
	DocumentFactKind = "documentation_document"
	// SectionFactKind identifies one section in a document revision.
	SectionFactKind = "documentation_section"
	// LinkFactKind identifies one link observed in documentation.
	LinkFactKind = "documentation_link"
	// EntityMentionFactKind identifies one entity mention in documentation.
	EntityMentionFactKind = "documentation_entity_mention"
	// ClaimCandidateFactKind identifies one non-authoritative documentation claim candidate.
	ClaimCandidateFactKind = "documentation_claim_candidate"
	// FindingFactKind identifies one read-only documentation truth finding.
	FindingFactKind = "documentation_finding"
	// EvidencePacketFactKind identifies one immutable documentation evidence packet.
	EvidencePacketFactKind = "documentation_evidence_packet"

	// FactSchemaVersion is the first documentation fact schema for
	// documentation fact kinds that have not introduced kind-specific schema
	// versions.
	FactSchemaVersion = "1.0.0"
	// SectionFactSchemaVersion is the documentation section schema
	// version that adds source-native content fields for updater diffing.
	SectionFactSchemaVersion = "1.1.0"
)

// FactKinds returns every documentation fact kind owned by Eshu
// core. It is the single source for the documentation family so the core
// fact-kind registry and the schema-version registry cannot drift.
func FactKinds() []string {
	return []string{
		SourceFactKind,
		DocumentFactKind,
		SectionFactKind,
		LinkFactKind,
		EntityMentionFactKind,
		ClaimCandidateFactKind,
		FindingFactKind,
		EvidencePacketFactKind,
	}
}

// SchemaVersion returns the schema version a core consumer supports
// for a documentation fact kind. The section kind carries its own version; the
// remaining documentation kinds share the base documentation schema version.
func SchemaVersion(factKind string) (string, bool) {
	switch factKind {
	case SectionFactKind:
		return SectionFactSchemaVersion, true
	case SourceFactKind,
		DocumentFactKind,
		LinkFactKind,
		EntityMentionFactKind,
		ClaimCandidateFactKind,
		FindingFactKind,
		EvidencePacketFactKind:
		return FactSchemaVersion, true
	default:
		return "", false
	}
}

const (
	// MentionResolutionExact means the mention resolved to one entity.
	MentionResolutionExact = "exact"
	// MentionResolutionAmbiguous means the mention has multiple candidate entities.
	MentionResolutionAmbiguous = "ambiguous"
	// MentionResolutionUnmatched means the mention did not resolve to an entity.
	MentionResolutionUnmatched = "unmatched"

	// ClaimAuthorityDocumentEvidence marks claims as evidence about document text only.
	ClaimAuthorityDocumentEvidence = "document_evidence"
)

// OwnerRef identifies an owner reference reported by a documentation source.
type OwnerRef struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	SourceURI   string `json:"source_uri,omitempty"`
}

// ACLSummary records bounded access metadata reported by a documentation source.
type ACLSummary struct {
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

// EvidenceRef references evidence used by a documentation payload.
type EvidenceRef struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	URI        string `json:"uri,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

// SourcePayload describes a documentation source such as Confluence or Git Markdown.
type SourcePayload struct {
	SourceID       string            `json:"source_id"`
	SourceSystem   string            `json:"source_system"`
	ExternalID     string            `json:"external_id"`
	DisplayName    string            `json:"display_name,omitempty"`
	BaseURI        string            `json:"base_uri,omitempty"`
	SourceType     string            `json:"source_type,omitempty"`
	Labels         []string          `json:"labels,omitempty"`
	OwnerRefs      []OwnerRef        `json:"owner_refs,omitempty"`
	ACLSummary     *ACLSummary       `json:"acl_summary,omitempty"`
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}

// DocumentPayload describes one source-neutral documentation document revision.
type DocumentPayload struct {
	SourceID          string            `json:"source_id"`
	DocumentID        string            `json:"document_id"`
	ExternalID        string            `json:"external_id"`
	RevisionID        string            `json:"revision_id"`
	CanonicalURI      string            `json:"canonical_uri,omitempty"`
	Title             string            `json:"title,omitempty"`
	ParentDocumentID  string            `json:"parent_document_id,omitempty"`
	DocumentType      string            `json:"document_type,omitempty"`
	Format            string            `json:"format,omitempty"`
	Language          string            `json:"language,omitempty"`
	Labels            []string          `json:"labels,omitempty"`
	OwnerRefs         []OwnerRef        `json:"owner_refs,omitempty"`
	ACLSummary        *ACLSummary       `json:"acl_summary,omitempty"`
	SourceMetadata    map[string]string `json:"source_metadata,omitempty"`
	ContentHash       string            `json:"content_hash,omitempty"`
	DocumentCreatedAt string            `json:"document_created_at,omitempty"`
	DocumentUpdatedAt string            `json:"document_updated_at,omitempty"`
}

// SectionPayload describes one bounded section in a document revision.
type SectionPayload struct {
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

// LinkPayload describes one link observed in a document section.
type LinkPayload struct {
	DocumentID     string            `json:"document_id"`
	RevisionID     string            `json:"revision_id"`
	SectionID      string            `json:"section_id,omitempty"`
	LinkID         string            `json:"link_id"`
	TargetURI      string            `json:"target_uri"`
	TargetKind     string            `json:"target_kind,omitempty"`
	AnchorTextHash string            `json:"anchor_text_hash,omitempty"`
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}

// EntityMentionPayload describes one possible entity mention in documentation.
type EntityMentionPayload struct {
	DocumentID       string        `json:"document_id"`
	RevisionID       string        `json:"revision_id,omitempty"`
	SectionID        string        `json:"section_id"`
	MentionID        string        `json:"mention_id"`
	MentionText      string        `json:"mention_text"`
	MentionKind      string        `json:"mention_kind"`
	ResolutionStatus string        `json:"resolution_status"`
	CandidateRefs    []EvidenceRef `json:"candidate_refs,omitempty"`
	ExcerptHash      string        `json:"excerpt_hash,omitempty"`
	// ACLSummary carries the bounded source access posture observed for the
	// document this mention was extracted from, propagated verbatim from the
	// owning source/document fact (see ACLSummary.SourceACLState).
	// It is additive evidence metadata: a mention inherits its document's
	// observed source_acl_state so the docs-evidence projection and readbacks
	// carry the posture end-to-end. It is omitted when the document asserted no
	// bounded ACL claim (absence means "no ACL claim"); a denied, partial,
	// missing, or stale observation is never upgraded to allowed. It is
	// factual propagation only and never decides disclosure or enforcement.
	ACLSummary     *ACLSummary       `json:"acl_summary,omitempty"`
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}

// ClaimCandidatePayload describes a non-authoritative claim found in documentation.
type ClaimCandidatePayload struct {
	DocumentID       string        `json:"document_id"`
	RevisionID       string        `json:"revision_id,omitempty"`
	SectionID        string        `json:"section_id"`
	ClaimID          string        `json:"claim_id"`
	ClaimType        string        `json:"claim_type"`
	ClaimText        string        `json:"claim_text"`
	ClaimHash        string        `json:"claim_hash"`
	ExcerptHash      string        `json:"excerpt_hash,omitempty"`
	SubjectMentionID string        `json:"subject_mention_id,omitempty"`
	ObjectMentionIDs []string      `json:"object_mention_ids,omitempty"`
	EvidenceRefs     []EvidenceRef `json:"evidence_refs,omitempty"`
	Authority        string        `json:"authority"`
	// ACLSummary carries the bounded source access posture observed for the
	// document this claim candidate was extracted from, propagated verbatim
	// from the owning source/document fact (see
	// ACLSummary.SourceACLState). It is additive evidence
	// metadata: a claim inherits its document's observed source_acl_state so
	// the docs-evidence projection and readbacks carry the posture end-to-end.
	// It is omitted when the document asserted no bounded ACL claim (absence
	// means "no ACL claim"); a denied, partial, missing, or stale observation
	// is never upgraded to allowed. It is factual propagation only and never
	// decides disclosure or enforcement.
	ACLSummary     *ACLSummary       `json:"acl_summary,omitempty"`
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}

// SourceStableID returns a stable ID for a documentation source.
func SourceStableID(payload SourcePayload) string {
	return encode.StableID(SourceFactKind, map[string]any{
		"source_id":     payload.SourceID,
		"source_system": payload.SourceSystem,
		"external_id":   payload.ExternalID,
	})
}

// DocumentStableID returns a stable ID for a documentation document revision.
func DocumentStableID(payload DocumentPayload) string {
	return encode.StableID(DocumentFactKind, map[string]any{
		"source_id":   payload.SourceID,
		"document_id": payload.DocumentID,
		"external_id": payload.ExternalID,
		"revision_id": payload.RevisionID,
	})
}

// SectionStableID returns a stable ID for a documentation section revision.
func SectionStableID(payload SectionPayload) string {
	return encode.StableID(SectionFactKind, map[string]any{
		"document_id":    payload.DocumentID,
		"revision_id":    payload.RevisionID,
		"section_id":     payload.SectionID,
		"section_anchor": payload.SectionAnchor,
		"ordinal_path":   payload.OrdinalPath,
		"text_hash":      payload.TextHash,
		"excerpt_hash":   payload.ExcerptHash,
	})
}

// LinkStableID returns a stable ID for one documentation link.
func LinkStableID(payload LinkPayload) string {
	return encode.StableID(LinkFactKind, map[string]any{
		"document_id": payload.DocumentID,
		"revision_id": payload.RevisionID,
		"section_id":  payload.SectionID,
		"link_id":     payload.LinkID,
		"target_uri":  payload.TargetURI,
	})
}

// EntityMentionStableID returns a stable ID for one entity mention.
func EntityMentionStableID(payload EntityMentionPayload) string {
	return encode.StableID(EntityMentionFactKind, map[string]any{
		"document_id":  payload.DocumentID,
		"revision_id":  payload.RevisionID,
		"section_id":   payload.SectionID,
		"mention_id":   payload.MentionID,
		"excerpt_hash": payload.ExcerptHash,
	})
}

// ClaimCandidateStableID returns a stable ID for one documentation claim candidate.
func ClaimCandidateStableID(payload ClaimCandidatePayload) string {
	return encode.StableID(ClaimCandidateFactKind, map[string]any{
		"document_id":  payload.DocumentID,
		"revision_id":  payload.RevisionID,
		"section_id":   payload.SectionID,
		"claim_id":     payload.ClaimID,
		"claim_hash":   payload.ClaimHash,
		"excerpt_hash": payload.ExcerptHash,
	})
}

// FindingStableID returns a stable ID for one documentation finding.
func FindingStableID(findingID, findingVersion string) string {
	return encode.StableID(FindingFactKind, map[string]any{
		"finding_id":      findingID,
		"finding_version": findingVersion,
	})
}

// EvidencePacketStableID returns a stable ID for one evidence packet.
func EvidencePacketStableID(packetID, packetVersion string) string {
	return encode.StableID(EvidencePacketFactKind, map[string]any{
		"packet_id":      packetID,
		"packet_version": packetVersion,
	})
}
