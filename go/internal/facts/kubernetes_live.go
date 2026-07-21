// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// KubernetesPodTemplateFactKind identifies one pod-template-backed workload
	// identity observed live in a Kubernetes cluster. It carries container and
	// init-container image references, declared ports, environment variable key
	// names (never values), volume references, service account, and selector
	// metadata. It never carries secret values, ConfigMap data payloads, or
	// container logs.
	KubernetesPodTemplateFactKind = "kubernetes_live.pod_template"
	// KubernetesRelationshipFactKind identifies one directed relationship edge
	// observed between two live Kubernetes objects, such as owner references,
	// selector-derived workload-to-pod edges, or ingress-to-service edges. It
	// preserves selector ambiguity evidence rather than asserting exact
	// ownership; the reducer owns canonical edge admission.
	KubernetesRelationshipFactKind = "kubernetes_live.relationship"
	// KubernetesWarningFactKind identifies one non-fatal Kubernetes live
	// collection warning, such as a forbidden resource, partial list, skipped
	// secret, unsupported API, or ambiguous selector. A warning reports a
	// capability gap; it never asserts that a resource set is complete.
	KubernetesWarningFactKind = "kubernetes_live.warning"
	// KubernetesNamespaceFactKind identifies one live Kubernetes namespace
	// object's metadata-only evidence, keyed by (cluster_id, namespace). It
	// carries the namespace's labels only (never annotations, reserved for
	// #5444) so the reducer can decide, per namespace, whether a label
	// declares an alias-recognized environment (issue #5434). A namespace
	// with no alias-recognized label stays environment-unbound; this fact
	// kind never invents an environment.
	KubernetesNamespaceFactKind = "kubernetes_live.namespace"

	// KubernetesPodTemplateSchemaVersion is the first pod-template fact schema.
	// Bumped 1.0.0 -> 1.1.0 for the additive optional resolved_image_digest field
	// on PodTemplateContainer (issue #5432). Bumped 1.1.0 -> 1.2.0 for the
	// additive optional runtime-status fields (desired_replicas, ready_replicas,
	// available_replicas, pod_phase) on PodTemplate (issue #5431).
	KubernetesPodTemplateSchemaVersion = "1.2.0"
	// KubernetesRelationshipSchemaVersion is the first relationship fact schema.
	KubernetesRelationshipSchemaVersion = "1.0.0"
	// KubernetesWarningSchemaVersion is the first warning fact schema.
	KubernetesWarningSchemaVersion = "1.0.0"
	// KubernetesNamespaceSchemaVersion is the first namespace fact schema
	// (issue #5434).
	KubernetesNamespaceSchemaVersion = "1.0.0"
)

var kubernetesLiveFactKinds = []string{
	KubernetesPodTemplateFactKind,
	KubernetesRelationshipFactKind,
	KubernetesWarningFactKind,
	KubernetesNamespaceFactKind,
}

var kubernetesLiveSchemaVersions = map[string]string{
	KubernetesPodTemplateFactKind:  KubernetesPodTemplateSchemaVersion,
	KubernetesRelationshipFactKind: KubernetesRelationshipSchemaVersion,
	KubernetesWarningFactKind:      KubernetesWarningSchemaVersion,
	KubernetesNamespaceFactKind:    KubernetesNamespaceSchemaVersion,
}

// KubernetesLiveFactKinds returns the accepted Kubernetes live fact kinds in
// their emission order. The returned slice is a copy; callers may mutate it
// without affecting the package-level registry.
func KubernetesLiveFactKinds() []string {
	return slices.Clone(kubernetesLiveFactKinds)
}

// KubernetesLiveSchemaVersion returns the schema version for a Kubernetes live
// fact kind. The boolean is false when the fact kind is not part of the
// Kubernetes live contract.
func KubernetesLiveSchemaVersion(factKind string) (string, bool) {
	version, ok := kubernetesLiveSchemaVersions[factKind]
	return version, ok
}
