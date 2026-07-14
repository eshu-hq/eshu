// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	documentationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/documentation/v1"
)

// DocumentationSourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_source" payload.
const DocumentationSourceSchemaID = schemaBaseID + "documentation/v1/source.schema.json"

// DocumentationSourceSchema returns the JSON Schema bytes for
// documentationv1.Source.
func DocumentationSourceSchema() ([]byte, error) {
	return reflectSchema(DocumentationSourceSchemaID, "Eshu documentation_source Payload (schema version 1)", &documentationv1.Source{})
}

// DocumentationDocumentSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_document" payload.
const DocumentationDocumentSchemaID = schemaBaseID + "documentation/v1/document.schema.json"

// DocumentationDocumentSchema returns the JSON Schema bytes for
// documentationv1.Document.
func DocumentationDocumentSchema() ([]byte, error) {
	return reflectSchema(DocumentationDocumentSchemaID, "Eshu documentation_document Payload (schema version 1)", &documentationv1.Document{})
}

// DocumentationSectionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1.1.0 "documentation_section" payload.
const DocumentationSectionSchemaID = schemaBaseID + "documentation/v1/section.schema.json"

// DocumentationSectionSchema returns the JSON Schema bytes for
// documentationv1.Section. The title names schema version 1.1.0
// (facts.DocumentationSectionFactSchemaVersion) because this kind is one
// minor ahead of the rest of the documentation family (which is 1.0.0); the
// decode seam still dispatches on the schema-version major only, mirroring
// gcp_cloud_resource's identical one-minor-ahead precedent.
func DocumentationSectionSchema() ([]byte, error) {
	return reflectSchema(DocumentationSectionSchemaID, "Eshu documentation_section Payload (schema version 1.1.0)", &documentationv1.Section{})
}

// DocumentationLinkSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_link" payload.
const DocumentationLinkSchemaID = schemaBaseID + "documentation/v1/link.schema.json"

// DocumentationLinkSchema returns the JSON Schema bytes for
// documentationv1.Link.
func DocumentationLinkSchema() ([]byte, error) {
	return reflectSchema(DocumentationLinkSchemaID, "Eshu documentation_link Payload (schema version 1)", &documentationv1.Link{})
}

// DocumentationEntityMentionSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "documentation_entity_mention" payload.
const DocumentationEntityMentionSchemaID = schemaBaseID + "documentation/v1/entity_mention.schema.json"

// DocumentationEntityMentionSchema returns the JSON Schema bytes for
// documentationv1.EntityMention.
func DocumentationEntityMentionSchema() ([]byte, error) {
	return reflectSchema(DocumentationEntityMentionSchemaID, "Eshu documentation_entity_mention Payload (schema version 1)", &documentationv1.EntityMention{})
}

// DocumentationClaimCandidateSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "documentation_claim_candidate" payload.
const DocumentationClaimCandidateSchemaID = schemaBaseID + "documentation/v1/claim_candidate.schema.json"

// DocumentationClaimCandidateSchema returns the JSON Schema bytes for
// documentationv1.ClaimCandidate.
func DocumentationClaimCandidateSchema() ([]byte, error) {
	return reflectSchema(DocumentationClaimCandidateSchemaID, "Eshu documentation_claim_candidate Payload (schema version 1)", &documentationv1.ClaimCandidate{})
}

// DocumentationFindingSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "documentation_finding" payload.
const DocumentationFindingSchemaID = schemaBaseID + "documentation/v1/finding.schema.json"

// DocumentationFindingSchema returns the JSON Schema bytes for
// documentationv1.Finding.
func DocumentationFindingSchema() ([]byte, error) {
	return reflectSchema(DocumentationFindingSchemaID, "Eshu documentation_finding Payload (schema version 1)", &documentationv1.Finding{})
}

// DocumentationEvidencePacketSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "documentation_evidence_packet" payload.
const DocumentationEvidencePacketSchemaID = schemaBaseID + "documentation/v1/evidence_packet.schema.json"

// DocumentationEvidencePacketSchema returns the JSON Schema bytes for
// documentationv1.EvidencePacket.
func DocumentationEvidencePacketSchema() ([]byte, error) {
	return reflectSchema(DocumentationEvidencePacketSchemaID, "Eshu documentation_evidence_packet Payload (schema version 1)", &documentationv1.EvidencePacket{})
}
