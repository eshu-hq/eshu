// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// AzureCloudResourceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_cloud_resource" payload.
const AzureCloudResourceSchemaID = schemaBaseID + "azure/v1/cloud_resource.schema.json"

// AzureCloudResourceSchema returns the JSON Schema bytes for
// azurev1.CloudResource.
func AzureCloudResourceSchema() ([]byte, error) {
	return reflectSchema(AzureCloudResourceSchemaID, "Eshu azure_cloud_resource Payload (schema version 1)", &azurev1.CloudResource{})
}

// AzureCloudRelationshipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_cloud_relationship" payload.
const AzureCloudRelationshipSchemaID = schemaBaseID + "azure/v1/cloud_relationship.schema.json"

// AzureCloudRelationshipSchema returns the JSON Schema bytes for
// azurev1.CloudRelationship.
func AzureCloudRelationshipSchema() ([]byte, error) {
	return reflectSchema(AzureCloudRelationshipSchemaID, "Eshu azure_cloud_relationship Payload (schema version 1)", &azurev1.CloudRelationship{})
}

// AzureDNSRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_dns_record" payload.
const AzureDNSRecordSchemaID = schemaBaseID + "azure/v1/dns_record.schema.json"

// AzureDNSRecordSchema returns the JSON Schema bytes for azurev1.DNSRecord.
func AzureDNSRecordSchema() ([]byte, error) {
	return reflectSchema(AzureDNSRecordSchemaID, "Eshu azure_dns_record Payload (schema version 1)", &azurev1.DNSRecord{})
}

// AzureCollectionWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_collection_warning" payload.
const AzureCollectionWarningSchemaID = schemaBaseID + "azure/v1/collection_warning.schema.json"

// AzureCollectionWarningSchema returns the JSON Schema bytes for
// azurev1.CollectionWarning.
func AzureCollectionWarningSchema() ([]byte, error) {
	return reflectSchema(AzureCollectionWarningSchemaID, "Eshu azure_collection_warning Payload (schema version 1)", &azurev1.CollectionWarning{})
}

// AzureTagObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_tag_observation" payload.
const AzureTagObservationSchemaID = schemaBaseID + "azure/v1/tag_observation.schema.json"

// AzureTagObservationSchema returns the JSON Schema bytes for azurev1.TagObservation.
func AzureTagObservationSchema() ([]byte, error) {
	return reflectSchema(AzureTagObservationSchemaID, "Eshu azure_tag_observation Payload (schema version 1)", &azurev1.TagObservation{})
}

// AzureIdentityObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_identity_observation" payload.
const AzureIdentityObservationSchemaID = schemaBaseID + "azure/v1/identity_observation.schema.json"

// AzureIdentityObservationSchema returns the JSON Schema bytes for azurev1.IdentityObservation.
func AzureIdentityObservationSchema() ([]byte, error) {
	return reflectSchema(AzureIdentityObservationSchemaID, "Eshu azure_identity_observation Payload (schema version 1)", &azurev1.IdentityObservation{})
}

// AzureResourceChangeSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_resource_change" payload.
const AzureResourceChangeSchemaID = schemaBaseID + "azure/v1/resource_change.schema.json"

// AzureResourceChangeSchema returns the JSON Schema bytes for azurev1.ResourceChange.
func AzureResourceChangeSchema() ([]byte, error) {
	return reflectSchema(AzureResourceChangeSchemaID, "Eshu azure_resource_change Payload (schema version 1)", &azurev1.ResourceChange{})
}

// AzureImageReferenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "azure_image_reference" payload.
const AzureImageReferenceSchemaID = schemaBaseID + "azure/v1/image_reference.schema.json"

// AzureImageReferenceSchema returns the JSON Schema bytes for azurev1.ImageReference.
func AzureImageReferenceSchema() ([]byte, error) {
	return reflectSchema(AzureImageReferenceSchemaID, "Eshu azure_image_reference Payload (schema version 1)", &azurev1.ImageReference{})
}
