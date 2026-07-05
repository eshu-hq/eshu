// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// The six types across this file and deferred_events.go — SourceHint,
// PackageArtifact, VulnerabilityHint, RegistryEvent, RepositoryHosting, and
// Warning — are TYPED-BUT-NOT-YET-CONSUMED (see doc.go). No decode-seam read
// consumer exists for their fact kinds today, so this wave ships their
// struct, schema, and fixture pack without converting a decode site, adding a
// regression test, or benchmarking a read path (there is none to benchmark).
// The decode-site conversion, the input_invalid regression test, and the
// No-Regression benchmark land in the change that first decodes each kind,
// matching how the terraform_state family typed
// Candidate/ProviderBinding/Warning ahead of their consumer.
//
// SourceHint, VulnerabilityHint, and Warning ARE read today, but only through
// raw payload map access outside this wave's scope: SourceHint by the
// reducer's package_source_correlation domain (a separate reducer family, not
// this projector wave); VulnerabilityHint's package_id and Warning's ecosystem
// and warning_code by raw-SQL-JSONB loaders in go/internal/storage/postgres
// (see doc.go and package_registry_sql_schema_lockstep_test.go). Typing them
// here makes the contract correct and ready the moment a decode-seam consumer
// is added, without asserting a stricter shape than any current reader
// requires.
//
// Their required sets follow the absent-vs-present-empty rule against the
// collector emitter's own fail-closed invariants
// (go/internal/collector/packageregistry/*.go), so the contract is accurate
// today even with no decode-seam consumer.

// SourceHint is the schema-version-1 typed payload for the
// "package_registry.source_hint" fact kind (Contract System v1 §3.1).
//
// A source hint is provenance-only evidence of a package's declared source
// repository, homepage, or SCM URL (go/internal/collector/packageregistry/source_hint.go
// NewSourceHintEnvelope). The emitter fails closed on a blank package identity,
// hint_kind, and the raw-or-normalized URL (at least one of raw_url/
// normalized_url must be non-blank), so package_id and hint_kind are REQUIRED;
// the URL pair stays split as two OPTIONAL fields (RawURL, NormalizedURL)
// rather than a single required "url" field, because the emitter's own
// fallback logic (normalized, falling back to raw) means the payload's
// unconditionally-present key is whichever of the two was supplied, not a
// fixed key name — modeling both as optional and letting a consumer apply the
// same normalized-then-raw fallback preserves that behavior exactly.
type SourceHint struct {
	// PackageID is the package this hint describes. Required — the hint's
	// primary join key back to a package identity.
	PackageID string `json:"package_id"`

	// HintKind classifies the hint (for example "repository", "homepage").
	// Required: the reducer's package_source_correlation domain treats a
	// non-"repository" hint kind as provenance-only and rejects it from
	// ownership correlation, so an absent hint_kind cannot be classified at
	// all.
	HintKind string `json:"hint_kind"`

	// VersionID is the specific version this hint was observed on, when the
	// registry scoped the hint to one version rather than the package as a
	// whole. Optional: blank when the hint is package-level.
	VersionID *string `json:"version_id,omitempty"`

	// Version is the raw version string paired with VersionID. Optional.
	Version *string `json:"version,omitempty"`

	// Ecosystem is the package ecosystem. Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the registry base URL or identifier. Optional.
	Registry *string `json:"registry,omitempty"`

	// RawURL is the hint URL exactly as the registry reported it, before
	// normalization. Optional: the emitter sanitizes it and may emit an empty
	// string when normalized_url alone is present.
	RawURL *string `json:"raw_url,omitempty"`

	// NormalizedURL is the canonicalized hint URL, when the collector could
	// derive one. Optional.
	NormalizedURL *string `json:"normalized_url,omitempty"`

	// ConfidenceReason is a human-readable note on why the hint was classified
	// with its reported confidence. Optional.
	ConfidenceReason *string `json:"confidence_reason,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this hint. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}

// PackageArtifact is the schema-version-1 typed payload for the
// "package_registry.package_artifact" fact kind (Contract System v1 §3.1).
//
// An artifact fact records one published binary/source artifact for a package
// version (go/internal/collector/packageregistry/artifact.go
// NewPackageArtifactEnvelope). The emitter fails closed on a blank
// artifact_key (after deriving package/version identity), so package_id,
// version_id, and artifact_key are REQUIRED: package_id and version_id are the
// emitter's own unconditional identity derivation (packageVersionID), and
// artifact_key is the artifact's own identity within that version.
type PackageArtifact struct {
	// PackageID is the owning package's stable identity key. Required.
	PackageID string `json:"package_id"`

	// VersionID is the owning version's stable identity key. Required.
	VersionID string `json:"version_id"`

	// ArtifactKey is this artifact's identity within its version (for example
	// a filename or classifier+extension key). Required: the emitter rejects a
	// blank artifact_key before building the envelope.
	ArtifactKey string `json:"artifact_key"`

	// Version is the owning version's raw version string. Optional.
	Version *string `json:"version,omitempty"`

	// Ecosystem is the package ecosystem. Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the registry base URL or identifier. Optional.
	Registry *string `json:"registry,omitempty"`

	// ArtifactType classifies the artifact (for example "wheel", "sdist",
	// "jar"). Optional.
	ArtifactType *string `json:"artifact_type,omitempty"`

	// ArtifactURL is the download URL for this artifact. Optional.
	ArtifactURL *string `json:"artifact_url,omitempty"`

	// ArtifactPath is the artifact's path within the registry or repository,
	// when distinct from ArtifactURL. Optional.
	ArtifactPath *string `json:"artifact_path,omitempty"`

	// SizeBytes is the artifact's reported size in bytes. Optional pointer so
	// nil (unreported) stays distinct from an observed 0.
	SizeBytes *int64 `json:"size_bytes,omitempty"`

	// Hashes maps a checksum algorithm name to its hex-encoded digest for this
	// artifact. Optional.
	Hashes map[string]string `json:"hashes,omitempty"`

	// Classifier is the ecosystem-specific artifact classifier (for example a
	// Maven classifier or a Python wheel tag). Optional.
	Classifier *string `json:"classifier,omitempty"`

	// PlatformTags are platform/ABI tags this artifact targets (for example a
	// Python wheel's platform tag). Optional.
	PlatformTags []string `json:"platform_tags,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this artifact. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}

// VulnerabilityHint is the schema-version-1 typed payload for the
// "package_registry.vulnerability_hint" fact kind (Contract System v1 §3.1).
//
// A vulnerability hint is registry-reported advisory metadata for a package or
// version (go/internal/collector/packageregistry/vulnerability_hint.go
// NewVulnerabilityHintEnvelope). The emitter fails closed on a blank
// package identity, advisory_id, and advisory_source, so package_id,
// AdvisoryID, and AdvisorySource are REQUIRED. PackageID's SQL-loader
// significance is documented on doc.go and locked by
// package_registry_sql_schema_lockstep_test.go: the shared supply-chain-impact
// loader (facts_active_supply_chain_impact.go) reads payload->>'package_id'
// against this kind alongside several others.
type VulnerabilityHint struct {
	// PackageID is the package this hint describes. Required: the emitter
	// derives it unconditionally from the observation's package identity, and
	// it is also the field the shared supply-chain-impact SQL loader reads
	// directly (see doc.go).
	PackageID string `json:"package_id"`

	// AdvisoryID is the registry- or source-specific advisory identifier.
	// Required: the emitter rejects a blank advisory_id before building the
	// envelope.
	AdvisoryID string `json:"advisory_id"`

	// AdvisorySource names the advisory's originating source (for example a
	// registry or advisory database name). Required: the emitter rejects a
	// blank advisory_source before building the envelope.
	AdvisorySource string `json:"advisory_source"`

	// VersionID is the specific version this hint applies to, when scoped to
	// one version rather than the package as a whole. Optional.
	VersionID *string `json:"version_id,omitempty"`

	// Version is the raw version string paired with VersionID. Optional.
	Version *string `json:"version,omitempty"`

	// Ecosystem is the package ecosystem. Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the registry base URL or identifier. Optional.
	Registry *string `json:"registry,omitempty"`

	// VulnerabilityID is the cross-referenced vulnerability identifier (for
	// example a CVE or GHSA id), when the advisory maps to one. Optional.
	VulnerabilityID *string `json:"vulnerability_id,omitempty"`

	// SourceSeverity is the advisory source's own severity label or score.
	// Optional.
	SourceSeverity *string `json:"source_severity,omitempty"`

	// AffectedRange is the declared affected version range. Optional.
	AffectedRange *string `json:"affected_range,omitempty"`

	// FixedVersion is the version the advisory reports as fixed, when known.
	// Optional.
	FixedVersion *string `json:"fixed_version,omitempty"`

	// URL is the advisory's reference URL. Optional.
	URL *string `json:"url,omitempty"`

	// Summary is the advisory's human-readable summary. Optional.
	Summary *string `json:"summary,omitempty"`

	// PublishedAt is the advisory's publish timestamp. Optional.
	PublishedAt *string `json:"published_at,omitempty"`

	// ModifiedAt is the advisory's last-modified timestamp. Optional.
	ModifiedAt *string `json:"modified_at,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this hint. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}

// RegistryEvent, RepositoryHosting, and Warning continue in
// deferred_events.go, split purely to keep each file well under the 500-line
// cap.
