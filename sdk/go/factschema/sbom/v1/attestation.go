// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Statement is the schema-version-1 typed payload for the
// "attestation.statement" fact kind: one in-toto attestation statement
// observed on an artifact.
//
// StatementID is the only required field: the attestation runtime collector
// (sbomruntime.attestationStatementEnvelope) always sets it — even on a
// parse-failure statement (attestationEnvelopes builds a malformed-status
// Statement before the parse error is known) — and it is the reducer's join
// key back to a statement's verification/warning evidence (index.documents,
// index.verifications, index.warnings all key by statement_id). A statement
// whose identifier is absent could never be attached to its verification
// evidence, matching the same accuracy contract Document.DocumentID protects
// for the SBOM side of this family.
type Statement struct {
	// StatementID is the collector-derived stable statement identifier.
	// Required — the reducer's sole index key for a statement.
	StatementID string `json:"statement_id"`

	// StatementDigest is the SHA-256 digest of the raw statement bytes.
	// Optional: always emitted.
	StatementDigest *string `json:"statement_digest,omitempty"`

	// PayloadDigest is the statement's payload digest, distinct from
	// StatementDigest when the collector separates envelope and payload
	// hashing. Optional.
	PayloadDigest *string `json:"payload_digest,omitempty"`

	// SubjectDigest is the single resolved subject digest when the statement
	// names exactly one subject. Optional: empty when the statement carries
	// zero or more than one subject (see SubjectDigests).
	SubjectDigest *string `json:"subject_digest,omitempty"`

	// SubjectDigests lists every subject digest the statement names.
	// Optional: more than one entry means the reducer cannot choose one
	// canonical image attachment (SBOMAttachmentAmbiguousSubject).
	SubjectDigests []string `json:"subject_digests,omitempty"`

	// ParseStatus is the collector's parse outcome ("parsed", "malformed").
	// Optional: an absent value defaults to "parsed" on the reducer's read
	// side.
	ParseStatus *string `json:"parse_status,omitempty"`

	// VerificationStatus is a statement-level verification outcome the
	// collector observed inline, distinct from a separate
	// attestation.signature_verification fact. Optional: empty when
	// verification is reported only through the separate kind.
	VerificationStatus *string `json:"verification_status,omitempty"`

	// VerificationPolicy names the policy VerificationStatus was evaluated
	// against. Optional.
	VerificationPolicy *string `json:"verification_policy,omitempty"`

	// AttestationFormat is the attestation envelope format (always "in-toto"
	// on the current collector path). Optional.
	AttestationFormat *string `json:"attestation_format,omitempty"`

	// AttestationVersion is the in-toto statement's own "_type" field value.
	// Optional: empty on a malformed statement the collector could not
	// parse.
	AttestationVersion *string `json:"attestation_version,omitempty"`

	// PredicateType is the in-toto statement's "predicateType" (for example
	// "https://slsa.dev/provenance/v1"). Optional: empty on a malformed
	// statement.
	PredicateType *string `json:"predicate_type,omitempty"`

	// SourceFormat is the source attestation encoding ("json" today).
	// Optional.
	SourceFormat *string `json:"source_format,omitempty"`
}

// SignatureVerification is the schema-version-1 typed payload for the
// "attestation.signature_verification" fact kind: one signature or provenance
// verification result for an attestation statement or SBOM document.
//
// StatementID is the only required field: the attestation runtime collector
// (sbomruntime.attestationVerificationEnvelope) always sets it, and it is the
// reducer's join key back to the statement it verifies (index.verifications
// keyed by firstNonBlank(statement_id, document_id)). DocumentID is optional
// because a verification fact can also be reported for an SBOM document
// directly (the reducer's key resolution accepts either), not only for an
// attestation statement.
type SignatureVerification struct {
	// StatementID is the verified attestation statement's identifier.
	// Required — the reducer's primary join key for a verification result.
	StatementID string `json:"statement_id"`

	// DocumentID is the verified SBOM document's identifier, when the
	// verification targets a document rather than a statement. Optional:
	// the reducer's index falls back to this key when StatementID resolves
	// to no known statement.
	DocumentID *string `json:"document_id,omitempty"`

	// StatementDigest is the SHA-256 digest of the verified statement.
	// Optional: always emitted by the current runtime verifier.
	StatementDigest *string `json:"statement_digest,omitempty"`

	// VerificationResult is the raw provider-reported verification outcome
	// (before the reducer's normalizedVerificationStatus mapping). Optional.
	VerificationResult *string `json:"verification_result,omitempty"`

	// VerificationStatus mirrors VerificationResult on the current collector
	// path (attestationVerificationEnvelope sets both to the same value).
	// Optional.
	VerificationStatus *string `json:"verification_status,omitempty"`

	// VerificationPolicy names the policy the verification was evaluated
	// against. Optional.
	VerificationPolicy *string `json:"verification_policy,omitempty"`

	// VerificationSubject is the subject digest the verification targeted,
	// when the collector reports it separately from the statement's own
	// subject fields. Optional.
	VerificationSubject *string `json:"verification_subject,omitempty"`
}

// SLSAProvenance is the schema-version-1 typed payload for the
// "attestation.slsa_provenance" fact kind: one SLSA provenance predicate
// extracted from an in-toto attestation statement.
//
// This kind is TYPED-BUT-NOT-YET-EMITTED: no collector in this repository
// constructs an attestation.slsa_provenance envelope today (SLSA provenance
// is currently observed only as the PredicateType field on a generic
// Statement, never decoded into its own predicate-specific fact). No
// reducer or storage read path decodes it either. StatementID is required,
// matching the join-key discipline every other kind in this family uses, so
// that when a SLSA-provenance-specific emitter and consumer are added, the
// identity contract is already in place; every other field is left
// deliberately minimal pending that emitter's real payload shape.
type SLSAProvenance struct {
	// StatementID is the owning attestation statement's identifier.
	// Required — matches the join-key convention Statement and
	// SignatureVerification already use in this family.
	StatementID string `json:"statement_id"`

	// PredicateType is the SLSA predicate type URI. Optional.
	PredicateType *string `json:"predicate_type,omitempty"`

	// BuilderID is the SLSA provenance builder identity, when reported.
	// Optional.
	BuilderID *string `json:"builder_id,omitempty"`
}
