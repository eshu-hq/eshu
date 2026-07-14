// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// GCPCloudResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1.1.0 "gcp_cloud_resource" payload.
const GCPCloudResourceSchemaID = schemaBaseID + "gcp/v1/resource.schema.json"

// GCPCloudResourceSchema returns the JSON Schema bytes for gcpv1.Resource.
// The title names schema version 1.1.0 (facts.GCPCloudResourceSchemaVersion)
// because this kind is one minor ahead of the rest of the gcp family; the
// decode seam still dispatches on the schema-version major only.
func GCPCloudResourceSchema() ([]byte, error) {
	return reflectSchema(GCPCloudResourceSchemaID, "Eshu gcp_cloud_resource Payload (schema version 1.1.0)", &gcpv1.Resource{})
}

// GCPCloudRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_cloud_relationship" payload.
const GCPCloudRelationshipSchemaID = schemaBaseID + "gcp/v1/relationship.schema.json"

// GCPCloudRelationshipSchema returns the JSON Schema bytes for
// gcpv1.Relationship.
func GCPCloudRelationshipSchema() ([]byte, error) {
	return reflectSchema(GCPCloudRelationshipSchemaID, "Eshu gcp_cloud_relationship Payload (schema version 1)", &gcpv1.Relationship{})
}

// GCPCollectionWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_collection_warning" payload.
const GCPCollectionWarningSchemaID = schemaBaseID + "gcp/v1/collection_warning.schema.json"

// GCPCollectionWarningSchema returns the JSON Schema bytes for
// gcpv1.CollectionWarning.
func GCPCollectionWarningSchema() ([]byte, error) {
	return reflectSchema(GCPCollectionWarningSchemaID, "Eshu gcp_collection_warning Payload (schema version 1)", &gcpv1.CollectionWarning{})
}

// GCPDNSRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_dns_record" payload.
const GCPDNSRecordSchemaID = schemaBaseID + "gcp/v1/dns_record.schema.json"

// GCPDNSRecordSchema returns the JSON Schema bytes for gcpv1.DNSRecord.
func GCPDNSRecordSchema() ([]byte, error) {
	return reflectSchema(GCPDNSRecordSchemaID, "Eshu gcp_dns_record Payload (schema version 1)", &gcpv1.DNSRecord{})
}

// GCPIAMPolicyObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_iam_policy_observation" payload.
const GCPIAMPolicyObservationSchemaID = schemaBaseID + "gcp/v1/iam_policy_observation.schema.json"

// GCPIAMPolicyObservationSchema returns the JSON Schema bytes for
// gcpv1.IAMPolicyObservation.
func GCPIAMPolicyObservationSchema() ([]byte, error) {
	return reflectSchema(GCPIAMPolicyObservationSchemaID, "Eshu gcp_iam_policy_observation Payload (schema version 1)", &gcpv1.IAMPolicyObservation{})
}

// GCPTagObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_tag_observation" payload.
const GCPTagObservationSchemaID = schemaBaseID + "gcp/v1/tag_observation.schema.json"

// GCPTagObservationSchema returns the JSON Schema bytes for gcpv1.TagObservation.
func GCPTagObservationSchema() ([]byte, error) {
	return reflectSchema(GCPTagObservationSchemaID, "Eshu gcp_tag_observation Payload (schema version 1)", &gcpv1.TagObservation{})
}

// GCPImageReferenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "gcp_image_reference" payload.
const GCPImageReferenceSchemaID = schemaBaseID + "gcp/v1/image_reference.schema.json"

// GCPImageReferenceSchema returns the JSON Schema bytes for gcpv1.ImageReference.
func GCPImageReferenceSchema() ([]byte, error) {
	return reflectSchema(GCPImageReferenceSchemaID, "Eshu gcp_image_reference Payload (schema version 1)", &gcpv1.ImageReference{})
}
