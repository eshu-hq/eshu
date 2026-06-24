// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomdocument

import "time"

// CollectorKind is the durable collector family name for SBOM document source
// facts.
const CollectorKind = "sbom_document"

// Format names the SBOM serialization format that produced one document.
type Format string

const (
	// FormatCycloneDX identifies CycloneDX JSON documents.
	FormatCycloneDX Format = "cyclonedx"
	// FormatSPDX identifies SPDX JSON documents.
	FormatSPDX Format = "spdx"
)

// SourceFormat names the on-disk encoding of the document.
type SourceFormat string

const (
	// SourceFormatJSON identifies JSON-encoded SBOM bodies.
	SourceFormatJSON SourceFormat = "json"
)

// ParseStatus names the parser outcome for one source document.
type ParseStatus string

const (
	// ParseStatusParsed means the source document decoded into a stable shape.
	ParseStatusParsed ParseStatus = "parsed"
	// ParseStatusMalformed means the source document could not be decoded.
	ParseStatusMalformed ParseStatus = "malformed"
)

// VerificationStatus names whether attestation evidence has confirmed the
// document. Parser facts must leave this unset because parsing alone is not
// proof of verification.
type VerificationStatus string

const (
	// VerificationStatusUnset means no signature or attestation evidence has
	// been observed for the document.
	VerificationStatusUnset VerificationStatus = ""
)

// WarningReason names a parser-level warning recorded against one document.
type WarningReason string

const (
	// WarningReasonMalformedDocument records that the document body could not
	// be decoded as JSON or did not match the expected schema family.
	WarningReasonMalformedDocument WarningReason = "malformed_document"
	// WarningReasonMissingSubject records that the document parsed but
	// reported no artifact subject digest.
	WarningReasonMissingSubject WarningReason = "missing_subject"
	// WarningReasonAmbiguousSubject records that the document reported more
	// than one distinct subject identity.
	WarningReasonAmbiguousSubject WarningReason = "ambiguous_subject"
	// WarningReasonDuplicateComponent records that two or more components
	// share the same identity within one document.
	WarningReasonDuplicateComponent WarningReason = "duplicate_component_identity"
	// WarningReasonUnsupportedField records that the document included a
	// field or shape that the parser does not project into facts.
	WarningReasonUnsupportedField WarningReason = "unsupported_field"
	// WarningReasonComponentMissingIdentity records that a component carried
	// neither a PURL nor a name+version identifier the parser could project.
	WarningReasonComponentMissingIdentity WarningReason = "component_missing_identity"
	// WarningReasonUnattachedRelationship records that a dependency edge
	// referenced a bom-ref or SPDX ID that did not resolve to a known
	// component.
	WarningReasonUnattachedRelationship WarningReason = "unattached_relationship"
)

// FixtureContext carries the collector boundary fields copied into fixture
// normalized facts.
type FixtureContext struct {
	// ScopeID anchors the durable scope boundary for emitted facts.
	ScopeID string
	// GenerationID anchors the durable generation boundary for emitted facts.
	GenerationID string
	// CollectorInstanceID identifies the offline collector run that produced
	// the document.
	CollectorInstanceID string
	// FencingToken is the durable lease token that bounds writes.
	FencingToken int64
	// ObservedAt is the timestamp the collector observed the document.
	ObservedAt time.Time
	// SourceURI points at the document body in the original system, if any.
	SourceURI string
	// SourceRecordID identifies the document in the originating system.
	SourceRecordID string
}
