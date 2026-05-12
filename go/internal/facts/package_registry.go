package facts

import "slices"

const (
	// PackageRegistryPackageFactKind identifies one package identity observed in
	// a package registry or feed.
	PackageRegistryPackageFactKind = "package_registry.package"
	// PackageRegistryPackageVersionFactKind identifies one package version
	// observed in a package registry or feed.
	PackageRegistryPackageVersionFactKind = "package_registry.package_version"
	// PackageRegistryPackageDependencyFactKind identifies one package-version
	// dependency edge reported by package-native metadata.
	PackageRegistryPackageDependencyFactKind = "package_registry.package_dependency"
	// PackageRegistryPackageArtifactFactKind identifies one package artifact
	// file, digest, classifier, or platform coordinate.
	PackageRegistryPackageArtifactFactKind = "package_registry.package_artifact"
	// PackageRegistrySourceHintFactKind identifies source repository, homepage,
	// SCM, or provenance hints reported by package metadata.
	PackageRegistrySourceHintFactKind = "package_registry.source_hint"
	// PackageRegistryVulnerabilityHintFactKind identifies advisory metadata
	// reported directly by a registry without assigning severity policy.
	PackageRegistryVulnerabilityHintFactKind = "package_registry.vulnerability_hint"
	// PackageRegistryRegistryEventFactKind identifies a publish, delete,
	// unlist, deprecate, yank, relist, or metadata-mutation event.
	PackageRegistryRegistryEventFactKind = "package_registry.registry_event"
	// PackageRegistryRepositoryHostingFactKind identifies provider repository
	// topology such as Artifactory local, remote, or virtual feeds.
	PackageRegistryRepositoryHostingFactKind = "package_registry.repository_hosting"
	// PackageRegistryWarningFactKind identifies non-fatal package-registry
	// collection warnings.
	PackageRegistryWarningFactKind = "package_registry.warning"

	// PackageRegistryPackageSchemaVersion is the first package fact schema.
	PackageRegistryPackageSchemaVersion = "1.0.0"
	// PackageRegistryPackageVersionSchemaVersion is the first version fact schema.
	PackageRegistryPackageVersionSchemaVersion = "1.0.0"
	// PackageRegistryPackageDependencySchemaVersion is the first dependency fact schema.
	PackageRegistryPackageDependencySchemaVersion = "1.0.0"
	// PackageRegistryPackageArtifactSchemaVersion is the first artifact fact schema.
	PackageRegistryPackageArtifactSchemaVersion = "1.0.0"
	// PackageRegistrySourceHintSchemaVersion is the first source hint fact schema.
	PackageRegistrySourceHintSchemaVersion = "1.0.0"
	// PackageRegistryVulnerabilityHintSchemaVersion is the first vulnerability
	// hint fact schema.
	PackageRegistryVulnerabilityHintSchemaVersion = "1.0.0"
	// PackageRegistryRegistryEventSchemaVersion is the first registry event fact schema.
	PackageRegistryRegistryEventSchemaVersion = "1.0.0"
	// PackageRegistryRepositoryHostingSchemaVersion is the first repository
	// hosting fact schema.
	PackageRegistryRepositoryHostingSchemaVersion = "1.0.0"
	// PackageRegistryWarningSchemaVersion is the first warning fact schema.
	PackageRegistryWarningSchemaVersion = "1.0.0"
)

var packageRegistryFactKinds = []string{
	PackageRegistryPackageFactKind,
	PackageRegistryPackageVersionFactKind,
	PackageRegistryPackageDependencyFactKind,
	PackageRegistryPackageArtifactFactKind,
	PackageRegistrySourceHintFactKind,
	PackageRegistryVulnerabilityHintFactKind,
	PackageRegistryRegistryEventFactKind,
	PackageRegistryRepositoryHostingFactKind,
	PackageRegistryWarningFactKind,
}

var packageRegistrySchemaVersions = map[string]string{
	PackageRegistryPackageFactKind:           PackageRegistryPackageSchemaVersion,
	PackageRegistryPackageVersionFactKind:    PackageRegistryPackageVersionSchemaVersion,
	PackageRegistryPackageDependencyFactKind: PackageRegistryPackageDependencySchemaVersion,
	PackageRegistryPackageArtifactFactKind:   PackageRegistryPackageArtifactSchemaVersion,
	PackageRegistrySourceHintFactKind:        PackageRegistrySourceHintSchemaVersion,
	PackageRegistryVulnerabilityHintFactKind: PackageRegistryVulnerabilityHintSchemaVersion,
	PackageRegistryRegistryEventFactKind:     PackageRegistryRegistryEventSchemaVersion,
	PackageRegistryRepositoryHostingFactKind: PackageRegistryRepositoryHostingSchemaVersion,
	PackageRegistryWarningFactKind:           PackageRegistryWarningSchemaVersion,
}

// PackageRegistryFactKinds returns the accepted package-registry fact kinds in
// their emission order.
func PackageRegistryFactKinds() []string {
	return slices.Clone(packageRegistryFactKinds)
}

// PackageRegistrySchemaVersion returns the schema version for a package-registry
// fact kind.
func PackageRegistrySchemaVersion(factKind string) (string, bool) {
	version, ok := packageRegistrySchemaVersions[factKind]
	return version, ok
}
