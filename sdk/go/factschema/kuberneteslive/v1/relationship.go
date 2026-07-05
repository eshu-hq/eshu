// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Relationship is the schema-version-1 typed payload for the
// "kubernetes_live.relationship" fact kind: one directed edge observed
// between two live Kubernetes objects (an owner reference, a
// selector-derived workload-to-pod edge, or an ingress-to-service edge).
//
// The required set matches the collector emitter
// (kuberneteslive.NewRelationshipEnvelope), which rejects a blank
// RelationshipType and an invalid From/To ObjectIdentity before the envelope
// is ever built, and the reducer's own edge-ingest gate
// (kubernetesCorrelationIndex.ingestRelationship), which already drops an
// edge missing any of the three fields rather than fabricating a partial
// edge. RelationshipType, FromObjectID, and ToObjectID are therefore
// required: a collector regression that drops one of them dead-letters as
// input_invalid instead of silently producing a directed edge with a blank
// endpoint or type.
type Relationship struct {
	// RelationshipType is the edge's relationship type (owner_reference,
	// selector_match, ingress_to_service). Required — the emitter rejects a
	// blank type, and the reducer's edge classifier already drops an edge
	// missing this field.
	RelationshipType string `json:"relationship_type"`

	// FromObjectID is the source object's collector-derived stable identity
	// (ObjectIdentity.ObjectID()). Required — the emitter validates the From
	// identity before building the envelope, and the reducer's edge classifier
	// already drops an edge missing this field.
	FromObjectID string `json:"from_object_id"`

	// ToObjectID is the target object's collector-derived stable identity.
	// Required — the emitter validates the To identity before building the
	// envelope, and the reducer's edge classifier already drops an edge
	// missing this field.
	ToObjectID string `json:"to_object_id"`

	// ClusterID is the operator-declared cluster identity. Optional: always
	// emitted, but the decode seam validates key presence, not
	// non-emptiness.
	ClusterID *string `json:"cluster_id,omitempty"`

	// FromGroupVersionResource is the source object's api-group/version/
	// resource label. Optional.
	FromGroupVersionResource *string `json:"from_group_version_resource,omitempty"`

	// ToGroupVersionResource is the target object's api-group/version/
	// resource label. Optional.
	ToGroupVersionResource *string `json:"to_group_version_resource,omitempty"`

	// CorrelationAnchors are redaction-safe join anchors (the from and to
	// object ids) the correlation reducer domain may use for name-only join
	// resolution. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
