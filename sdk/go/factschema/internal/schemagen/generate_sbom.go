// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
)

// SBOMDocumentSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.document" payload.
const SBOMDocumentSchemaID = schemaBaseID + "sbom/v1/document.schema.json"

// SBOMDocumentSchema returns the JSON Schema bytes for sbomv1.Document.
func SBOMDocumentSchema() ([]byte, error) {
	return reflectSchema(SBOMDocumentSchemaID, "Eshu sbom.document Payload (schema version 1)", &sbomv1.Document{})
}

// SBOMComponentSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.component" payload.
const SBOMComponentSchemaID = schemaBaseID + "sbom/v1/component.schema.json"

// SBOMComponentSchema returns the JSON Schema bytes for sbomv1.Component.
func SBOMComponentSchema() ([]byte, error) {
	return reflectSchema(SBOMComponentSchemaID, "Eshu sbom.component Payload (schema version 1)", &sbomv1.Component{})
}

// SBOMDependencyRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.dependency_relationship" payload.
const SBOMDependencyRelationshipSchemaID = schemaBaseID + "sbom/v1/dependency_relationship.schema.json"

// SBOMDependencyRelationshipSchema returns the JSON Schema bytes for
// sbomv1.DependencyRelationship.
func SBOMDependencyRelationshipSchema() ([]byte, error) {
	return reflectSchema(SBOMDependencyRelationshipSchemaID, "Eshu sbom.dependency_relationship Payload (schema version 1)", &sbomv1.DependencyRelationship{})
}

// SBOMExternalReferenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.external_reference" payload.
const SBOMExternalReferenceSchemaID = schemaBaseID + "sbom/v1/external_reference.schema.json"

// SBOMExternalReferenceSchema returns the JSON Schema bytes for
// sbomv1.ExternalReference.
func SBOMExternalReferenceSchema() ([]byte, error) {
	return reflectSchema(SBOMExternalReferenceSchemaID, "Eshu sbom.external_reference Payload (schema version 1)", &sbomv1.ExternalReference{})
}

// SBOMWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "sbom.warning" payload.
const SBOMWarningSchemaID = schemaBaseID + "sbom/v1/warning.schema.json"

// SBOMWarningSchema returns the JSON Schema bytes for sbomv1.Warning.
func SBOMWarningSchema() ([]byte, error) {
	return reflectSchema(SBOMWarningSchemaID, "Eshu sbom.warning Payload (schema version 1)", &sbomv1.Warning{})
}

// AttestationStatementSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "attestation.statement" payload.
const AttestationStatementSchemaID = schemaBaseID + "sbom/v1/statement.schema.json"

// AttestationStatementSchema returns the JSON Schema bytes for
// sbomv1.Statement.
func AttestationStatementSchema() ([]byte, error) {
	return reflectSchema(AttestationStatementSchemaID, "Eshu attestation.statement Payload (schema version 1)", &sbomv1.Statement{})
}

// AttestationSignatureVerificationSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "attestation.signature_verification" payload.
const AttestationSignatureVerificationSchemaID = schemaBaseID + "sbom/v1/signature_verification.schema.json"

// AttestationSignatureVerificationSchema returns the JSON Schema bytes for
// sbomv1.SignatureVerification.
func AttestationSignatureVerificationSchema() ([]byte, error) {
	return reflectSchema(AttestationSignatureVerificationSchemaID, "Eshu attestation.signature_verification Payload (schema version 1)", &sbomv1.SignatureVerification{})
}

// AttestationSLSAProvenanceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "attestation.slsa_provenance" payload.
const AttestationSLSAProvenanceSchemaID = schemaBaseID + "sbom/v1/slsa_provenance.schema.json"

// AttestationSLSAProvenanceSchema returns the JSON Schema bytes for
// sbomv1.SLSAProvenance.
func AttestationSLSAProvenanceSchema() ([]byte, error) {
	return reflectSchema(AttestationSLSAProvenanceSchemaID, "Eshu attestation.slsa_provenance Payload (schema version 1)", &sbomv1.SLSAProvenance{})
}
