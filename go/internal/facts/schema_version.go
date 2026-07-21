// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

// schemaSemverPattern matches the MAJOR.MINOR.PATCH form every core fact family
// registers as its supported schema version.
var schemaSemverPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// IsCanonicalSchemaVersion reports whether version is a canonical fact schema
// version in the exact MAJOR.MINOR.PATCH form the registry stores: bare digits,
// no leading "v", no pre-release or build metadata, and no surrounding
// whitespace. It is the single source of truth for "is this a well-formed
// schema-version string" and is shared by the runtime classifier and the
// fact-kind registry generator, which uses it to validate the registry v1.1
// deprecated_in/removed_in markers so a typo like "next" or "2" cannot reach
// the generated source-of-truth artifact. It does not compare the version
// against any supported version; use ClassifySchemaVersion for compatibility.
func IsCanonicalSchemaVersion(version string) bool {
	if !schemaSemverPattern.MatchString(version) {
		return false
	}
	// Defense in depth: the pattern already rejects pre-release/build/"v"
	// forms, but confirm the value is also a valid semver core so the two
	// definitions cannot drift.
	return semver.IsValid("v" + version)
}

// Compatibility classifies a candidate fact schema version against the version
// a core consumer currently supports for a fact kind. It implements the
// documented compatibility contract: unsupported majors are rejected with no
// silent fallback, and a minor or patch ahead of the supported version is not
// yet authoritative.
type Compatibility string

const (
	// CompatibilitySupported means the candidate version is the supported
	// version or an older same-major version a backward-compatible reader
	// accepts.
	CompatibilitySupported Compatibility = "supported"
	// CompatibilityUnsupportedMajor means the candidate version crosses a major
	// boundary, or cannot be parsed as a semantic version, and must be rejected.
	// Breaking changes require an explicit migration, not a silent fallback.
	CompatibilityUnsupportedMajor Compatibility = "unsupported_major"
	// CompatibilityUnsupportedMinor means the candidate shares the supported
	// major but is ahead by minor or patch, so the core consumer has not yet
	// declared support and must not treat it as authoritative.
	CompatibilityUnsupportedMinor Compatibility = "unsupported_minor"
	// CompatibilityUnknownKind means the fact kind is not a core-owned kind with
	// a registered schema version, so core does not own its compatibility.
	CompatibilityUnknownKind Compatibility = "unknown_kind"
)

// schemaVersionFamily pairs a fact family's kind enumeration with its supported
// schema-version lookup so the central registry stays consistent with each
// owning family.
type schemaVersionFamily struct {
	kinds   func() []string
	version func(string) (string, bool)
}

// schemaVersionFamilies is the authoritative list of core fact families that
// declare a schema version. A new versioned family must be added here so the
// central registry, classifier, and drift guard see it.
var schemaVersionFamilies = []schemaVersionFamily{
	{DocumentationFactKinds, DocumentationSchemaVersion},
	{AWSFactKinds, AWSSchemaVersion},
	{AzureFactKinds, AzureSchemaVersion},
	{CICDRunFactKinds, CICDRunSchemaVersion},
	{CodeownersFactKinds, CodeownersSchemaVersion},
	{EC2InstancePostureFactKinds, EC2InstancePostureSchemaVersion},
	{GCPFactKinds, GCPSchemaVersion},
	{IncidentContextFactKinds, IncidentContextSchemaVersion},
	{IncidentRoutingFactKinds, IncidentRoutingSchemaVersion},
	{KubernetesLiveFactKinds, KubernetesLiveSchemaVersion},
	{ObservabilityFactKinds, ObservabilitySchemaVersion},
	{OCIRegistryFactKinds, OCIRegistrySchemaVersion},
	{PackageRegistryFactKinds, PackageRegistrySchemaVersion},
	{RDSPostureFactKinds, RDSPostureSchemaVersion},
	{ReducerDerivedFactKinds, ReducerDerivedSchemaVersion},
	{S3BucketPostureFactKinds, S3BucketPostureSchemaVersion},
	{S3ExternalPrincipalGrantFactKinds, S3ExternalPrincipalGrantSchemaVersion},
	{SBOMAttestationFactKinds, SBOMAttestationSchemaVersion},
	{ScannerWorkerFactKinds, ScannerWorkerSchemaVersion},
	{SecretsIAMFactKinds, SecretsIAMSchemaVersion},
	{SecurityAlertFactKinds, SecurityAlertSchemaVersion},
	{SemanticFactKinds, SemanticSchemaVersion},
	{ServiceCatalogFactKinds, ServiceCatalogSchemaVersion},
	{TerraformStateFactKinds, TerraformStateSchemaVersion},
	{VulnerabilityIntelligenceFactKinds, VulnerabilityIntelligenceSchemaVersion},
	{VulnerabilitySuppressionFactKinds, VulnerabilitySuppressionSchemaVersion},
	{WorkItemFactKinds, WorkItemSchemaVersion},
}

// supportedSchemaVersionRegistry is the precomputed core fact-kind to supported
// schema-version map. It is built once at init from schemaVersionFamilies so the
// per-fact lookups SchemaVersion and ValidateSchemaVersion use on the projection
// hot path are O(1) rather than a per-call scan over every family.
var supportedSchemaVersionRegistry = buildFactKindSchemaRegistry(factKindRegistryEntries)

// liveSchemaVersionRegistry flattens every versioned family into one
// map. Two families must never claim the same fact kind; that is a programming
// error in schemaVersionFamilies, so it panics at init and surfaces in any test.
func liveSchemaVersionRegistry() map[string]string {
	registry := make(map[string]string)
	for _, family := range schemaVersionFamilies {
		for _, kind := range family.kinds() {
			version, ok := family.version(kind)
			if !ok {
				continue
			}
			if existing, dup := registry[kind]; dup {
				panic(fmt.Sprintf("facts: duplicate schema version registration for kind %q: %q and %q", kind, existing, version))
			}
			registry[kind] = version
		}
	}
	return registry
}

// SchemaVersion returns the schema version a core consumer currently supports
// for factKind via an O(1) lookup against the precomputed registry. The boolean
// is false for any fact kind core does not own a versioned schema for, including
// out-of-tree component fact kinds.
func SchemaVersion(factKind string) (string, bool) {
	version, ok := supportedSchemaVersionRegistry[strings.TrimSpace(factKind)]
	return version, ok
}

// SupportedSchemaVersions returns a copy of the core fact-kind to supported
// schema-version registry. Diagnostics and compatibility surfaces read this to
// report the versions a reducer or query consumer accepts.
func SupportedSchemaVersions() map[string]string {
	registry := make(map[string]string, len(supportedSchemaVersionRegistry))
	for kind, version := range supportedSchemaVersionRegistry {
		registry[kind] = version
	}
	return registry
}

// ClassifySchemaVersion reports how a candidate schema version for factKind
// relates to the version core currently supports. It is deterministic and
// pure, so reducers, projectors, the component activation path, and API/MCP
// diagnostics classify a collector version the same way.
func ClassifySchemaVersion(factKind, candidate string) Compatibility {
	supported, ok := SchemaVersion(factKind)
	if !ok {
		return CompatibilityUnknownKind
	}
	// Fast path for the overwhelmingly common case: a collector emits exactly the
	// supported version. This keeps per-fact admission allocation-free and avoids
	// the semver parse on the projection hot path.
	if strings.TrimSpace(candidate) == supported {
		return CompatibilitySupported
	}
	if !schemaSemverPattern.MatchString(strings.TrimSpace(candidate)) {
		// A blank or non-canonical version (not MAJOR.MINOR.PATCH) cannot be
		// proven same-major compatible, so it fails closed as a breaking change.
		return CompatibilityUnsupportedMajor
	}
	wantSemver := normalizeFactSemver(supported)
	gotSemver := normalizeFactSemver(candidate)
	if semver.Major(gotSemver) != semver.Major(wantSemver) {
		return CompatibilityUnsupportedMajor
	}
	if semver.Compare(gotSemver, wantSemver) > 0 {
		return CompatibilityUnsupportedMinor
	}
	return CompatibilitySupported
}

// ValidateSchemaVersion returns a non-nil error when a core-owned fact kind
// carries an unsupported schema version. It returns nil for supported versions
// and for fact kinds core does not own, so callers can apply it uniformly
// without falsely rejecting out-of-tree component facts.
func ValidateSchemaVersion(factKind, candidate string) error {
	switch ClassifySchemaVersion(factKind, candidate) {
	case CompatibilitySupported, CompatibilityUnknownKind:
		return nil
	default:
		supported, _ := SchemaVersion(factKind)
		return fmt.Errorf(
			"fact kind %q schema_version %q is unsupported; core supports %q",
			factKind,
			strings.TrimSpace(candidate),
			supported,
		)
	}
}

// normalizeFactSemver prepares a registered or candidate version for semver
// comparison by trimming and prefixing the "v" the semver package expects.
func normalizeFactSemver(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	return "v" + trimmed
}
