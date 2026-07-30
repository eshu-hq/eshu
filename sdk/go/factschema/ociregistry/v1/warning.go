// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Warning is the schema-version-1 typed payload for the
// "oci_registry.warning" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// The container-image-identity reducer consumes this payload before retiring a
// previously canonical image reference. Active config-blob, tag-list, and
// missing-manifest warnings hold the affected reference set; missing-manifest
// is repository-wide because the payload carries no scanned-reference field.
// Malformed active warnings fail retirement closed so bounded collector
// incompleteness is not mistaken for authoritative demotion.
//
// The required field is WarningCode: the collector emitter fails closed on a
// blank warning code (warning.go rejects it before the envelope is built), so
// it is the kind's only unconditional wire invariant. WarningKey is derived
// (defaults to WarningCode) and always emitted, but is not separately validated
// by the emitter, so it stays optional. A retirement consumer applies stricter
// code-specific targeting: config_blob_unavailable requires a concrete
// RepositoryID and sha256 Digest, while tag_list_truncated and
// missing_manifest_digest require a concrete RepositoryID. Missing, malformed,
// or registry-wide placeholder targets fail retirement closed.
type Warning struct {
	// WarningCode is the bounded warning code the collector classified.
	// Required — the emitter fails closed on a blank code.
	WarningCode string `json:"warning_code"`

	// WarningKey is the stable per-warning key (defaults to WarningCode).
	// Optional: derived and always emitted, but not a separate emitter-
	// validated invariant.
	WarningKey *string `json:"warning_key,omitempty"`

	// Severity is the warning severity ("warning" by default). Optional.
	Severity *string `json:"severity,omitempty"`

	// Message is the redaction-safe warning message. Optional.
	Message *string `json:"message,omitempty"`

	// Digest is the digest the warning is scoped to, when applicable. Optional.
	Digest *string `json:"digest,omitempty"`

	// ReferrersState classifies the referrers-API state when the warning is an
	// unsupported-referrers-API warning. Optional.
	ReferrersState *string `json:"referrers_state,omitempty"`

	// RepositoryID is the owning repository identity, when the warning is scoped
	// to one repository. Optional: a registry-wide warning carries the shared
	// "oci-registry://warnings" placeholder.
	RepositoryID *string `json:"repository_id,omitempty"`

	// Provider is the registry provider kind. Optional.
	Provider *string `json:"provider,omitempty"`

	// Registry is the registry host. Optional.
	Registry *string `json:"registry,omitempty"`

	// Repository is the repository path within the registry. Optional.
	Repository *string `json:"repository,omitempty"`

	// CollectorInstanceID is the emitting collector instance's boundary id.
	// Optional control-plane metadata.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are the redaction-safe anchors the collector published.
	// Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
