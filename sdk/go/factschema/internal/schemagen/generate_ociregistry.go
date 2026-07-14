// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

// OCIRegistryRepositorySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.repository" payload.
const OCIRegistryRepositorySchemaID = schemaBaseID + "ociregistry/v1/repository.schema.json"

// OCIRegistryRepositorySchema returns the JSON Schema bytes for
// ociregistryv1.Repository.
func OCIRegistryRepositorySchema() ([]byte, error) {
	return reflectSchema(OCIRegistryRepositorySchemaID, "Eshu oci_registry.repository Payload (schema version 1)", &ociregistryv1.Repository{})
}

// OCIImageManifestSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_manifest" payload.
const OCIImageManifestSchemaID = schemaBaseID + "ociregistry/v1/image_manifest.schema.json"

// OCIImageManifestSchema returns the JSON Schema bytes for
// ociregistryv1.ImageManifest.
func OCIImageManifestSchema() ([]byte, error) {
	return reflectSchema(OCIImageManifestSchemaID, "Eshu oci_registry.image_manifest Payload (schema version 1)", &ociregistryv1.ImageManifest{})
}

// OCIImageIndexSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_index" payload.
const OCIImageIndexSchemaID = schemaBaseID + "ociregistry/v1/image_index.schema.json"

// OCIImageIndexSchema returns the JSON Schema bytes for
// ociregistryv1.ImageIndex.
func OCIImageIndexSchema() ([]byte, error) {
	return reflectSchema(OCIImageIndexSchemaID, "Eshu oci_registry.image_index Payload (schema version 1)", &ociregistryv1.ImageIndex{})
}

// OCIImageDescriptorSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_descriptor" payload.
const OCIImageDescriptorSchemaID = schemaBaseID + "ociregistry/v1/image_descriptor.schema.json"

// OCIImageDescriptorSchema returns the JSON Schema bytes for
// ociregistryv1.ImageDescriptor.
func OCIImageDescriptorSchema() ([]byte, error) {
	return reflectSchema(OCIImageDescriptorSchemaID, "Eshu oci_registry.image_descriptor Payload (schema version 1)", &ociregistryv1.ImageDescriptor{})
}

// OCIImageTagObservationSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_tag_observation" payload.
const OCIImageTagObservationSchemaID = schemaBaseID + "ociregistry/v1/tag_observation.schema.json"

// OCIImageTagObservationSchema returns the JSON Schema bytes for
// ociregistryv1.TagObservation.
func OCIImageTagObservationSchema() ([]byte, error) {
	return reflectSchema(OCIImageTagObservationSchemaID, "Eshu oci_registry.image_tag_observation Payload (schema version 1)", &ociregistryv1.TagObservation{})
}

// OCIImageReferrerSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.image_referrer" payload.
const OCIImageReferrerSchemaID = schemaBaseID + "ociregistry/v1/image_referrer.schema.json"

// OCIImageReferrerSchema returns the JSON Schema bytes for
// ociregistryv1.ImageReferrer.
func OCIImageReferrerSchema() ([]byte, error) {
	return reflectSchema(OCIImageReferrerSchemaID, "Eshu oci_registry.image_referrer Payload (schema version 1)", &ociregistryv1.ImageReferrer{})
}

// OCIRegistryWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "oci_registry.warning" payload.
const OCIRegistryWarningSchemaID = schemaBaseID + "ociregistry/v1/warning.schema.json"

// OCIRegistryWarningSchema returns the JSON Schema bytes for
// ociregistryv1.Warning. This kind is deferred (typed-but-not-consumed), but
// its schema is still generated so the kind is contract-complete.
func OCIRegistryWarningSchema() ([]byte, error) {
	return reflectSchema(OCIRegistryWarningSchemaID, "Eshu oci_registry.warning Payload (schema version 1)", &ociregistryv1.Warning{})
}
