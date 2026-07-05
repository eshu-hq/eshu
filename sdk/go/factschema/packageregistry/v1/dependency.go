// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// PackageDependency is the schema-version-1 typed payload for the
// "package_registry.package_dependency" fact kind (Contract System v1 §3.1).
//
// One fact is emitted per package-native dependency edge (a version declaring
// a dependency on another package). The projector materializes a canonical
// PackageRegistryDependency edge row keyed by the fact's own StableFactKey,
// joined by PackageID, VersionID, and DependencyPackageID
// (go/internal/projector/package_registry_canonical.go
// packageRegistryDependencyRow), which DROPS a dependency observation missing
// any of those three join keys — or carrying a blank StableFactKey — rather
// than fabricating an edge under a broken join. The three payload fields are
// therefore REQUIRED (the StableFactKey requirement is enforced on the
// envelope, not the payload, so it is not a struct field here).
type PackageDependency struct {
	// PackageID is the declaring package's stable identity key. Required — the
	// first half of the dependency edge's source join key; an absent
	// package_id dead-letters rather than fabricating a broken join.
	PackageID string `json:"package_id"`

	// VersionID is the declaring version's stable identity key. Required — the
	// edge's actual source node join key (versions, not packages, declare
	// dependencies).
	VersionID string `json:"version_id"`

	// DependencyPackageID is the depended-upon package's stable identity key.
	// Required — the edge's target join key.
	DependencyPackageID string `json:"dependency_package_id"`

	// Version is the declaring version's raw version string. Optional:
	// descriptive, copied onto the row when present.
	Version *string `json:"version,omitempty"`

	// Ecosystem is the declaring package's ecosystem. Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the declaring package's registry. Optional.
	Registry *string `json:"registry,omitempty"`

	// DependencyEcosystem is the depended-upon package's ecosystem. Optional.
	DependencyEcosystem *string `json:"dependency_ecosystem,omitempty"`

	// DependencyRegistry is the depended-upon package's registry. Optional.
	DependencyRegistry *string `json:"dependency_registry,omitempty"`

	// DependencyNamespace is the depended-upon package's scope or group
	// namespace. Optional.
	DependencyNamespace *string `json:"dependency_namespace,omitempty"`

	// DependencyNormalized is the depended-upon package's normalized name.
	// Optional.
	DependencyNormalized *string `json:"dependency_normalized,omitempty"`

	// DependencyPURL is the depended-upon package's Package URL. Optional.
	DependencyPURL *string `json:"dependency_purl,omitempty"`

	// DependencyBOMRef is the depended-upon package's CycloneDX bom-ref.
	// Optional.
	DependencyBOMRef *string `json:"dependency_bom_ref,omitempty"`

	// DependencyManager is the depended-upon package's package-manager label.
	// Optional.
	DependencyManager *string `json:"dependency_manager,omitempty"`

	// DependencyRange is the declared version range or constraint for this
	// dependency (for example "^1.3.0"). Optional.
	DependencyRange *string `json:"dependency_range,omitempty"`

	// DependencyType classifies the dependency (for example "runtime", "dev",
	// "peer", "build"). Optional.
	DependencyType *string `json:"dependency_type,omitempty"`

	// TargetFramework is the target framework moniker this dependency applies
	// under, for ecosystems with framework-conditional dependencies (for
	// example .NET). Optional.
	TargetFramework *string `json:"target_framework,omitempty"`

	// Marker is the ecosystem-specific environment marker or condition
	// controlling when this dependency applies (for example a Python PEP 508
	// marker). Optional.
	Marker *string `json:"marker,omitempty"`

	// Optional reports whether the declaring ecosystem marks this dependency
	// optional. Optional descriptive flag, NOT an identity key: a pointer so
	// nil (unreported) stays distinct from an observed false, matching the
	// ociregistry Mutated / terraformstate Sensitive *bool convention. The
	// collector's live emitter writes this key today, but the projector
	// re-decodes STORED facts on every re-projection (runtime.go), so a
	// persisted, older, or out-of-tree fact that omits it must still decode and
	// project the dependency edge (nil derefs to false in the row builder)
	// rather than quarantine the whole edge on a missing descriptive flag. See
	// Excluded for the same convention.
	Optional *bool `json:"optional,omitempty"`

	// Excluded reports whether this dependency is explicitly excluded from
	// resolution. Optional — see Optional's doc comment for the
	// descriptive-flag rationale shared by both boolean flags.
	Excluded *bool `json:"excluded,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this dependency. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
