// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Document is the schema-version-1 typed payload for the "sbom.document" fact
// kind: one parsed or attempted SBOM document (CycloneDX or SPDX), one per
// scanned artifact.
//
// DocumentID is the only required field: every collector document envelope
// (sbomdocument.cycloneDXDocumentEnvelope, sbomdocument.spdxDocumentEnvelope,
// sbomruntime's attestation-adjacent runtime path) always sets it, and it is
// the reducer's own join/index key (buildSBOMAttachmentIndex keys
// index.documents by DocumentID). A collector regression that drops the key
// now dead-letters as input_invalid instead of the document silently
// vanishing from the attachment index. Every other field is optional: the
// collector always WRITES them (so an observed empty string is a valid,
// common value — for example DocumentDigest or SubjectDigest before subject
// resolution), but none of them anchors reducer identity the way DocumentID
// does.
type Document struct {
	// DocumentID is the collector-derived stable document identifier.
	// Required — the reducer's sole index key for a document.
	DocumentID string `json:"document_id"`

	// DocumentDigest is the SHA-256 digest of the raw document bytes.
	// Optional: always emitted, but reported facts do not carry parse
	// evidence to compute one until reached parse.
	DocumentDigest *string `json:"document_digest,omitempty"`

	// SubjectDigest is the single resolved subject digest when the document
	// names exactly one subject. Optional: empty when the document carries
	// zero or more than one subject (see SubjectDigests).
	SubjectDigest *string `json:"subject_digest,omitempty"`

	// SubjectDigests lists every subject digest the document names. Optional:
	// more than one entry means the reducer cannot choose one canonical image
	// attachment (SBOMAttachmentAmbiguousSubject).
	SubjectDigests []string `json:"subject_digests,omitempty"`

	// ParseStatus is the collector's parse outcome ("parsed", "unparseable",
	// "malformed", ...). Optional: an absent value defaults to "parsed" on the
	// reducer's read side (defaultStatus).
	ParseStatus *string `json:"parse_status,omitempty"`

	// VerificationStatus is the document-level verification outcome the
	// collector observed inline (distinct from a separate
	// attestation.signature_verification fact). Optional: empty when
	// verification was not configured.
	VerificationStatus *string `json:"verification_status,omitempty"`

	// VerificationPolicy names the policy the collector evaluated
	// VerificationStatus against. Optional.
	VerificationPolicy *string `json:"verification_policy,omitempty"`

	// Format is the SBOM document format ("cyclonedx", "spdx"). Optional:
	// always emitted by the collector.
	Format *string `json:"format,omitempty"`

	// SpecVersion is the format's spec version string (for example "1.6").
	// Optional: always emitted by the collector.
	SpecVersion *string `json:"spec_version,omitempty"`
}

// Component is the schema-version-1 typed payload for the "sbom.component"
// fact kind: one component (package/library) an SBOM document declares.
//
// DocumentID is the only required field: every collector component envelope
// (sbomdocument.cycloneDXComponentEnvelope, sbomdocument.spdxComponentEnvelope)
// always sets it, and it is the reducer's join key back to the owning
// Document (index.components keyed by DocumentID) AND the supply-chain
// impact index's join key
// (supplyChainSBOMComponentFromEnvelope/supply_chain_impact_index.go). A
// component whose document_id is absent could never join to its document or
// contribute impact evidence, so this is the one field worth a decode-time
// guarantee rather than a silent empty-string join failure. PURL, CPE,
// PackageID, and Version are all optional even though the reducer's supply
// chain matcher requires at least one of PURL/PackageID/CPE to be non-empty
// to index a component — that is the matcher's OWN materialization gate
// (mirroring the identity-completeness gates every other typed family
// keeps), not a decode-time requirement, so a present-but-empty value is a
// valid decode that the matcher still declines to index.
type Component struct {
	// DocumentID is the owning SBOM document's identifier. Required — the
	// reducer's join key from a component back to its document and into the
	// supply-chain impact index.
	DocumentID string `json:"document_id"`

	// ComponentID is the collector-derived stable component identifier.
	// Optional: always emitted, but not a reducer join key.
	ComponentID *string `json:"component_id,omitempty"`

	// PURL is the component's Package URL. Optional: empty when the
	// component carries no derivable purl.
	PURL *string `json:"purl,omitempty"`

	// CPE is the component's CPE identifier. Optional.
	CPE *string `json:"cpe,omitempty"`

	// PackageID is the canonical cross-source package identity the collector
	// derives from PURL, shared with vulnerability and package-registry
	// facts. Optional: absent for components ingested before the collector
	// carried package_id (the reducer falls back to deriving one from PURL).
	PackageID *string `json:"package_id,omitempty"`

	// Version is the component's version string. Optional: the reducer falls
	// back to a version parsed from PURL when this is empty.
	Version *string `json:"version,omitempty"`

	// Name is the component's declared name. Optional.
	Name *string `json:"name,omitempty"`

	// LockfilePath is the manifest/lockfile path evidence, when the collector
	// attaches it. Optional.
	LockfilePath *string `json:"lockfile_path,omitempty"`

	// Ecosystem is the component's package ecosystem, when a producer reports
	// it directly rather than through PURL's scheme segment. Optional: no
	// current in-tree collector emits this key, but the reducer's
	// componentEvidenceRows has always read it defensively (supply_chain
	// impact matching also treats a component as indexable whenever PURL,
	// PackageID, or CPE is present, independent of this field), so it is
	// modeled to preserve byte-identical behavior for any payload producer
	// that does set it.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// DependencyScope classifies the component's declared scope (runtime,
	// dev, ...). Optional.
	DependencyScope *string `json:"dependency_scope,omitempty"`

	// DependencyType classifies how the dependency was declared. Optional.
	DependencyType *string `json:"dependency_type,omitempty"`

	// ExtractionReason records why the collector extracted this component
	// when it required a fallback heuristic. Optional.
	ExtractionReason *string `json:"extraction_reason,omitempty"`
}

// DependencyRelationship is the schema-version-1 typed payload for the
// "sbom.dependency_relationship" fact kind: one resolved dependency edge
// between two components in the same SBOM document.
//
// This kind is TYPED-BUT-NOT-YET-CONSUMED: no reducer or storage read path
// decodes it today (go/internal/reducer/sbom_attestation_attachment.go loads
// the kind alongside its siblings but never inspects its payload). DocumentID
// is required to keep the same join-key discipline as every other kind in
// this family should a future consumer join dependency edges back to their
// document; every other field mirrors the collector emitter
// (sbomdocument.dependencyFact) verbatim.
type DependencyRelationship struct {
	// DocumentID is the owning SBOM document's identifier. Required.
	DocumentID string `json:"document_id"`

	// FromComponentID is the dependent component's identifier. Optional.
	FromComponentID *string `json:"from_component_id,omitempty"`

	// ToComponentID is the depended-upon component's identifier. Optional.
	ToComponentID *string `json:"to_component_id,omitempty"`

	// RelationshipType is the dependency relationship type the SBOM format
	// names (for example "depends_on"). Optional.
	RelationshipType *string `json:"relationship_type,omitempty"`

	// RelationshipOrigin classifies how the collector resolved the edge.
	// Optional.
	RelationshipOrigin *string `json:"relationship_origin,omitempty"`
}

// ExternalReference is the schema-version-1 typed payload for the
// "sbom.external_reference" fact kind: one external reference (advisory,
// website, VCS, ...) an SBOM component declares.
//
// This kind is TYPED-BUT-NOT-YET-CONSUMED, mirroring DependencyRelationship:
// no reducer or storage read path decodes it today. DocumentID is required
// for the same join-key discipline; every other field mirrors the collector
// emitter (sbomdocument.externalReferenceFact) verbatim.
type ExternalReference struct {
	// DocumentID is the owning SBOM document's identifier. Required.
	DocumentID string `json:"document_id"`

	// ComponentID is the referencing component's identifier. Optional.
	ComponentID *string `json:"component_id,omitempty"`

	// ReferenceType classifies the external reference (for example
	// "website", "vcs", "advisory"). Optional.
	ReferenceType *string `json:"reference_type,omitempty"`

	// ReferenceURL is the reference's URL, when the SBOM format expresses it
	// as a URL. Optional.
	ReferenceURL *string `json:"reference_url,omitempty"`

	// ReferenceLocator is the reference's locator, when the SBOM format
	// expresses it as a locator string distinct from a URL. Optional.
	ReferenceLocator *string `json:"reference_locator,omitempty"`
}

// Warning is the schema-version-1 typed payload for the "sbom.warning" fact
// kind: one non-fatal warning the collector raised while parsing or
// resolving an SBOM or attestation document.
//
// Every field is OPTIONAL, including DocumentID and StatementID, because two
// distinct collector paths emit this one fact kind with two distinct,
// mutually-exclusive identity keys: the SBOM document collector
// (sbomdocument.warningFact) always sets DocumentID and never StatementID,
// while the attestation runtime collector (sbomruntime.attestationWarningEnvelope)
// always sets StatementID and never DocumentID. Neither key is present on
// every sbom.warning fact, so neither can be required without dead-lettering
// half of this kind's real traffic. The reducer's own read side already
// tolerates either being absent (index.warnings keyed by
// firstNonBlank(document_id, statement_id)); this struct preserves that
// graceful degradation rather than introducing a decode-time requirement the
// collector contract does not make.
type Warning struct {
	// DocumentID is the owning SBOM document's identifier, set by the SBOM
	// document collector path. Optional: absent on an attestation-path
	// warning, which sets StatementID instead.
	DocumentID *string `json:"document_id,omitempty"`

	// StatementID is the owning attestation statement's identifier, set by
	// the attestation runtime collector path. Optional: absent on an
	// SBOM-document-path warning, which sets DocumentID instead.
	StatementID *string `json:"statement_id,omitempty"`

	// Reason classifies the warning (for example "malformed_document").
	// Optional.
	Reason *string `json:"reason,omitempty"`

	// Summary is the human-readable warning summary. Optional.
	Summary *string `json:"summary,omitempty"`

	// OccurrenceCount is the number of times the collector observed this
	// warning condition, when it batches repeated warnings. Optional: an
	// absent value defaults to 1 occurrence on the reducer's read side
	// (warningOccurrenceCount).
	OccurrenceCount *int `json:"occurrence_count,omitempty"`
}
