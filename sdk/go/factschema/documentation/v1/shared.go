// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// OwnerRef identifies an owner reference reported by a documentation source.
// It mirrors go/internal/facts.DocumentationOwnerRef verbatim; every field is
// optional because a source/document envelope may report zero, one, or many
// owner refs with a partial shape.
type OwnerRef struct {
	// Kind classifies the owner reference (for example "user" or "group").
	// Optional.
	Kind *string `json:"kind,omitempty"`

	// ID is the owner's external identifier. Optional.
	ID *string `json:"id,omitempty"`

	// DisplayName is the owner's human-readable name. Optional.
	DisplayName *string `json:"display_name,omitempty"`

	// SourceURI is a source-native URI for the owner. Optional.
	SourceURI *string `json:"source_uri,omitempty"`
}

// ACLSummary records bounded access metadata reported by a documentation
// source. It mirrors go/internal/facts.DocumentationACLSummary verbatim;
// every field is optional because a collector may report a partial or empty
// ACL observation, and an absent SourceACLState means "no ACL claim" (see
// facts.ValidSourceACLState) rather than an implicit default.
type ACLSummary struct {
	// Visibility is the source-reported visibility class. Optional.
	Visibility *string `json:"visibility,omitempty"`

	// ReaderGroups lists reader group identifiers. Optional.
	ReaderGroups []string `json:"reader_groups,omitempty"`

	// WriterGroups lists writer group identifiers. Optional.
	WriterGroups []string `json:"writer_groups,omitempty"`

	// ReaderUsers lists reader user identifiers. Optional.
	ReaderUsers []string `json:"reader_users,omitempty"`

	// WriterUsers lists writer user identifiers. Optional.
	WriterUsers []string `json:"writer_users,omitempty"`

	// HasInherited reports whether the ACL includes inherited entries.
	// Optional.
	HasInherited *bool `json:"has_inherited,omitempty"`

	// IsPartial reports whether the collector could only observe a partial
	// ACL. Optional.
	IsPartial *bool `json:"is_partial,omitempty"`

	// SourceACLState is the bounded, additive source-ACL-state observation
	// for this documentation content/evidence fact
	// (allowed|denied|partial|missing|stale — facts.ValidSourceACLState). It
	// is set only when the collector observed a real access-posture signal
	// at the origin; absence means "no ACL claim." A denied, partial,
	// missing, or stale observation is never upgraded to allowed. Optional.
	SourceACLState *string `json:"source_acl_state,omitempty"`

	// PartialReason explains why IsPartial is true, when known. Optional.
	PartialReason *string `json:"partial_reason,omitempty"`
}

// EvidenceRef references evidence used by a documentation payload. It
// mirrors go/internal/facts.DocumentationEvidenceRef verbatim; every field is
// optional because the reducer's own read (ExtractDocumentationEdgeRows)
// only requires that at least one candidate_refs entry decode with an ID and
// Kind — a payload may still report a partial ref for other consumers.
type EvidenceRef struct {
	// Kind classifies the referenced entity (for example "service",
	// "function", "workload"). Optional.
	Kind *string `json:"kind,omitempty"`

	// ID is the referenced entity's identifier. Optional.
	ID *string `json:"id,omitempty"`

	// URI is a source-native URI for the reference. Optional.
	URI *string `json:"uri,omitempty"`

	// Confidence is the collector's confidence label for the reference.
	// Optional.
	Confidence *string `json:"confidence,omitempty"`
}
