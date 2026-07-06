// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"
)

// DecodeDocumentationSource decodes env.Payload into the latest
// documentationv1.Source struct for the "documentation_source" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2.
// Callers (reducer handlers) receive either the decoded struct or a
// classified *DecodeError; they must never substitute a zero-value struct on
// error. This kind is typed but not yet consumed by any reducer or storage
// read path (documentation/v1/README.md).
func DecodeDocumentationSource(env Envelope) (documentationv1.Source, error) {
	return decodeLatestMajor[documentationv1.Source](FactKindDocumentationSource, env)
}

// EncodeDocumentationSource marshals a documentationv1.Source into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeDocumentationSource for schema-version-1 payloads.
func EncodeDocumentationSource(source documentationv1.Source) (map[string]any, error) {
	return encodeToPayload(source)
}

// DecodeDocumentationDocument decodes env.Payload into the latest
// documentationv1.Document struct for the "documentation_document" fact
// kind. See DecodeDocumentationSource for the dispatch and error contract.
// This is one of the two kinds a reducer decode site consumes today
// (buildDocumentationDeltaScope).
func DecodeDocumentationDocument(env Envelope) (documentationv1.Document, error) {
	return decodeLatestMajor[documentationv1.Document](FactKindDocumentationDocument, env)
}

// EncodeDocumentationDocument marshals a documentationv1.Document into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeDocumentationDocument for schema-version-1 payloads.
func EncodeDocumentationDocument(document documentationv1.Document) (map[string]any, error) {
	return encodeToPayload(document)
}

// DecodeDocumentationSection decodes env.Payload into the latest
// documentationv1.Section struct for the "documentation_section" fact kind.
// See DecodeDocumentationSource for the dispatch and error contract.
// documentation_section carries its OWN schema version
// (DocumentationSectionSchemaVersion, "1.1.0"), but decodeLatestMajor
// dispatches on the schema-version MAJOR only ("1"), so this shares the same
// dispatch path as the rest of the family's 1.0.0 kinds; the distinct minor
// version only changes which generated JSON schema backs this struct. This
// kind is typed but not yet consumed by any reducer or storage read path.
func DecodeDocumentationSection(env Envelope) (documentationv1.Section, error) {
	return decodeLatestMajor[documentationv1.Section](FactKindDocumentationSection, env)
}

// EncodeDocumentationSection marshals a documentationv1.Section into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeDocumentationSection for schema-version-1 payloads.
func EncodeDocumentationSection(section documentationv1.Section) (map[string]any, error) {
	return encodeToPayload(section)
}

// DecodeDocumentationLink decodes env.Payload into the latest
// documentationv1.Link struct for the "documentation_link" fact kind. See
// DecodeDocumentationSource for the dispatch and error contract. This kind is
// typed but not yet consumed by any reducer or storage read path.
func DecodeDocumentationLink(env Envelope) (documentationv1.Link, error) {
	return decodeLatestMajor[documentationv1.Link](FactKindDocumentationLink, env)
}

// EncodeDocumentationLink marshals a documentationv1.Link into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeDocumentationLink for schema-version-1 payloads.
func EncodeDocumentationLink(link documentationv1.Link) (map[string]any, error) {
	return encodeToPayload(link)
}

// DecodeDocumentationEntityMention decodes env.Payload into the latest
// documentationv1.EntityMention struct for the "documentation_entity_mention"
// fact kind. See DecodeDocumentationSource for the dispatch and error
// contract. This is one of the two kinds a reducer decode site consumes
// today (ExtractDocumentationEdgeRows).
func DecodeDocumentationEntityMention(env Envelope) (documentationv1.EntityMention, error) {
	return decodeLatestMajor[documentationv1.EntityMention](FactKindDocumentationEntityMention, env)
}

// EncodeDocumentationEntityMention marshals a documentationv1.EntityMention
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeDocumentationEntityMention for schema-version-1 payloads.
func EncodeDocumentationEntityMention(mention documentationv1.EntityMention) (map[string]any, error) {
	return encodeToPayload(mention)
}

// DecodeDocumentationClaimCandidate decodes env.Payload into the latest
// documentationv1.ClaimCandidate struct for the
// "documentation_claim_candidate" fact kind. See DecodeDocumentationSource
// for the dispatch and error contract. This kind is typed but not yet
// consumed by any reducer or storage read path.
func DecodeDocumentationClaimCandidate(env Envelope) (documentationv1.ClaimCandidate, error) {
	return decodeLatestMajor[documentationv1.ClaimCandidate](FactKindDocumentationClaimCandidate, env)
}

// EncodeDocumentationClaimCandidate marshals a documentationv1.ClaimCandidate
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeDocumentationClaimCandidate for schema-version-1
// payloads.
func EncodeDocumentationClaimCandidate(claim documentationv1.ClaimCandidate) (map[string]any, error) {
	return encodeToPayload(claim)
}

// DecodeDocumentationFinding decodes env.Payload into the latest
// documentationv1.Finding struct for the "documentation_finding" fact kind.
// See DecodeDocumentationSource for the dispatch and error contract. This
// kind is typed but not yet consumed by any reducer decode site: it is
// emitted by go/internal/doctruth and read only by the query layer's raw SQL
// (documentation/v1/README.md).
func DecodeDocumentationFinding(env Envelope) (documentationv1.Finding, error) {
	return decodeLatestMajor[documentationv1.Finding](FactKindDocumentationFinding, env)
}

// EncodeDocumentationFinding marshals a documentationv1.Finding into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeDocumentationFinding for schema-version-1 payloads.
func EncodeDocumentationFinding(finding documentationv1.Finding) (map[string]any, error) {
	return encodeToPayload(finding)
}

// DecodeDocumentationEvidencePacket decodes env.Payload into the latest
// documentationv1.EvidencePacket struct for the
// "documentation_evidence_packet" fact kind. See DecodeDocumentationSource
// for the dispatch and error contract. This kind is typed but not yet
// consumed by any reducer decode site: it is emitted by
// go/internal/doctruth and read only by the query layer's raw SQL
// (documentation/v1/README.md).
func DecodeDocumentationEvidencePacket(env Envelope) (documentationv1.EvidencePacket, error) {
	return decodeLatestMajor[documentationv1.EvidencePacket](FactKindDocumentationEvidencePacket, env)
}

// EncodeDocumentationEvidencePacket marshals a documentationv1.EvidencePacket
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeDocumentationEvidencePacket for schema-version-1
// payloads.
func EncodeDocumentationEvidencePacket(packet documentationv1.EvidencePacket) (map[string]any, error) {
	return encodeToPayload(packet)
}
