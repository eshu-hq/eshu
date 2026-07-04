// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Warning is the schema-version-1 typed payload for the
// "oci_registry.warning" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// DEFERRED — TYPED BUT NOT YET CONSUMED. oci_registry.warning has no read-side
// consumer in the projector or the reducer today: the collector emits it
// (ociregistry.NewWarningEnvelope) but no decode site reads it, so it is a
// declared provenance-only kind (design §3.4). This struct, its schema, its
// fixturepack entry, and its registry payload_schema ref exist so the kind is
// contract-complete and an external collector can validate it in conformance,
// but NO decode-site conversion, input_invalid regression test, or benchmark
// accompanies it — there is no read path to convert. It migrates its decode
// site WITH the future consumer, matching how the gcp wave shipped
// gcp_image_reference/gcp_tag_observation as typed-but-deferred.
//
// The required field is WarningCode: the collector emitter fails closed on a
// blank warning code (warning.go rejects it before the envelope is built), so
// it is the kind's only unconditional identity invariant. WarningKey is
// derived (defaults to WarningCode) and always emitted, but is not separately
// validated by the emitter, so it stays optional.
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
