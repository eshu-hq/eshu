// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbomdocument parses fixture-backed CycloneDX and SPDX JSON
// documents into Eshu's reported-confidence SBOM source facts.
//
// The package owns one slice of the
// sync -> discover -> parse -> emit facts -> enqueue work -> reducer
// flow: turning a single SBOM document body into stable
// [github.com/eshu-hq/eshu/go/internal/facts] envelopes that the SBOM and
// attestation attachment reducer can later classify.
//
// # Source confidence
//
// Parser facts are always emitted with [facts.SourceConfidenceReported]. A
// successfully parsed SBOM does not by itself prove the document is bound to
// any artifact. Only the reducer, after observing attestation or signature
// evidence, may promote a document to verified truth. The parser leaves
// verification_status blank on every fact it emits.
//
// # Fact kinds
//
// CycloneDXFixtureEnvelopes and SPDXFixtureEnvelopes emit a subset of the
// SBOM fact kinds declared in [facts.SBOMAttestationFactKinds]:
//
//   - [facts.SBOMDocumentFactKind] — one per source document with subject
//     digest, format, spec version, parse status, and counts.
//   - [facts.SBOMComponentFactKind] — one per projected component or SPDX
//     package, including the metadata.component or SPDXRef-DOCUMENT subject.
//   - [facts.SBOMExternalReferenceFactKind] — one per CycloneDX external
//     reference or SPDX external ref locator.
//   - [facts.SBOMDependencyRelationshipFactKind] — one per resolved
//     dependency or SPDX relationship edge.
//   - [facts.SBOMWarningFactKind] — one per parser-level warning
//     (malformed_document, missing_subject, ambiguous_subject,
//     duplicate_component_identity, unsupported_field,
//     component_missing_identity, unattached_relationship).
//
// # Subject identity
//
// CycloneDX subject digests are derived from metadata.component.hashes.
// SPDX subject digests are derived from the checksums of packages
// referenced via DESCRIBES relationships from SPDXRef-DOCUMENT. When zero
// or more than one distinct subject is found the document fact carries an
// empty subject_digest and a missing_subject or ambiguous_subject warning
// is emitted so the reducer can classify the document as unknown_subject
// or ambiguous_subject.
//
// # Duplicates and unsupported fields
//
// Components or packages that share a canonical identity within one document
// are still emitted but flagged with is_duplicate=true and a
// duplicate_component_identity warning. CycloneDX vulnerabilities, services,
// compositions, formulation, and annotations sections — and the equivalent
// SPDX hasExtractedLicensingInfos, files, snippets, and annotations sections
// — are intentionally not projected into stable facts; the parser instead
// emits unsupported_field warnings so the reducer can surface coverage gaps.
package sbomdocument
