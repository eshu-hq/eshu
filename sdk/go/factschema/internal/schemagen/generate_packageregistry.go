// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
)

// PackageRegistryPackageSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "package_registry.package" payload.
const PackageRegistryPackageSchemaID = schemaBaseID + "packageregistry/v1/package.schema.json"

// PackageRegistryPackageSchema returns the JSON Schema bytes for
// packageregistryv1.Package.
func PackageRegistryPackageSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageSchemaID, "Eshu package_registry.package Payload (schema version 1)", &packageregistryv1.Package{})
}

// PackageRegistryPackageVersionSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "package_registry.package_version" payload.
const PackageRegistryPackageVersionSchemaID = schemaBaseID + "packageregistry/v1/package_version.schema.json"

// PackageRegistryPackageVersionSchema returns the JSON Schema bytes for
// packageregistryv1.PackageVersion.
func PackageRegistryPackageVersionSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageVersionSchemaID, "Eshu package_registry.package_version Payload (schema version 1)", &packageregistryv1.PackageVersion{})
}

// PackageRegistryPackageDependencySchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "package_registry.package_dependency" payload.
const PackageRegistryPackageDependencySchemaID = schemaBaseID + "packageregistry/v1/package_dependency.schema.json"

// PackageRegistryPackageDependencySchema returns the JSON Schema bytes for
// packageregistryv1.PackageDependency.
func PackageRegistryPackageDependencySchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageDependencySchemaID, "Eshu package_registry.package_dependency Payload (schema version 1)", &packageregistryv1.PackageDependency{})
}

// PackageRegistrySourceHintSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "package_registry.source_hint" payload.
const PackageRegistrySourceHintSchemaID = schemaBaseID + "packageregistry/v1/source_hint.schema.json"

// PackageRegistrySourceHintSchema returns the JSON Schema bytes for
// packageregistryv1.SourceHint.
func PackageRegistrySourceHintSchema() ([]byte, error) {
	return reflectSchema(PackageRegistrySourceHintSchemaID, "Eshu package_registry.source_hint Payload (schema version 1)", &packageregistryv1.SourceHint{})
}

// PackageRegistryPackageArtifactSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "package_registry.package_artifact" payload.
const PackageRegistryPackageArtifactSchemaID = schemaBaseID + "packageregistry/v1/package_artifact.schema.json"

// PackageRegistryPackageArtifactSchema returns the JSON Schema bytes for
// packageregistryv1.PackageArtifact.
func PackageRegistryPackageArtifactSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryPackageArtifactSchemaID, "Eshu package_registry.package_artifact Payload (schema version 1)", &packageregistryv1.PackageArtifact{})
}

// PackageRegistryVulnerabilityHintSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "package_registry.vulnerability_hint" payload.
const PackageRegistryVulnerabilityHintSchemaID = schemaBaseID + "packageregistry/v1/vulnerability_hint.schema.json"

// PackageRegistryVulnerabilityHintSchema returns the JSON Schema bytes for
// packageregistryv1.VulnerabilityHint.
func PackageRegistryVulnerabilityHintSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryVulnerabilityHintSchemaID, "Eshu package_registry.vulnerability_hint Payload (schema version 1)", &packageregistryv1.VulnerabilityHint{})
}

// PackageRegistryRegistryEventSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "package_registry.registry_event" payload.
const PackageRegistryRegistryEventSchemaID = schemaBaseID + "packageregistry/v1/registry_event.schema.json"

// PackageRegistryRegistryEventSchema returns the JSON Schema bytes for
// packageregistryv1.RegistryEvent.
func PackageRegistryRegistryEventSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryRegistryEventSchemaID, "Eshu package_registry.registry_event Payload (schema version 1)", &packageregistryv1.RegistryEvent{})
}

// PackageRegistryRepositoryHostingSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "package_registry.repository_hosting" payload.
const PackageRegistryRepositoryHostingSchemaID = schemaBaseID + "packageregistry/v1/repository_hosting.schema.json"

// PackageRegistryRepositoryHostingSchema returns the JSON Schema bytes for
// packageregistryv1.RepositoryHosting.
func PackageRegistryRepositoryHostingSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryRepositoryHostingSchemaID, "Eshu package_registry.repository_hosting Payload (schema version 1)", &packageregistryv1.RepositoryHosting{})
}

// PackageRegistryWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "package_registry.warning" payload.
const PackageRegistryWarningSchemaID = schemaBaseID + "packageregistry/v1/warning.schema.json"

// PackageRegistryWarningSchema returns the JSON Schema bytes for
// packageregistryv1.Warning.
func PackageRegistryWarningSchema() ([]byte, error) {
	return reflectSchema(PackageRegistryWarningSchemaID, "Eshu package_registry.warning Payload (schema version 1)", &packageregistryv1.Warning{})
}
