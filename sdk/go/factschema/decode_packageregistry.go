// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
)

// DecodePackageRegistryPackage decodes env.Payload into the latest
// packageregistryv1.Package struct for the "package_registry.package" fact
// kind, dispatching on env.SchemaVersion major per Contract System v1 §3.2. A
// payload missing the required "package_id" key (or supplying it as null)
// dead-letters as a classified input_invalid error naming the field, rather
// than fabricating an empty-identity package node.
func DecodePackageRegistryPackage(env Envelope) (packageregistryv1.Package, error) {
	return decodeLatestMajor[packageregistryv1.Package](FactKindPackageRegistryPackage, env)
}

// EncodePackageRegistryPackage marshals a packageregistryv1.Package into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodePackageRegistryPackage for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodePackageRegistryPackage(pkg packageregistryv1.Package) (map[string]any, error) {
	return encodeToPayload(pkg)
}

// DecodePackageRegistryPackageVersion decodes env.Payload into the latest
// packageregistryv1.PackageVersion struct for the
// "package_registry.package_version" fact kind. A payload missing any of the
// required "package_id", "version_id", or "version" keys dead-letters as
// input_invalid. See DecodePackageRegistryPackage for the dispatch and error
// contract.
func DecodePackageRegistryPackageVersion(env Envelope) (packageregistryv1.PackageVersion, error) {
	return decodeLatestMajor[packageregistryv1.PackageVersion](FactKindPackageRegistryPackageVersion, env)
}

// EncodePackageRegistryPackageVersion marshals a
// packageregistryv1.PackageVersion into the map[string]any payload shape an
// Envelope carries.
func EncodePackageRegistryPackageVersion(version packageregistryv1.PackageVersion) (map[string]any, error) {
	return encodeToPayload(version)
}

// DecodePackageRegistryPackageDependency decodes env.Payload into the latest
// packageregistryv1.PackageDependency struct for the
// "package_registry.package_dependency" fact kind. A payload missing any of
// the required "package_id", "version_id", or "dependency_package_id" join
// keys dead-letters as input_invalid, rather than silently breaking the
// dependency edge's join. See DecodePackageRegistryPackage for the dispatch
// and error contract.
func DecodePackageRegistryPackageDependency(env Envelope) (packageregistryv1.PackageDependency, error) {
	return decodeLatestMajor[packageregistryv1.PackageDependency](FactKindPackageRegistryPackageDependency, env)
}

// EncodePackageRegistryPackageDependency marshals a
// packageregistryv1.PackageDependency into the map[string]any payload shape an
// Envelope carries.
func EncodePackageRegistryPackageDependency(dependency packageregistryv1.PackageDependency) (map[string]any, error) {
	return encodeToPayload(dependency)
}

// DecodePackageRegistrySourceHint decodes env.Payload into the latest
// packageregistryv1.SourceHint struct for the "package_registry.source_hint"
// fact kind. This kind is typed-but-not-yet-consumed through this module's
// decode seam (packageregistry/v1/doc.go): the seam exists so the contract is
// ready, but the reducer's package_source_correlation domain reads this
// payload through raw map access today, not through this seam. A payload
// missing a required identity field (package_id, hint_kind) dead-letters as
// input_invalid.
func DecodePackageRegistrySourceHint(env Envelope) (packageregistryv1.SourceHint, error) {
	return decodeLatestMajor[packageregistryv1.SourceHint](FactKindPackageRegistrySourceHint, env)
}

// EncodePackageRegistrySourceHint marshals a packageregistryv1.SourceHint into
// the map[string]any payload shape an Envelope carries.
func EncodePackageRegistrySourceHint(hint packageregistryv1.SourceHint) (map[string]any, error) {
	return encodeToPayload(hint)
}

// DecodePackageRegistryPackageArtifact decodes env.Payload into the latest
// packageregistryv1.PackageArtifact struct for the
// "package_registry.package_artifact" fact kind. Typed-but-not-yet-consumed.
// A payload missing a required identity field (package_id, version_id,
// artifact_key) dead-letters as input_invalid.
func DecodePackageRegistryPackageArtifact(env Envelope) (packageregistryv1.PackageArtifact, error) {
	return decodeLatestMajor[packageregistryv1.PackageArtifact](FactKindPackageRegistryPackageArtifact, env)
}

// EncodePackageRegistryPackageArtifact marshals a
// packageregistryv1.PackageArtifact into the map[string]any payload shape an
// Envelope carries.
func EncodePackageRegistryPackageArtifact(artifact packageregistryv1.PackageArtifact) (map[string]any, error) {
	return encodeToPayload(artifact)
}

// DecodePackageRegistryVulnerabilityHint decodes env.Payload into the latest
// packageregistryv1.VulnerabilityHint struct for the
// "package_registry.vulnerability_hint" fact kind. Typed-but-not-yet-consumed
// through this seam; its package_id field is read by a raw-SQL-JSONB loader
// (packageregistry/v1/doc.go). A payload missing a required field
// (package_id, advisory_id, advisory_source) dead-letters as input_invalid.
func DecodePackageRegistryVulnerabilityHint(env Envelope) (packageregistryv1.VulnerabilityHint, error) {
	return decodeLatestMajor[packageregistryv1.VulnerabilityHint](FactKindPackageRegistryVulnerabilityHint, env)
}

// EncodePackageRegistryVulnerabilityHint marshals a
// packageregistryv1.VulnerabilityHint into the map[string]any payload shape an
// Envelope carries.
func EncodePackageRegistryVulnerabilityHint(hint packageregistryv1.VulnerabilityHint) (map[string]any, error) {
	return encodeToPayload(hint)
}

// DecodePackageRegistryRegistryEvent decodes env.Payload into the latest
// packageregistryv1.RegistryEvent struct for the
// "package_registry.registry_event" fact kind. Typed-but-not-yet-consumed. A
// payload missing a required field (event_key, event_type) dead-letters as
// input_invalid.
func DecodePackageRegistryRegistryEvent(env Envelope) (packageregistryv1.RegistryEvent, error) {
	return decodeLatestMajor[packageregistryv1.RegistryEvent](FactKindPackageRegistryRegistryEvent, env)
}

// EncodePackageRegistryRegistryEvent marshals a
// packageregistryv1.RegistryEvent into the map[string]any payload shape an
// Envelope carries.
func EncodePackageRegistryRegistryEvent(event packageregistryv1.RegistryEvent) (map[string]any, error) {
	return encodeToPayload(event)
}

// DecodePackageRegistryRepositoryHosting decodes env.Payload into the latest
// packageregistryv1.RepositoryHosting struct for the
// "package_registry.repository_hosting" fact kind. Typed-but-not-yet-consumed.
// A payload missing a required field (provider, registry, repository)
// dead-letters as input_invalid.
func DecodePackageRegistryRepositoryHosting(env Envelope) (packageregistryv1.RepositoryHosting, error) {
	return decodeLatestMajor[packageregistryv1.RepositoryHosting](FactKindPackageRegistryRepositoryHosting, env)
}

// EncodePackageRegistryRepositoryHosting marshals a
// packageregistryv1.RepositoryHosting into the map[string]any payload shape an
// Envelope carries.
func EncodePackageRegistryRepositoryHosting(hosting packageregistryv1.RepositoryHosting) (map[string]any, error) {
	return encodeToPayload(hosting)
}

// DecodePackageRegistryWarning decodes env.Payload into the latest
// packageregistryv1.Warning struct for the "package_registry.warning" fact
// kind. Typed-but-not-yet-consumed through this seam; its ecosystem and
// warning_code fields are read by a raw-SQL-JSONB loader
// (packageregistry/v1/doc.go). A payload missing a required field
// (warning_key, warning_code) dead-letters as input_invalid.
func DecodePackageRegistryWarning(env Envelope) (packageregistryv1.Warning, error) {
	return decodeLatestMajor[packageregistryv1.Warning](FactKindPackageRegistryWarning, env)
}

// EncodePackageRegistryWarning marshals a packageregistryv1.Warning into the
// map[string]any payload shape an Envelope carries.
func EncodePackageRegistryWarning(warning packageregistryv1.Warning) (map[string]any, error) {
	return encodeToPayload(warning)
}
