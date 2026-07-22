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

	// Annotations are the object's identity-binding annotations, allowlisted
	// at the collector boundary to a fixed, small key set — currently
	// argocd.argoproj.io/tracking-id and the Kustomize/Helm app.kubernetes.io
	// instance/name convention — never the full Kubernetes annotation map.
	// It exists to carry the production-faithful declared->live identity
	// signal that survives a Helm/Kustomize rename — so the reducer can bind
	// a live workload back to its declared source without relying on a
	// shared image digest (#5471 F2). This schema places no upper bound on
	// map size or value length; the collector (see
	// go/internal/collector/kuberneteslive/clientgo, identityAnnotationAllowlist)
	// is responsible for the allowlist because raw Kubernetes annotation
	// values are unbounded and can embed secret material (e.g.
	// kubectl.kubernetes.io/last-applied-configuration). Optional: absent
	// when the object carries no allowlisted annotation or the collector
	// observed none; an absent map must decode to nil, never an empty map,
	// so callers can distinguish "not observed" from "observed empty".
	Annotations map[string]string `json:"annotations,omitempty"`

	// CorrelationAnchors are redaction-safe join anchors (the object id plus
	// every declared image reference) the correlation reducer domain may use
	// for name-only join resolution. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// DesiredReplicas is the DESIRED replica count declared on a Deployment or
	// ReplicaSet's .Spec.Replicas. It is the desired-state truth basis, not an
	// observation of running pods. Absent for a Pod object (a Pod has no
	// replica spec) and absent when the source Spec.Replicas was nil. Optional.
	DesiredReplicas *int32 `json:"desired_replicas,omitempty"`

	// ReadyReplicas is the OBSERVED ready replica count from a Deployment or
	// ReplicaSet's .Status.ReadyReplicas. Absent for a Pod object. Optional.
	ReadyReplicas *int32 `json:"ready_replicas,omitempty"`

	// AvailableReplicas is the OBSERVED available replica count from a
	// Deployment or ReplicaSet's .Status.AvailableReplicas. Absent for a Pod
	// object. Optional.
	AvailableReplicas *int32 `json:"available_replicas,omitempty"`

	// PodPhase is the OBSERVED pod lifecycle phase from a Pod's .Status.Phase
	// (one of Pending, Running, Succeeded, Failed, or Unknown). Absent for a
	// Deployment or ReplicaSet object (they carry a pod template spec, not pod
	// status) and absent when the source phase was empty. Optional.
	PodPhase *string `json:"pod_phase,omitempty"`
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

	// ResolvedImageDigest is the CRI-resolved digest for this container,
	// normalized from pod.Status.ContainerStatuses[].ImageID into the bare
	// repo@sha256:<digest> form. It is present only when the pod status has been
	// observed and the ImageID normalizes to a joinable digest. Deployments and
	// ReplicaSets carry pod spec only (no status) so this field is absent for
	// them. Optional.
	ResolvedImageDigest *string `json:"resolved_image_digest,omitempty"`
}
