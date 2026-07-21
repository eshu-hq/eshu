// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Namespace is the schema-version-1 typed payload for the
// "kubernetes_live.namespace" fact kind: one live Kubernetes namespace
// object's metadata-only evidence, keyed by (cluster_id, namespace) through
// the underlying collector-derived ObjectIdentity. It carries the
// namespace's labels so the reducer can decide, per namespace, whether a
// label declares an alias-recognized environment (issue #5434); it never
// carries annotations (reserved, see Annotations) or any other namespace
// object detail.
//
// The required set matches the collector emitter
// (kuberneteslive.NewNamespaceEnvelope), which builds ObjectID from the
// validated ObjectIdentity before the envelope exists, mirroring
// PodTemplate's ObjectID contract. ObjectID is therefore the sole required
// field: making it required means a collector regression that drops the key
// dead-letters as input_invalid instead of silently producing an
// empty-string namespace identity.
type Namespace struct {
	// ObjectID is the collector-derived stable identity for this live
	// namespace object (ObjectIdentity.ObjectID()). Required — it anchors
	// the namespace's (cluster_id, namespace) identity for the environment
	// alias binding reducer domain.
	ObjectID string `json:"object_id"`

	// ClusterID is the operator-declared cluster identity. Optional: always
	// emitted, but the decode seam validates key presence, not
	// non-emptiness.
	ClusterID *string `json:"cluster_id,omitempty"`

	// Namespace is the namespace name itself (a Namespace object's
	// metadata.name; the object is cluster-scoped so it carries no
	// containing namespace of its own). Optional: always emitted by the
	// collector, but the decode seam validates key presence, not
	// non-emptiness.
	Namespace *string `json:"namespace,omitempty"`

	// Labels are the namespace object's labels. This is the ONLY evidence
	// surface for environment-alias binding (issue #5434): the reducer
	// checks a small documented set of label keys against
	// go/internal/environment's known-token vocabulary to decide whether
	// this namespace binds to an Environment node. Optional: a namespace
	// with no labels is valid and stays environment-unbound.
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is reserved for #5444 (ArgoCD-destination evidence). This
	// fact kind's scope is labels only — annotations may carry sensitive or
	// operator-authored data that breaches the metadata-only invariant
	// (go/internal/collector/kuberneteslive/AGENTS.md) without a redaction
	// review, so the collector never populates this field today; it always
	// encodes as nil/absent. The field exists only to reserve the payload
	// slot so #5444 is an additive minor bump, not a shape change.
	Annotations map[string]string `json:"annotations,omitempty"`

	// CorrelationAnchors are redaction-safe join anchors (the object id) the
	// correlation reducer domain may use for name-only join resolution.
	// Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
