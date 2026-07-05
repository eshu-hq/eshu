// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// DecodeSBOMDocument decodes env.Payload into the latest sbomv1.Document
// struct for the "sbom.document" fact kind, dispatching on env.SchemaVersion
// major per Contract System v1 §3.2. Callers (reducer handlers) receive
// either the decoded struct or a classified *DecodeError; they must never
// substitute a zero-value struct on error.
func DecodeSBOMDocument(env Envelope) (sbomv1.Document, error) {
	return decodeLatestMajor[sbomv1.Document](FactKindSBOMDocument, env)
}

// EncodeSBOMDocument marshals a sbomv1.Document into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeSBOMDocument
// for schema-version-1 payloads, used by collectors emitting this fact kind
// and by this module's round-trip tests.
func EncodeSBOMDocument(document sbomv1.Document) (map[string]any, error) {
	return encodeToPayload(document)
}

// DecodeSBOMComponent decodes env.Payload into the latest sbomv1.Component
// struct for the "sbom.component" fact kind. See DecodeSBOMDocument for the
// dispatch and error contract.
func DecodeSBOMComponent(env Envelope) (sbomv1.Component, error) {
	return decodeLatestMajor[sbomv1.Component](FactKindSBOMComponent, env)
}

// EncodeSBOMComponent marshals a sbomv1.Component into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeSBOMComponent
// for schema-version-1 payloads.
func EncodeSBOMComponent(component sbomv1.Component) (map[string]any, error) {
	return encodeToPayload(component)
}

// DecodeSBOMDependencyRelationship decodes env.Payload into the latest
// sbomv1.DependencyRelationship struct for the "sbom.dependency_relationship"
// fact kind. See DecodeSBOMDocument for the dispatch and error contract. This
// kind is typed but not yet consumed by any reducer handler (sbom/v1/doc.go).
func DecodeSBOMDependencyRelationship(env Envelope) (sbomv1.DependencyRelationship, error) {
	return decodeLatestMajor[sbomv1.DependencyRelationship](FactKindSBOMDependencyRelationship, env)
}

// EncodeSBOMDependencyRelationship marshals a sbomv1.DependencyRelationship
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeSBOMDependencyRelationship for schema-version-1 payloads.
func EncodeSBOMDependencyRelationship(relationship sbomv1.DependencyRelationship) (map[string]any, error) {
	return encodeToPayload(relationship)
}

// DecodeSBOMExternalReference decodes env.Payload into the latest
// sbomv1.ExternalReference struct for the "sbom.external_reference" fact
// kind. See DecodeSBOMDocument for the dispatch and error contract. This kind
// is typed but not yet consumed by any reducer handler (sbom/v1/doc.go).
func DecodeSBOMExternalReference(env Envelope) (sbomv1.ExternalReference, error) {
	return decodeLatestMajor[sbomv1.ExternalReference](FactKindSBOMExternalReference, env)
}

// EncodeSBOMExternalReference marshals a sbomv1.ExternalReference into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeSBOMExternalReference for schema-version-1 payloads.
func EncodeSBOMExternalReference(reference sbomv1.ExternalReference) (map[string]any, error) {
	return encodeToPayload(reference)
}

// DecodeSBOMWarning decodes env.Payload into the latest sbomv1.Warning
// struct for the "sbom.warning" fact kind. See DecodeSBOMDocument for the
// dispatch and error contract. sbomv1.Warning has zero required fields (see
// its godoc), so this never returns a missing-required-field error; only an
// unsupported schema major or an undecodable payload shape fails.
func DecodeSBOMWarning(env Envelope) (sbomv1.Warning, error) {
	return decodeLatestMajor[sbomv1.Warning](FactKindSBOMWarning, env)
}

// EncodeSBOMWarning marshals a sbomv1.Warning into the map[string]any
// payload shape an Envelope carries. It is the inverse of DecodeSBOMWarning
// for schema-version-1 payloads.
func EncodeSBOMWarning(warning sbomv1.Warning) (map[string]any, error) {
	return encodeToPayload(warning)
}

// DecodeAttestationStatement decodes env.Payload into the latest
// sbomv1.Statement struct for the "attestation.statement" fact kind. See
// DecodeSBOMDocument for the dispatch and error contract.
func DecodeAttestationStatement(env Envelope) (sbomv1.Statement, error) {
	return decodeLatestMajor[sbomv1.Statement](FactKindAttestationStatement, env)
}

// EncodeAttestationStatement marshals a sbomv1.Statement into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAttestationStatement for schema-version-1 payloads.
func EncodeAttestationStatement(statement sbomv1.Statement) (map[string]any, error) {
	return encodeToPayload(statement)
}

// DecodeAttestationSignatureVerification decodes env.Payload into the latest
// sbomv1.SignatureVerification struct for the
// "attestation.signature_verification" fact kind. See DecodeSBOMDocument for
// the dispatch and error contract.
func DecodeAttestationSignatureVerification(env Envelope) (sbomv1.SignatureVerification, error) {
	return decodeLatestMajor[sbomv1.SignatureVerification](FactKindAttestationSignatureVerification, env)
}

// EncodeAttestationSignatureVerification marshals a
// sbomv1.SignatureVerification into the map[string]any payload shape an
// Envelope carries. It is the inverse of
// DecodeAttestationSignatureVerification for schema-version-1 payloads.
func EncodeAttestationSignatureVerification(verification sbomv1.SignatureVerification) (map[string]any, error) {
	return encodeToPayload(verification)
}

// DecodeAttestationSLSAProvenance decodes env.Payload into the latest
// sbomv1.SLSAProvenance struct for the "attestation.slsa_provenance" fact
// kind. See DecodeSBOMDocument for the dispatch and error contract. This kind
// is typed but not yet consumed by any reducer handler, nor emitted by any
// collector today (sbom/v1/doc.go).
func DecodeAttestationSLSAProvenance(env Envelope) (sbomv1.SLSAProvenance, error) {
	return decodeLatestMajor[sbomv1.SLSAProvenance](FactKindAttestationSLSAProvenance, env)
}

// EncodeAttestationSLSAProvenance marshals a sbomv1.SLSAProvenance into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeAttestationSLSAProvenance for schema-version-1 payloads.
func EncodeAttestationSLSAProvenance(provenance sbomv1.SLSAProvenance) (map[string]any, error) {
	return encodeToPayload(provenance)
}
