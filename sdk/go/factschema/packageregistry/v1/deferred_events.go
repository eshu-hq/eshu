// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// RegistryEvent, RepositoryHosting, and Warning are TYPED-BUT-NOT-YET-CONSUMED
// (see doc.go and deferred.go's package-level doc comment for the shared
// rationale). This file continues deferred.go's SourceHint/PackageArtifact/
// VulnerabilityHint split purely to keep each file well under the 500-line
// cap; there is no semantic grouping distinction between the two files.

// RegistryEvent is the schema-version-1 typed payload for the
// "package_registry.registry_event" fact kind (Contract System v1 §3.1).
//
// A registry event records a publish, delete, deprecate, or similar
// registry-reported lifecycle event
// (go/internal/collector/packageregistry/registry_event.go
// NewRegistryEventEnvelope). The emitter fails closed on a blank event_key and
// event_type, so EventKey and EventType are REQUIRED.
type RegistryEvent struct {
	// EventKey is this event's own stable identity. Required: the emitter
	// rejects a blank event_key before building the envelope.
	EventKey string `json:"event_key"`

	// EventType classifies the event (for example "publish", "unpublish",
	// "deprecate"). Required: the emitter rejects a blank event_type before
	// building the envelope.
	EventType string `json:"event_type"`

	// PackageID is the package this event applies to, when the registry
	// reported one. Optional: the emitter derives it best-effort via
	// optionalPackageVersionID, which tolerates an absent package identity for
	// registry-wide events.
	PackageID *string `json:"package_id,omitempty"`

	// VersionID is the version this event applies to, when scoped to one
	// version. Optional.
	VersionID *string `json:"version_id,omitempty"`

	// Version is the raw version string paired with VersionID. Optional.
	Version *string `json:"version,omitempty"`

	// Ecosystem is the package ecosystem. Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the registry base URL or identifier. Optional.
	Registry *string `json:"registry,omitempty"`

	// ArtifactKey is the specific artifact this event applies to, when scoped
	// to one artifact rather than the version as a whole. Optional.
	ArtifactKey *string `json:"artifact_key,omitempty"`

	// Actor is the identity that triggered the event, when the registry
	// reported one. Optional.
	Actor *string `json:"actor,omitempty"`

	// Message is a human-readable event description or reason. Optional.
	Message *string `json:"message,omitempty"`

	// OccurredAt is the event's reported occurrence timestamp. Optional.
	OccurredAt *string `json:"occurred_at,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this event. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}

// RepositoryHosting is the schema-version-1 typed payload for the
// "package_registry.repository_hosting" fact kind (Contract System v1 §3.1).
//
// A repository-hosting fact records provider repository/feed topology (for
// example an Artifactory repository) rather than package identity
// (go/internal/collector/packageregistry/repository_hosting.go
// NewRepositoryHostingEnvelope). The emitter fails closed on a blank provider,
// registry, and repository, so those three are REQUIRED — together they form
// the emitter's own repositoryID ("provider://registry/repository").
type RepositoryHosting struct {
	// Provider is the hosting provider label (for example "artifactory").
	// Required: the emitter rejects a blank provider before building the
	// envelope.
	Provider string `json:"provider"`

	// Registry is the normalized registry identifier. Required: the emitter
	// rejects a blank (post-normalization) registry before building the
	// envelope.
	Registry string `json:"registry"`

	// Repository is the repository or feed name within the provider/registry.
	// Required: the emitter rejects a blank repository before building the
	// envelope.
	Repository string `json:"repository"`

	// RepositoryType classifies the repository (for example "local", "remote",
	// "virtual"). Optional: the emitter trims but does not require it.
	RepositoryType *string `json:"repository_type,omitempty"`

	// Ecosystem is the package ecosystem this repository serves, when the
	// provider scopes repositories by ecosystem. Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// UpstreamID is the identifier of an upstream/remote repository this one
	// proxies, when applicable. Optional.
	UpstreamID *string `json:"upstream_id,omitempty"`

	// UpstreamURL is the URL of the upstream/remote repository this one
	// proxies. Optional.
	UpstreamURL *string `json:"upstream_url,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// observed this repository. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}

// Warning is the schema-version-1 typed payload for the
// "package_registry.warning" fact kind (Contract System v1 §3.1).
//
// A warning records a non-fatal package-registry collection issue
// (go/internal/collector/packageregistry/warning.go NewWarningEnvelope). The
// emitter fails closed on a blank warning_key and warning_code, so those two
// are REQUIRED. Ecosystem's SQL-loader significance is documented on doc.go
// and locked by package_registry_sql_schema_lockstep_test.go: the status
// registry query (status_registry.go) reads payload->>'ecosystem' and
// payload->>'warning_code' directly.
type Warning struct {
	// WarningKey is this warning's own stable identity. Required: the emitter
	// rejects a blank warning_key before building the envelope.
	WarningKey string `json:"warning_key"`

	// WarningCode is the bounded warning category (for example
	// "unsupported_metadata_source", "registry_not_found"). Required: the
	// emitter rejects a blank warning_code before building the envelope, and it
	// is the field the operator status registry query reads directly (see
	// doc.go).
	WarningCode string `json:"warning_code"`

	// Severity is the classified warning severity, when the warning code
	// classifies. Optional.
	Severity *string `json:"severity,omitempty"`

	// Message is the human-readable warning explanation. Optional.
	Message *string `json:"message,omitempty"`

	// Ecosystem is the package ecosystem the warning applies to, when the
	// collector could attribute one. Optional: the emitter writes this key
	// only when non-empty (either from a direct ecosystem observation or a
	// derived package identity). It is also the field the operator status
	// registry query reads directly (see doc.go), so an absent value there
	// simply means the warning is not attributed to one ecosystem.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// Registry is the registry base URL or identifier, when the warning is
	// attributed to a specific package. Optional.
	Registry *string `json:"registry,omitempty"`

	// PackageID is the package this warning applies to, when attributable.
	// Optional.
	PackageID *string `json:"package_id,omitempty"`

	// Version is the raw version string, when the warning is attributed to a
	// specific version. Optional.
	Version *string `json:"version,omitempty"`

	// VersionID is the version identity key paired with Version. Optional.
	VersionID *string `json:"version_id,omitempty"`

	// CollectorInstanceID identifies the collector process instance that
	// raised this warning. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are redaction-safe identity strings the collector
	// publishes for cross-fact correlation. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
