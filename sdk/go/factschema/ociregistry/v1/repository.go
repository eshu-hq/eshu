// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Repository is the schema-version-1 typed payload for the
// "oci_registry.repository" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// The required set is the single identity field the projector's repository
// node UID is derived from: RepositoryID. Every prior wave's rule applies —
// absent RepositoryID today yields a repository row with an empty-string UID
// (the projector drops it, but no operator signal is emitted), so requiring
// RepositoryID makes an absent key a classified input_invalid dead-letter,
// the accuracy fix this package adds. A present-but-empty RepositoryID stays a
// valid decode that the projector's own identity gate still drops, byte-
// identical to today.
//
// The remaining fields (provider, registry, repository, visibility, auth_mode)
// are common properties copied onto the node but never used to form identity,
// so they are optional: the collector always emits them, but an absent value
// must not dead-letter a fact whose identity is intact.
type Repository struct {
	// RepositoryID is the canonical repository identity
	// ("oci-registry://<registry>/<repository>") the repository node UID is
	// derived from. Required.
	RepositoryID string `json:"repository_id"`

	// Provider is the registry provider kind (ghcr, ecr, gar, ...). Optional:
	// always emitted but may be empty.
	Provider *string `json:"provider,omitempty"`

	// Registry is the registry host. Optional: always emitted but may be empty.
	Registry *string `json:"registry,omitempty"`

	// Repository is the repository path within the registry. Optional: always
	// emitted but may be empty.
	Repository *string `json:"repository,omitempty"`

	// Visibility is the repository visibility classification (public, private,
	// unknown). Optional.
	Visibility *string `json:"visibility,omitempty"`

	// AuthMode is the observed authentication mode (anonymous, credentialed,
	// unknown). Optional.
	AuthMode *string `json:"auth_mode,omitempty"`

	// CollectorInstanceID is the emitting collector instance's boundary id.
	// Optional control-plane metadata; carried for round-trip fidelity.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// CorrelationAnchors are the redaction-safe name/URI anchors the collector
	// published. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
