// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// This file is the facts root's transitional compatibility surface for the
// supply-chain fact families, which moved to [chain] in issue #6776 so the
// root directory drops back under the 40-file dirgate cap. Every entry is an
// alias or a thin forwarder with no behavior change: the value, the type
// identity, and the returned bytes are the same ones the root declared
// before the move.
//
// It carries only the names that still have a caller -- the facts root's own
// schemaVersionFamilies registry wiring and registry tests plus the
// collectors, reducers, projectors, and query surfaces that reach them as
// facts.X today. A later family move adds a stanza to this file and never
// creates a new compat_*.go, and each entry here is deleted once its last
// caller has moved to the chain package directly (see the importer-migration
// follow-up, #6950).

import "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"

// Stanza: oci_registry.go (moved to chain/oci_registry.go).
const (
	// OCIImageDescriptorFactKind identifies one reusable OCI descriptor. See
	// [chain.OCIImageDescriptorFactKind].
	OCIImageDescriptorFactKind = chain.OCIImageDescriptorFactKind
	// OCIImageDescriptorSchemaVersion is the first descriptor fact schema. See
	// [chain.OCIImageDescriptorSchemaVersion].
	OCIImageDescriptorSchemaVersion = chain.OCIImageDescriptorSchemaVersion
	// OCIImageIndexFactKind identifies one digest-addressed image index. See
	// [chain.OCIImageIndexFactKind].
	OCIImageIndexFactKind = chain.OCIImageIndexFactKind
	// OCIImageIndexSchemaVersion is the first image-index fact schema. See
	// [chain.OCIImageIndexSchemaVersion].
	OCIImageIndexSchemaVersion = chain.OCIImageIndexSchemaVersion
	// OCIImageManifestFactKind identifies one digest-addressed image manifest.
	// See [chain.OCIImageManifestFactKind].
	OCIImageManifestFactKind = chain.OCIImageManifestFactKind
	// OCIImageManifestSchemaVersion is the first manifest fact schema. See
	// [chain.OCIImageManifestSchemaVersion].
	OCIImageManifestSchemaVersion = chain.OCIImageManifestSchemaVersion
	// OCIImageReferrerFactKind identifies one descriptor reported as a referrer.
	// See [chain.OCIImageReferrerFactKind].
	OCIImageReferrerFactKind = chain.OCIImageReferrerFactKind
	// OCIImageReferrerSchemaVersion is the first referrer fact schema. See
	// [chain.OCIImageReferrerSchemaVersion].
	OCIImageReferrerSchemaVersion = chain.OCIImageReferrerSchemaVersion
	// OCIImageTagObservationFactKind identifies one mutable tag-to-digest
	// observation. See [chain.OCIImageTagObservationFactKind].
	OCIImageTagObservationFactKind = chain.OCIImageTagObservationFactKind
	// OCIImageTagObservationSchemaVersion is the first tag fact schema. See
	// [chain.OCIImageTagObservationSchemaVersion].
	OCIImageTagObservationSchemaVersion = chain.OCIImageTagObservationSchemaVersion
	// OCIRegistryRepositoryFactKind identifies one OCI registry repository
	// configured for observation. See [chain.OCIRegistryRepositoryFactKind].
	OCIRegistryRepositoryFactKind = chain.OCIRegistryRepositoryFactKind
	// OCIRegistryRepositorySchemaVersion is the first repository fact schema. See
	// [chain.OCIRegistryRepositorySchemaVersion].
	OCIRegistryRepositorySchemaVersion = chain.OCIRegistryRepositorySchemaVersion
	// OCIRegistryWarningFactKind identifies one non-fatal OCI registry warning.
	// See [chain.OCIRegistryWarningFactKind].
	OCIRegistryWarningFactKind = chain.OCIRegistryWarningFactKind
	// OCIRegistryWarningSchemaVersion is the first warning fact schema. See
	// [chain.OCIRegistryWarningSchemaVersion].
	OCIRegistryWarningSchemaVersion = chain.OCIRegistryWarningSchemaVersion
)

// OCIRegistryFactKinds returns the accepted OCI registry fact kinds in their
// emission order. See [chain.OCIRegistryFactKinds].
func OCIRegistryFactKinds() []string {
	return chain.OCIRegistryFactKinds()
}

// OCIRegistrySchemaVersion returns the schema version for an OCI registry fact
// kind. See [chain.OCIRegistrySchemaVersion].
func OCIRegistrySchemaVersion(factKind string) (string, bool) {
	return chain.OCIRegistrySchemaVersion(factKind)
}

// Stanza: package_registry.go (moved to chain/package_registry.go).
const (
	// PackageRegistryPackageArtifactFactKind identifies one package artifact
	// file, digest, classifier, or platform coordinate. See
	// [chain.PackageRegistryPackageArtifactFactKind].
	PackageRegistryPackageArtifactFactKind = chain.PackageRegistryPackageArtifactFactKind
	// PackageRegistryPackageArtifactSchemaVersion is the first artifact fact
	// schema. See [chain.PackageRegistryPackageArtifactSchemaVersion].
	PackageRegistryPackageArtifactSchemaVersion = chain.PackageRegistryPackageArtifactSchemaVersion
	// PackageRegistryPackageDependencyFactKind identifies one package-version
	// dependency edge reported by package-native metadata. See
	// [chain.PackageRegistryPackageDependencyFactKind].
	PackageRegistryPackageDependencyFactKind = chain.PackageRegistryPackageDependencyFactKind
	// PackageRegistryPackageDependencySchemaVersion is the first dependency fact
	// schema. See [chain.PackageRegistryPackageDependencySchemaVersion].
	PackageRegistryPackageDependencySchemaVersion = chain.PackageRegistryPackageDependencySchemaVersion
	// PackageRegistryPackageFactKind identifies one package identity observed in
	// a package registry or feed. See [chain.PackageRegistryPackageFactKind].
	PackageRegistryPackageFactKind = chain.PackageRegistryPackageFactKind
	// PackageRegistryPackageSchemaVersion is the first package fact schema. See
	// [chain.PackageRegistryPackageSchemaVersion].
	PackageRegistryPackageSchemaVersion = chain.PackageRegistryPackageSchemaVersion
	// PackageRegistryPackageVersionFactKind identifies one package version
	// observed in a package registry or feed. See
	// [chain.PackageRegistryPackageVersionFactKind].
	PackageRegistryPackageVersionFactKind = chain.PackageRegistryPackageVersionFactKind
	// PackageRegistryPackageVersionSchemaVersion is the first version fact
	// schema. See [chain.PackageRegistryPackageVersionSchemaVersion].
	PackageRegistryPackageVersionSchemaVersion = chain.PackageRegistryPackageVersionSchemaVersion
	// PackageRegistryRegistryEventFactKind identifies a publish, delete, unlist,
	// deprecate, yank, relist, or metadata-mutation event. See
	// [chain.PackageRegistryRegistryEventFactKind].
	PackageRegistryRegistryEventFactKind = chain.PackageRegistryRegistryEventFactKind
	// PackageRegistryRegistryEventSchemaVersion is the first registry event fact
	// schema. See [chain.PackageRegistryRegistryEventSchemaVersion].
	PackageRegistryRegistryEventSchemaVersion = chain.PackageRegistryRegistryEventSchemaVersion
	// PackageRegistryRepositoryHostingFactKind identifies provider repository
	// topology such as Artifactory local, remote, or virtual feeds. See
	// [chain.PackageRegistryRepositoryHostingFactKind].
	PackageRegistryRepositoryHostingFactKind = chain.PackageRegistryRepositoryHostingFactKind
	// PackageRegistryRepositoryHostingSchemaVersion is the first repository
	// hosting fact schema. See
	// [chain.PackageRegistryRepositoryHostingSchemaVersion].
	PackageRegistryRepositoryHostingSchemaVersion = chain.PackageRegistryRepositoryHostingSchemaVersion
	// PackageRegistrySourceHintFactKind identifies source repository, homepage,
	// SCM, or provenance hints reported by package metadata. See
	// [chain.PackageRegistrySourceHintFactKind].
	PackageRegistrySourceHintFactKind = chain.PackageRegistrySourceHintFactKind
	// PackageRegistrySourceHintSchemaVersion is the first source hint fact
	// schema. See [chain.PackageRegistrySourceHintSchemaVersion].
	PackageRegistrySourceHintSchemaVersion = chain.PackageRegistrySourceHintSchemaVersion
	// PackageRegistryVulnerabilityHintFactKind identifies advisory metadata
	// reported directly by a registry without assigning severity policy. See
	// [chain.PackageRegistryVulnerabilityHintFactKind].
	PackageRegistryVulnerabilityHintFactKind = chain.PackageRegistryVulnerabilityHintFactKind
	// PackageRegistryVulnerabilityHintSchemaVersion is the first vulnerability
	// hint fact schema. See
	// [chain.PackageRegistryVulnerabilityHintSchemaVersion].
	PackageRegistryVulnerabilityHintSchemaVersion = chain.PackageRegistryVulnerabilityHintSchemaVersion
	// PackageRegistryWarningFactKind identifies non-fatal package-registry
	// collection warnings. See [chain.PackageRegistryWarningFactKind].
	PackageRegistryWarningFactKind = chain.PackageRegistryWarningFactKind
	// PackageRegistryWarningSchemaVersion is the first warning fact schema. See
	// [chain.PackageRegistryWarningSchemaVersion].
	PackageRegistryWarningSchemaVersion = chain.PackageRegistryWarningSchemaVersion
)

// PackageRegistryFactKinds returns the accepted package-registry fact kinds in
// their emission order. See [chain.PackageRegistryFactKinds].
func PackageRegistryFactKinds() []string {
	return chain.PackageRegistryFactKinds()
}

// PackageRegistrySchemaVersion returns the schema version for a package-
// registry fact kind. See [chain.PackageRegistrySchemaVersion].
func PackageRegistrySchemaVersion(factKind string) (string, bool) {
	return chain.PackageRegistrySchemaVersion(factKind)
}

// Stanza: sbom_attestation.go (moved to chain/sbom_attestation.go).
const (
	// AttestationSLSAProvenanceFactKind identifies one SLSA provenance predicate.
	// See [chain.AttestationSLSAProvenanceFactKind].
	AttestationSLSAProvenanceFactKind = chain.AttestationSLSAProvenanceFactKind
	// AttestationSignatureVerificationFactKind identifies one signature
	// verification result. See [chain.AttestationSignatureVerificationFactKind].
	AttestationSignatureVerificationFactKind = chain.AttestationSignatureVerificationFactKind
	// AttestationStatementFactKind identifies one in-toto statement envelope. See
	// [chain.AttestationStatementFactKind].
	AttestationStatementFactKind = chain.AttestationStatementFactKind
	// SBOMAttestationSchemaVersionV1 is the SBOM/attestation fact schema version.
	// Bumped 1.0.0 -> 1.1.0 (#5456) for the additive-optional
	// materials/config_source fields added to attestation.slsa_provenance
	// (sdk/go/factschema/sbom/v1.SLSAProvenance); no existing field, kind, or
	// required set changed, so every other kind in this shared family stays
	// backward compatible under the bump. See
	// [chain.SBOMAttestationSchemaVersionV1].
	SBOMAttestationSchemaVersionV1 = chain.SBOMAttestationSchemaVersionV1
	// SBOMComponentFactKind identifies one component from an SBOM document. See
	// [chain.SBOMComponentFactKind].
	SBOMComponentFactKind = chain.SBOMComponentFactKind
	// SBOMDependencyRelationshipFactKind identifies one SBOM dependency edge. See
	// [chain.SBOMDependencyRelationshipFactKind].
	SBOMDependencyRelationshipFactKind = chain.SBOMDependencyRelationshipFactKind
	// SBOMDocumentFactKind identifies one parsed or attempted SBOM document. See
	// [chain.SBOMDocumentFactKind].
	SBOMDocumentFactKind = chain.SBOMDocumentFactKind
	// SBOMExternalReferenceFactKind identifies one SBOM external reference. See
	// [chain.SBOMExternalReferenceFactKind].
	SBOMExternalReferenceFactKind = chain.SBOMExternalReferenceFactKind
	// SBOMWarningFactKind identifies non-fatal SBOM or attestation warnings. See
	// [chain.SBOMWarningFactKind].
	SBOMWarningFactKind = chain.SBOMWarningFactKind
)

// SBOMAttestationFactKinds returns the accepted SBOM and attestation fact
// kinds in their source contract order. See [chain.SBOMAttestationFactKinds].
func SBOMAttestationFactKinds() []string {
	return chain.SBOMAttestationFactKinds()
}

// SBOMAttestationSchemaVersion returns the schema version for an SBOM or
// attestation fact kind. See [chain.SBOMAttestationSchemaVersion].
func SBOMAttestationSchemaVersion(factKind string) (string, bool) {
	return chain.SBOMAttestationSchemaVersion(factKind)
}

// Stanza: vulnerability_intelligence.go (moved to chain/vulnerability_intelligence.go).
const (
	// VulnerabilityAffectedPackageFactKind identifies package-native advisory
	// applicability evidence. See [chain.VulnerabilityAffectedPackageFactKind].
	VulnerabilityAffectedPackageFactKind = chain.VulnerabilityAffectedPackageFactKind
	// VulnerabilityAffectedProductFactKind identifies source-reported product or
	// CPE applicability evidence. See
	// [chain.VulnerabilityAffectedProductFactKind].
	VulnerabilityAffectedProductFactKind = chain.VulnerabilityAffectedProductFactKind
	// VulnerabilityCVEFactKind identifies one CVE or advisory identity record.
	// See [chain.VulnerabilityCVEFactKind].
	VulnerabilityCVEFactKind = chain.VulnerabilityCVEFactKind
	// VulnerabilityEPSSScoreFactKind identifies one EPSS score observation. See
	// [chain.VulnerabilityEPSSScoreFactKind].
	VulnerabilityEPSSScoreFactKind = chain.VulnerabilityEPSSScoreFactKind
	// VulnerabilityGoCallReachabilityFactKind identifies one govulncheck-compat
	// reachability observation for a Go advisory. The payload preserves the
	// govulncheck JSON evidence (osv id, module, package, symbol, call stack,
	// reachable flag) so reducers can classify call-graph reachability without
	// re-running govulncheck. See
	// [chain.VulnerabilityGoCallReachabilityFactKind].
	VulnerabilityGoCallReachabilityFactKind = chain.VulnerabilityGoCallReachabilityFactKind
	// VulnerabilityGoModuleEvidenceFactKind identifies one Go-module requirement
	// observed in a repository's go.mod (with optional go.sum hash anchor) so
	// reducers can correlate Go vulnerability advisories to a repo-anchored
	// module path, required version, replacement target, and indirect flag. See
	// [chain.VulnerabilityGoModuleEvidenceFactKind].
	VulnerabilityGoModuleEvidenceFactKind = chain.VulnerabilityGoModuleEvidenceFactKind
	// VulnerabilityIntelligenceSchemaVersionV1 is the first vulnerability
	// intelligence fact schema. See
	// [chain.VulnerabilityIntelligenceSchemaVersionV1].
	VulnerabilityIntelligenceSchemaVersionV1 = chain.VulnerabilityIntelligenceSchemaVersionV1
	// VulnerabilityKnownExploitedFactKind identifies one CISA KEV observation.
	// See [chain.VulnerabilityKnownExploitedFactKind].
	VulnerabilityKnownExploitedFactKind = chain.VulnerabilityKnownExploitedFactKind
	// VulnerabilityOSPackageFactKind identifies one installed OS package
	// observation. Carries distro, package manager, epoch/version/release, arch,
	// source package, repository class, and vendor advisory source so reducers
	// can match vendor advisories against installed evidence without promoting
	// upstream fixed-version strings to backported builds. See
	// [chain.VulnerabilityOSPackageFactKind].
	VulnerabilityOSPackageFactKind = chain.VulnerabilityOSPackageFactKind
	// VulnerabilityReferenceFactKind identifies one source advisory reference.
	// See [chain.VulnerabilityReferenceFactKind].
	VulnerabilityReferenceFactKind = chain.VulnerabilityReferenceFactKind
	// VulnerabilitySourceSnapshotFactKind identifies one vulnerability source
	// observation boundary and freshness checkpoint. See
	// [chain.VulnerabilitySourceSnapshotFactKind].
	VulnerabilitySourceSnapshotFactKind = chain.VulnerabilitySourceSnapshotFactKind
	// VulnerabilityWarningFactKind identifies non-fatal vulnerability source
	// collection or normalization warnings. See
	// [chain.VulnerabilityWarningFactKind].
	VulnerabilityWarningFactKind = chain.VulnerabilityWarningFactKind
)

// VulnerabilityIntelligenceFactKinds returns the accepted vulnerability
// intelligence fact kinds in source-contract order. See
// [chain.VulnerabilityIntelligenceFactKinds].
func VulnerabilityIntelligenceFactKinds() []string {
	return chain.VulnerabilityIntelligenceFactKinds()
}

// VulnerabilityIntelligenceSchemaVersion returns the schema version for a
// vulnerability intelligence fact kind. See
// [chain.VulnerabilityIntelligenceSchemaVersion].
func VulnerabilityIntelligenceSchemaVersion(factKind string) (string, bool) {
	return chain.VulnerabilityIntelligenceSchemaVersion(factKind)
}

// Stanza: vulnerability_suppression.go (moved to chain/vulnerability_suppression.go).
const (
	// VulnerabilitySuppressionFactKind identifies one VEX or operator-authored
	// suppression assertion about a vulnerability finding. Suppression facts are
	// first-class evidence: they capture who asserted the suppression, the
	// justification, the scope, the evidence reference, and an optional
	// expiration. The reducer applies them only when scope matches and expiration
	// has not passed; the API surfaces the suppression rationale even when the
	// finding is hidden from the default view. See
	// [chain.VulnerabilitySuppressionFactKind].
	VulnerabilitySuppressionFactKind = chain.VulnerabilitySuppressionFactKind
	// VulnerabilitySuppressionJustificationAcceptedRisk marks operator-accepted
	// residual risk (deployment context, compensating control, etc.). See
	// [chain.VulnerabilitySuppressionJustificationAcceptedRisk].
	VulnerabilitySuppressionJustificationAcceptedRisk = chain.VulnerabilitySuppressionJustificationAcceptedRisk
	// VulnerabilitySuppressionJustificationFalsePositive marks an operator
	// assertion that the finding is a false positive. See
	// [chain.VulnerabilitySuppressionJustificationFalsePositive].
	VulnerabilitySuppressionJustificationFalsePositive = chain.VulnerabilitySuppressionJustificationFalsePositive
	// VulnerabilitySuppressionJustificationIgnored marks a temporary ignore;
	// callers SHOULD set expires_at. See
	// [chain.VulnerabilitySuppressionJustificationIgnored].
	VulnerabilitySuppressionJustificationIgnored = chain.VulnerabilitySuppressionJustificationIgnored
	// VulnerabilitySuppressionJustificationNotAffected matches the VEX
	// "not_affected" status (component is present but exploitation conditions do
	// not apply). See [chain.VulnerabilitySuppressionJustificationNotAffected].
	VulnerabilitySuppressionJustificationNotAffected = chain.VulnerabilitySuppressionJustificationNotAffected
	// VulnerabilitySuppressionJustificationProviderDismissed reflects a provider-
	// side dismissal copied into Eshu as evidence. See
	// [chain.VulnerabilitySuppressionJustificationProviderDismissed].
	VulnerabilitySuppressionJustificationProviderDismissed = chain.VulnerabilitySuppressionJustificationProviderDismissed
	// VulnerabilitySuppressionSchemaVersionV1 is the latest backward-compatible
	// schema version in major 1. Version 1.1 adds optional deployment-context
	// narrowing fields to suppression scopes. See
	// [chain.VulnerabilitySuppressionSchemaVersionV1].
	VulnerabilitySuppressionSchemaVersionV1 = chain.VulnerabilitySuppressionSchemaVersionV1
	// VulnerabilitySuppressionSourcePolicy marks the assertion as operator policy
	// authored inside Eshu (CLI, MCP, API, or stored configuration). See
	// [chain.VulnerabilitySuppressionSourcePolicy].
	VulnerabilitySuppressionSourcePolicy = chain.VulnerabilitySuppressionSourcePolicy
	// VulnerabilitySuppressionSourceProviderDismissal marks the assertion as a
	// pointer to a provider-side dismissal (for example GitHub Dependabot).
	// Provider dismissals are evidence, not automatic Eshu suppressions: the
	// reducer surfaces them as a distinct state so callers can audit them. See
	// [chain.VulnerabilitySuppressionSourceProviderDismissal].
	VulnerabilitySuppressionSourceProviderDismissal = chain.VulnerabilitySuppressionSourceProviderDismissal
	// VulnerabilitySuppressionSourceVEX marks the assertion as derived from a VEX
	// statement (OpenVEX, CSAF VEX, CycloneDX VEX, etc.). See
	// [chain.VulnerabilitySuppressionSourceVEX].
	VulnerabilitySuppressionSourceVEX = chain.VulnerabilitySuppressionSourceVEX
)

// VulnerabilitySuppressionFactKinds returns the accepted vulnerability
// suppression fact kinds in source-contract order. See
// [chain.VulnerabilitySuppressionFactKinds].
func VulnerabilitySuppressionFactKinds() []string {
	return chain.VulnerabilitySuppressionFactKinds()
}

// VulnerabilitySuppressionSchemaVersion returns the schema version for a
// vulnerability suppression fact kind. See
// [chain.VulnerabilitySuppressionSchemaVersion].
func VulnerabilitySuppressionSchemaVersion(factKind string) (string, bool) {
	return chain.VulnerabilitySuppressionSchemaVersion(factKind)
}
