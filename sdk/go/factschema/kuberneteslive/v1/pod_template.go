// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// PodTemplate is the schema-version-1 typed payload for the
// "kubernetes_live.pod_template" fact kind: one live pod-template-backed
// workload identity observed in a Kubernetes cluster.
//
// The required set matches the collector emitter
// (kuberneteslive.NewPodTemplateEnvelope), which builds ObjectID from the
// validated ObjectIdentity before the envelope exists, and the reducer's own
// node-row gate (kubernetesWorkloadNodeRow), which already drops a pod
// template lacking an object_id rather than fabricating a node. ObjectID is
// therefore the sole required field: making it required means a collector
// regression that drops the key dead-letters as input_invalid instead of
// silently producing an empty-string KubernetesWorkload node identity — the
// exact accuracy hole Contract System v1 exists to close.
//
// Every other field is optional even though the emitter unconditionally
// writes most of them (ClusterID, Namespace, Name, WorkloadUID,
// GroupVersionResource, ServiceAccount, Containers, ImageRefs, Selector,
// Labels, CorrelationAnchors): the emitter can legitimately write an empty
// string for a cluster-scoped or unlabeled object (for example a
// cluster-scoped resource has no Namespace), and the reducer's own read path
// (kubernetesWorkloadNodeRow, workloadImageRefs) already tolerates an absent
// or empty value for every one of them. Requiring a field the emitter can
// validly leave empty would dead-letter a valid fact — the reverse of the
// accuracy goal.
type PodTemplate struct {
	// ObjectID is the collector-derived stable identity for this live object
	// (ObjectIdentity.ObjectID()). Required — it anchors the KubernetesWorkload
	// node, and the reducer's node-row gate already drops a fact lacking it
	// rather than fabricating a node.
	ObjectID string `json:"object_id"`

	// ClusterID is the operator-declared cluster identity. Optional: always
	// emitted, but the decode seam validates key presence, not non-emptiness.
	ClusterID *string `json:"cluster_id,omitempty"`

	// Namespace is the object's namespace. Optional: empty for a
	// cluster-scoped object (for example a ClusterRole).
	Namespace *string `json:"namespace,omitempty"`

	// Name is the object's name. Optional.
	Name *string `json:"name,omitempty"`

	// WorkloadUID is the raw Kubernetes metadata.uid, carried as a property
	// only — never the node identity (ObjectID already folds it into its
	// identity tuple). Optional.
	WorkloadUID *string `json:"uid,omitempty"`

	// GroupVersionResource is the object's api-group/version/resource label
	// (ObjectIdentity.GroupVersionResource()). Optional.
	GroupVersionResource *string `json:"group_version_resource,omitempty"`

	// ServiceAccount is the pod template's declared service account name.
	// Optional: empty when the pod template specifies none (Kubernetes
	// defaults to "default" server-side, but the collector observes the
	// declared value verbatim).
	ServiceAccount *string `json:"service_account,omitempty"`

	// Containers are the redacted per-container summaries (name, image,
	// init flag, declared ports, env var NAMES only, and whether a
	// secret-backed env reference exists). Optional: a pod template with no
	// containers is unusual but not invalid.
	Containers []PodTemplateContainer `json:"containers,omitempty"`

	// ImageRefs are the workload's declared image references, redacted to
	// the raw reference string. Optional.
	ImageRefs []string `json:"image_refs,omitempty"`

	// Selector is the workload's label selector (for a workload-type object;
	// nil for objects with no selector). Optional.
	Selector map[string]string `json:"selector,omitempty"`

	// Labels are the object's labels. Optional.
	Labels map[string]string `json:"labels,omitempty"`

	// CorrelationAnchors are redaction-safe join anchors (the object id plus
	// every declared image reference) the correlation reducer domain may use
	// for name-only join resolution. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}

// PodTemplateContainer is the redacted, metadata-only view of one container or
// init container in a pod template. It carries image references and declared
// shape, never environment values, secret references resolved to values, or
// logs.
type PodTemplateContainer struct {
	// Name is the container name. Optional.
	Name *string `json:"name,omitempty"`

	// Image is the raw image reference string as declared in the pod
	// template. Optional: the collector may observe a container with no
	// image reference declared (malformed spec).
	Image *string `json:"image,omitempty"`

	// Init reports whether this is an init container. Optional: the emitter
	// always writes it, but false and "not observed" are both valid.
	Init *bool `json:"init,omitempty"`

	// Ports are the container's declared ports. Optional.
	Ports []int32 `json:"ports,omitempty"`

	// EnvKeys are environment variable NAMES only. Values are never
	// collected. Optional.
	EnvKeys []string `json:"env_keys,omitempty"`

	// EnvFromSecret reports whether the container references secret-backed
	// env without collecting any value. Optional.
	EnvFromSecret *bool `json:"env_from_secret,omitempty"`
}
