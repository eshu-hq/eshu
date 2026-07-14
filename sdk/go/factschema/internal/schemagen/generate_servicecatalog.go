// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	servicecatalogv1 "github.com/eshu-hq/eshu/sdk/go/factschema/servicecatalog/v1"
)

// ServiceCatalogEntitySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "service_catalog.entity" payload.
const ServiceCatalogEntitySchemaID = schemaBaseID + "servicecatalog/v1/entity.schema.json"

// ServiceCatalogEntitySchema returns the JSON Schema bytes for
// servicecatalogv1.Entity.
func ServiceCatalogEntitySchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogEntitySchemaID, "Eshu service_catalog.entity Payload (schema version 1)", &servicecatalogv1.Entity{})
}

// ServiceCatalogOwnershipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "service_catalog.ownership" payload.
const ServiceCatalogOwnershipSchemaID = schemaBaseID + "servicecatalog/v1/ownership.schema.json"

// ServiceCatalogOwnershipSchema returns the JSON Schema bytes for
// servicecatalogv1.Ownership.
func ServiceCatalogOwnershipSchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogOwnershipSchemaID, "Eshu service_catalog.ownership Payload (schema version 1)", &servicecatalogv1.Ownership{})
}

// ServiceCatalogRepositoryLinkSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "service_catalog.repository_link" payload.
const ServiceCatalogRepositoryLinkSchemaID = schemaBaseID + "servicecatalog/v1/repository_link.schema.json"

// ServiceCatalogRepositoryLinkSchema returns the JSON Schema bytes for
// servicecatalogv1.RepositoryLink.
func ServiceCatalogRepositoryLinkSchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogRepositoryLinkSchemaID, "Eshu service_catalog.repository_link Payload (schema version 1)", &servicecatalogv1.RepositoryLink{})
}

// ServiceCatalogOperationalLinkSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "service_catalog.operational_link" payload.
const ServiceCatalogOperationalLinkSchemaID = schemaBaseID + "servicecatalog/v1/operational_link.schema.json"

// ServiceCatalogOperationalLinkSchema returns the JSON Schema bytes for
// servicecatalogv1.OperationalLink.
func ServiceCatalogOperationalLinkSchema() ([]byte, error) {
	return reflectSchema(ServiceCatalogOperationalLinkSchemaID, "Eshu service_catalog.operational_link Payload (schema version 1)", &servicecatalogv1.OperationalLink{})
}
