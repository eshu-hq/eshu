// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Package is the schema-version-1 typed payload for the
// "package_registry.package" fact kind (Contract System v1 §3.1).
//
// One fact is emitted per package identity observed at a registry or in a
// manifest. The projector materializes a canonical PackageRegistryPackage node
// keyed by PackageID (go/internal/projector/package_registry_canonical.go
// packageRegistryPackageRow), which DROPS a package whose package_id is empty
// rather than fabricating a node. PackageID is therefore the sole REQUIRED
// identity field: an absent package_id must dead-letter as input_invalid, not
// silently produce an empty-identity node or vanish with no operator signal.
//
// The remaining named fields are descriptive metadata the projector copies
// onto the node; each is OPTIONAL because the projector tolerates an empty
// value and the package still materializes on its package_id alone. The
// collector emitter (go/internal/collector/packageregistry/envelope.go
// NewPackageEnvelope) unconditionally writes every field below (each is a
// possibly-empty string derived from NormalizePackageIdentity, never an absent
// key), so the optional typing simply preserves the pre-typing tolerance
// rather than asserting a stricter contract the read path does not require.
type Package struct {
	// PackageID is the stable package identity key (for example
	// "package://npm/registry.npmjs.org/@scope/pkg"). Required — the package
	// node's identity; an absent package_id dead-letters rather than
	// fabricating an empty-identity node.
	PackageID string `json:"package_id"`

	// Ecosystem is the package ecosystem (for example "npm", "pypi", "maven").
	// Optional: copied onto the node when present.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the registry base URL or identifier the package was observed
	// at. Optional.
	Registry *string `json:"registry,omitempty"`

	// RawName is the package name exactly as the registry or manifest reported
	// it. Optional.
	RawName *string `json:"raw_name,omitempty"`

	// NormalizedName is the ecosystem-normalized package name. Optional.
	NormalizedName *string `json:"normalized_name,omitempty"`

	// Namespace is the package's scope or group namespace, when the ecosystem
	// has one (for example an npm scope or a Maven groupId). Optional.
	Namespace *string `json:"namespace,omitempty"`

	// Classifier is the ecosystem-specific package classifier (for example a
	// Maven artifact classifier). Optional.
	Classifier *string `json:"classifier,omitempty"`

	// PURL is the Package URL (purl) identifying the package. Optional.
	PURL *string `json:"purl,omitempty"`

	// BOMRef is the CycloneDX bom-ref this package correlates to, when derived
	// from an SBOM. Optional.
	BOMRef *string `json:"bom_ref,omitempty"`

	// PackageManager is the ecosystem's package manager label (for example
	// "npm", "pip"). Optional.
	PackageManager *string `json:"package_manager,omitempty"`

	// SourcePath is the manifest or lockfile path the package was declared in,
	// when observed from a manifest rather than a live registry query.
	// Optional.
	SourcePath *string `json:"source_path,omitempty"`

	// SourceSpecificID is the registry- or ecosystem-specific identifier for
	// this package, distinct from the normalized PackageID. Optional.
	SourceSpecificID *string `json:"source_specific_id,omitempty"`

	// Visibility is the package's declared visibility (for example "public",
	// "private", "unknown"). Optional.
	Visibility *string `json:"visibility,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this package, for operational correlation. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings (package id, purl,
	// bom-ref) the collector publishes for cross-fact correlation. Optional: a
	// package with no derivable anchors omits the key.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
