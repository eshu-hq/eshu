// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// TagObservation is the schema-version-1 typed payload for the
// "oci_registry.image_tag_observation" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A mutable tag-to-digest observation. Its graph identity is an OBSERVATION
// identity, not an image identity: the projector's tag-observation UID folds
// RepositoryID, Tag, and ResolvedDigest, and the container-image-identity
// reducer keys tag observations on the same three. All three are therefore
// required — absent any one today yields a UID built from an empty-string
// segment (or a silently dropped row); requiring them makes an absent key a
// classified input_invalid dead-letter. A present-but-empty value stays a valid
// decode the projector's identity gate still drops.
//
// ResolvedDigest is required because it is the tag's join target: it derives
// the ResolvedDescriptorUID that links the tag observation to its digest node.
// The remaining fields are optional common properties.
type TagObservation struct {
	// RepositoryID is the owning repository identity. Required.
	RepositoryID string `json:"repository_id"`

	// Tag is the mutable tag name observed. Required.
	Tag string `json:"tag"`

	// ResolvedDigest is the digest the tag resolved to at observation time.
	// Required — the tag observation's join target.
	ResolvedDigest string `json:"resolved_digest"`

	// MediaType is the resolved manifest's media type, when observed. Optional.
	MediaType *string `json:"media_type,omitempty"`

	// PreviousDigest is the digest the tag previously pointed at, when the
	// collector observed a mutation. Optional.
	PreviousDigest *string `json:"previous_digest,omitempty"`

	// Mutated reports whether the tag was observed to have moved off a prior
	// digest. Optional pointer so nil (unreported) stays distinct from an
	// observed false.
	Mutated *bool `json:"mutated,omitempty"`

	// IdentityStrength classifies how strong the tag-to-digest evidence is
	// ("weak_tag" by default). Optional: the projector substitutes "weak_tag"
	// when it is empty, so an absent value never blocks the observation.
	IdentityStrength *string `json:"identity_strength,omitempty"`

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
