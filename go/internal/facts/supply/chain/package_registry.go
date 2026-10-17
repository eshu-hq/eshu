// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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

// PackageRegistryFamilyDeclaration is the typed fact-family contract declaration for the
// package_registry family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var PackageRegistryFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "package_registry",
	FactDir:                "go/internal/facts/supply/chain",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "package_source_correlation",
	ProjectionHook:         "package_source_correlation",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/package-registry/packages",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "package_registry.package", PayloadSchema: "sdk/go/factschema/schema/package_registry.package.v1.schema.json"},
		{Kind: "package_registry.package_artifact", PayloadSchema: "sdk/go/factschema/schema/package_registry.package_artifact.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode"},
		{Kind: "package_registry.package_dependency", PayloadSchema: "sdk/go/factschema/schema/package_registry.package_dependency.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode"},
		{Kind: "package_registry.package_version", PayloadSchema: "sdk/go/factschema/schema/package_registry.package_version.v1.schema.json"},
		{Kind: "package_registry.registry_event", PayloadSchema: "sdk/go/factschema/schema/package_registry.registry_event.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode"},
		{Kind: "package_registry.repository_hosting", PayloadSchema: "sdk/go/factschema/schema/package_registry.repository_hosting.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Registry feed topology with no graph consumer; it does not advance the package-to-source provenance backbone and none is planned.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (#5458 KEEP DISCLOSURE entry)"},
		{Kind: "package_registry.source_hint", PayloadSchema: "sdk/go/factschema/schema/package_registry.source_hint.v1.schema.json"},
		{Kind: "package_registry.vulnerability_hint", PayloadSchema: "sdk/go/factschema/schema/package_registry.vulnerability_hint.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Join-key-only evidence: the supply-chain impact query filters on fact_kind but never decodes the payload, and the projector intentionally does not consume it; corroboration is owned by the vulnerability_intelligence family and epic #5462.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (#5458 KEEP DISCLOSURE entry)"},
		{Kind: "package_registry.warning", PayloadSchema: "sdk/go/factschema/schema/package_registry.warning.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in query read model"},
	},
	Producers: []contract.Producer{
		{ID: "collector-package-registry", Repository: "eshu-hq/eshu", Path: "go/internal/collector/packageregistry", PlannedRepository: "eshu-hq/eshu-collector-package-registry", AuthorityMode: "core_issued_grant", Kinds: []string{"package_registry.package", "package_registry.package_artifact", "package_registry.package_dependency", "package_registry.package_version", "package_registry.registry_event", "package_registry.repository_hosting", "package_registry.source_hint", "package_registry.vulnerability_hint", "package_registry.warning"}},
	},
})
