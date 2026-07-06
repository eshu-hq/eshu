// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Source is the schema-version-1 typed payload for the "documentation_source"
// fact kind: one documentation source such as Confluence or a git Markdown
// tree.
//
// This kind is TYPED-BUT-NOT-YET-CONSUMED by any reducer or storage-loader
// read path: go/internal/query's documentation read models filter on it only
// by fact_kind column (never a structured payload field), and no reducer
// decode site reads it. SourceID/SourceSystem/ExternalID form
// facts.DocumentationSourceStableID, so they are modeled required to match
// that identity discipline ahead of a future consumer, mirroring how the
// sbom_attestation family typed DependencyRelationship/ExternalReference
// before either had a reader.
type Source struct {
	// SourceID is the collector-derived stable source identifier. Required —
	// part of facts.DocumentationSourceStableID.
	SourceID string `json:"source_id"`

	// SourceSystem is the documentation source's system class (for example
	// "confluence" or "git_markdown"). Required — part of
	// facts.DocumentationSourceStableID.
	SourceSystem string `json:"source_system"`

	// ExternalID is the source's external identifier. Required — part of
	// facts.DocumentationSourceStableID.
	ExternalID string `json:"external_id"`

	// DisplayName is the source's human-readable name. Optional.
	DisplayName *string `json:"display_name,omitempty"`

	// BaseURI is the source's base URI. Optional.
	BaseURI *string `json:"base_uri,omitempty"`

	// SourceType further classifies the source. Optional.
	SourceType *string `json:"source_type,omitempty"`

	// Labels lists source labels. Optional.
	Labels []string `json:"labels,omitempty"`

	// OwnerRefs lists owner references the source reports. Optional.
	OwnerRefs []OwnerRef `json:"owner_refs,omitempty"`

	// ACLSummary carries the bounded access metadata the source reports.
	// Optional.
	ACLSummary *ACLSummary `json:"acl_summary,omitempty"`

	// SourceMetadata carries source-native metadata as a flat string map.
	// Optional.
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}

// Document is the schema-version-1 typed payload for the
// "documentation_document" fact kind: one source-neutral documentation
// document revision.
//
// DocumentID is the sole field the reducer's own delta-scope builder
// (buildDocumentationDeltaScope, go/internal/reducer/documentation_edge_delta_scope.go)
// treats as mandatory identity: a document fact missing document_id cannot be
// matched to a changed/deleted relative path and is dropped from delta
// tracking today (silently, via the pre-typing semanticPayloadString returning
// "" and the empty-value early-continue). Every other field mirrors the
// collector emitter (go/internal/facts.DocumentationDocumentPayload) verbatim;
// SourceMetadata (specifically its "path" and "repo_id" entries) is read by
// the same delta-scope builder but only as a best-effort git-delta path match,
// never as identity, so it stays optional.
type Document struct {
	// DocumentID is the collector-derived stable document identifier.
	// Required — the reducer's delta-scope builder's sole join key for a
	// document (buildDocumentationDeltaScope keys changed/deleted document
	// ids by this field).
	DocumentID string `json:"document_id"`

	// SourceID is the owning documentation source's identifier. Optional.
	SourceID *string `json:"source_id,omitempty"`

	// ExternalID is the document's external identifier. Optional.
	ExternalID *string `json:"external_id,omitempty"`

	// RevisionID is the document revision identifier. Optional.
	RevisionID *string `json:"revision_id,omitempty"`

	// CanonicalURI is the document's canonical URI. Optional.
	CanonicalURI *string `json:"canonical_uri,omitempty"`

	// Title is the document's title. Optional.
	Title *string `json:"title,omitempty"`

	// ParentDocumentID is the parent document's identifier, for a nested
	// document. Optional.
	ParentDocumentID *string `json:"parent_document_id,omitempty"`

	// DocumentType classifies the document (for example "page", "readme").
	// Optional.
	DocumentType *string `json:"document_type,omitempty"`

	// Format is the document's content format. Optional.
	Format *string `json:"format,omitempty"`

	// Language is the document's language. Optional.
	Language *string `json:"language,omitempty"`

	// Labels lists document labels. Optional.
	Labels []string `json:"labels,omitempty"`

	// OwnerRefs lists owner references the document reports. Optional.
	OwnerRefs []OwnerRef `json:"owner_refs,omitempty"`

	// ACLSummary carries the bounded access metadata the document reports.
	// Optional.
	ACLSummary *ACLSummary `json:"acl_summary,omitempty"`

	// SourceMetadata carries source-native metadata as a flat string map,
	// including the "path" and "repo_id" keys the reducer's delta-scope
	// builder reads for a best-effort git-delta path match
	// (sourceMetadataString in documentation_edge_delta_scope.go). Optional.
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`

	// ContentHash is a content fingerprint for the document revision.
	// Optional.
	ContentHash *string `json:"content_hash,omitempty"`

	// DocumentCreatedAt is the document's creation timestamp, source-native
	// string form. Optional.
	DocumentCreatedAt *string `json:"document_created_at,omitempty"`

	// DocumentUpdatedAt is the document's last-updated timestamp,
	// source-native string form. Optional.
	DocumentUpdatedAt *string `json:"document_updated_at,omitempty"`
}

// Section is the schema-version-1 typed payload for the
// "documentation_section" fact kind: one bounded section in a document
// revision.
//
// This kind is TYPED-BUT-NOT-YET-CONSUMED by any reducer decode site: the
// reducer's edge materialization only reads documentation_entity_mention, and
// the query read model filters on documentation_section only by fact_kind
// column plus raw payload->>'heading_text'/'content'/'source_metadata'
// string-search reads (go/internal/query/documentation_read_model.go), never
// through a structured decode. This kind additionally carries its OWN schema
// version — DocumentationSectionFactSchemaVersion ("1.1.0") in
// go/internal/facts/documentation.go, distinct from every other kind in this
// family (all "1.0.0") — because it added source-native content fields for
// updater diffing after the base family was first defined. DocumentID and
// SectionID together with the identity-shaping fields form
// facts.DocumentationSectionStableID, so they are modeled required ahead of a
// future consumer, matching the DependencyRelationship/ExternalReference
// typed-but-deferred precedent.
type Section struct {
	// DocumentID is the owning document's identifier. Required — part of
	// facts.DocumentationSectionStableID.
	DocumentID string `json:"document_id"`

	// RevisionID is the owning document revision's identifier. Required —
	// part of facts.DocumentationSectionStableID.
	RevisionID string `json:"revision_id"`

	// SectionID is the section's identifier within the document. Required —
	// part of facts.DocumentationSectionStableID.
	SectionID string `json:"section_id"`

	// ParentSectionID is the parent section's identifier, for a nested
	// section. Optional.
	ParentSectionID *string `json:"parent_section_id,omitempty"`

	// SectionAnchor is the section's source-native anchor. Optional: part of
	// facts.DocumentationSectionStableID when present, but the collector may
	// omit it for a section with no anchor.
	SectionAnchor *string `json:"section_anchor,omitempty"`

	// HeadingText is the section's heading text, read by the query
	// read-model's text search (documentation_read_model.go). Optional.
	HeadingText *string `json:"heading_text,omitempty"`

	// OrdinalPath is the section's position within the document's section
	// tree. Optional: part of facts.DocumentationSectionStableID when
	// present.
	OrdinalPath []int `json:"ordinal_path,omitempty"`

	// Content is the section's body content, read by the query read-model's
	// text search. Optional.
	Content *string `json:"content,omitempty"`

	// ContentFormat is the section content's format (for example "mermaid",
	// "markdown"). Optional.
	ContentFormat *string `json:"content_format,omitempty"`

	// TextHash is a content fingerprint for the section text. Optional: part
	// of facts.DocumentationSectionStableID when present.
	TextHash *string `json:"text_hash,omitempty"`

	// ExcerptHash is a fingerprint for a bounded excerpt of the section.
	// Optional: part of facts.DocumentationSectionStableID when present.
	ExcerptHash *string `json:"excerpt_hash,omitempty"`

	// SourceStartRef is a source-native reference to the section's start.
	// Optional.
	SourceStartRef *string `json:"source_start_ref,omitempty"`

	// SourceEndRef is a source-native reference to the section's end.
	// Optional.
	SourceEndRef *string `json:"source_end_ref,omitempty"`

	// SourceMetadata carries source-native metadata as a flat string map.
	// Optional.
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`

	// ContainsWarnings reports whether the section carries collector
	// warnings. Optional.
	ContainsWarnings *bool `json:"contains_warnings,omitempty"`
}

// Link is the schema-version-1 typed payload for the "documentation_link"
// fact kind: one link observed in a document section.
//
// This kind is TYPED-BUT-NOT-YET-CONSUMED by any reducer decode site: the
// query read model filters on it only by fact_kind column plus a raw
// payload->>'target_uri' text-search read, never a structured decode.
// DocumentID, RevisionID, SectionID, LinkID, and TargetURI form
// facts.DocumentationLinkStableID, so DocumentID/LinkID/TargetURI are modeled
// required ahead of a future consumer (mirroring the family's typed-but-
// deferred precedent); RevisionID and SectionID are optional because a link
// may be observed at the document level, without a specific section.
type Link struct {
	// DocumentID is the owning document's identifier. Required — part of
	// facts.DocumentationLinkStableID.
	DocumentID string `json:"document_id"`

	// RevisionID is the owning document revision's identifier. Optional:
	// part of facts.DocumentationLinkStableID when present.
	RevisionID *string `json:"revision_id,omitempty"`

	// SectionID is the owning section's identifier, when the link was
	// observed within a specific section. Optional.
	SectionID *string `json:"section_id,omitempty"`

	// LinkID is the link's identifier within the document. Required — part
	// of facts.DocumentationLinkStableID.
	LinkID string `json:"link_id"`

	// TargetURI is the link's target URI, read by the query read-model's
	// text search. Required — part of facts.DocumentationLinkStableID.
	TargetURI string `json:"target_uri"`

	// TargetKind classifies the link target (for example "service",
	// "external"). Optional.
	TargetKind *string `json:"target_kind,omitempty"`

	// AnchorTextHash is a fingerprint for the link's anchor text. Optional.
	AnchorTextHash *string `json:"anchor_text_hash,omitempty"`

	// SourceMetadata carries source-native metadata as a flat string map.
	// Optional.
	SourceMetadata map[string]string `json:"source_metadata,omitempty"`
}
