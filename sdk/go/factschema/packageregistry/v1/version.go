// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// PackageVersion is the schema-version-1 typed payload for the
// "package_registry.package_version" fact kind (Contract System v1 §3.1).
//
// One fact is emitted per package version observed at a registry. The
// projector materializes a canonical PackageRegistryVersion node keyed by
// VersionID, joined to its owning package by PackageID
// (go/internal/projector/package_registry_canonical.go
// packageRegistryVersionRow), which DROPS a version observation missing
// package_id, version_id, or version rather than fabricating a node under a
// broken join. Those three fields are therefore REQUIRED. The collector
// emitter (go/internal/collector/packageregistry/version.go
// NewPackageVersionEnvelope) fails closed on a blank version before building
// the envelope and always derives version_id and package_id from the same
// normalized identity, so an absent value is not a shape the emitter produces;
// an absent key still dead-letters as input_invalid rather than trusting that
// invariant blindly.
type PackageVersion struct {
	// PackageID is the owning package's stable identity key. Required — the
	// version node's join key to its package; an absent package_id
	// dead-letters rather than fabricating a broken join.
	PackageID string `json:"package_id"`

	// VersionID is the stable version identity key (PackageID + "@" +
	// Version). Required — the version node's own identity.
	VersionID string `json:"version_id"`

	// Version is the raw version string (for example "1.2.3"). Required: the
	// collector emitter itself rejects a blank version before building the
	// envelope, so an absent value here is a decode-time defense of that same
	// invariant.
	Version string `json:"version"`

	// Ecosystem is the package ecosystem. Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the registry base URL or identifier. Optional.
	Registry *string `json:"registry,omitempty"`

	// PURL is the Package URL (purl) identifying this version. Optional.
	PURL *string `json:"purl,omitempty"`

	// BOMRef is the CycloneDX bom-ref this version correlates to. Optional.
	BOMRef *string `json:"bom_ref,omitempty"`

	// PackageManager is the ecosystem's package manager label. Optional.
	PackageManager *string `json:"package_manager,omitempty"`

	// PublishedAt is the RFC 3339 publish timestamp the registry reported.
	// Optional: the collector emitter omits the key entirely when it has no
	// publish time (observation.PublishedAt.IsZero()), so an absent key is a
	// normal, non-erroneous shape.
	PublishedAt *string `json:"published_at,omitempty"`

	// IsYanked reports whether the registry marked this version as yanked or
	// withdrawn. Optional descriptive flag, NOT an identity key: a pointer so
	// nil (unreported) stays distinct from an observed false, matching the
	// ociregistry Mutated / terraformstate Sensitive *bool convention. The
	// collector's live emitter writes this key today, but the projector
	// re-decodes STORED facts on every re-projection (runtime.go), so a
	// persisted, older, or out-of-tree fact that omits it must still decode and
	// project the version node (nil derefs to false in the row builder) rather
	// than quarantine the whole node on a missing descriptive flag. See
	// IsUnlisted, IsDeprecated, IsRetracted for the same convention.
	IsYanked *bool `json:"is_yanked,omitempty"`

	// IsUnlisted reports whether the registry marked this version unlisted.
	// Optional — see IsYanked's doc comment for the descriptive-flag rationale
	// shared by all four boolean flags.
	IsUnlisted *bool `json:"is_unlisted,omitempty"`

	// IsDeprecated reports whether the registry marked this version
	// deprecated. Optional — see IsYanked's doc comment.
	IsDeprecated *bool `json:"is_deprecated,omitempty"`

	// IsRetracted reports whether the registry marked this version retracted.
	// Optional — see IsYanked's doc comment.
	IsRetracted *bool `json:"is_retracted,omitempty"`

	// ArtifactURLs are download URLs for this version's published artifacts.
	// Optional: the collector emitter clones observation.ArtifactURLs through
	// cloneStrings, which returns nil for an empty input, so an absent key is a
	// normal "no artifacts reported" shape.
	ArtifactURLs []string `json:"artifact_urls,omitempty"`

	// Checksums maps a checksum algorithm name (for example "sha512") to its
	// hex-encoded digest for this version's primary artifact. Optional: the
	// collector emitter clones observation.Checksums through cloneStringMap,
	// which returns nil for an empty input.
	Checksums map[string]string `json:"checksums,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this version. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
