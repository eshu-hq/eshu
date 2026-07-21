// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// DecodeKubernetesLivePodTemplate decodes env.Payload into the latest
// kuberneteslivev1.PodTemplate struct for the "kubernetes_live.pod_template"
// fact kind, dispatching on env.SchemaVersion major per Contract System v1
// §3.2. Callers (reducer handlers) receive either the decoded struct or a
// classified *DecodeError; they must never substitute a zero-value struct on
// error. A payload missing the required object_id key dead-letters as
// input_invalid rather than producing an empty-string KubernetesWorkload node
// identity.
func DecodeKubernetesLivePodTemplate(env Envelope) (kuberneteslivev1.PodTemplate, error) {
	return decodeLatestMajor[kuberneteslivev1.PodTemplate](FactKindKubernetesLivePodTemplate, env)
}

// EncodeKubernetesLivePodTemplate marshals a kuberneteslivev1.PodTemplate into
// the map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeKubernetesLivePodTemplate for schema-version-1 payloads, used by
// collectors emitting this fact kind and by this module's round-trip tests.
func EncodeKubernetesLivePodTemplate(podTemplate kuberneteslivev1.PodTemplate) (map[string]any, error) {
	payload := map[string]any{
		"object_id": podTemplate.ObjectID,
	}
	addStringPtr(payload, "cluster_id", podTemplate.ClusterID)
	addStringPtr(payload, "namespace", podTemplate.Namespace)
	addStringPtr(payload, "name", podTemplate.Name)
	addStringPtr(payload, "uid", podTemplate.WorkloadUID)
	addStringPtr(payload, "group_version_resource", podTemplate.GroupVersionResource)
	addStringPtr(payload, "service_account", podTemplate.ServiceAccount)
	if podTemplate.Containers != nil {
		payload["containers"] = encodeKubernetesLiveContainers(podTemplate.Containers)
	}
	addStringSlice(payload, "image_refs", podTemplate.ImageRefs)
	addStringMap(payload, "selector", podTemplate.Selector)
	addStringMap(payload, "labels", podTemplate.Labels)
	addStringMap(payload, "annotations", podTemplate.Annotations)
	addStringSlice(payload, "correlation_anchors", podTemplate.CorrelationAnchors)
	addInt32Ptr(payload, "desired_replicas", podTemplate.DesiredReplicas)
	addInt32Ptr(payload, "ready_replicas", podTemplate.ReadyReplicas)
	addInt32Ptr(payload, "available_replicas", podTemplate.AvailableReplicas)
	addStringPtr(payload, "pod_phase", podTemplate.PodPhase)
	return payload, nil
}

// DecodeKubernetesLiveRelationship decodes env.Payload into the latest
// kuberneteslivev1.Relationship struct for the "kubernetes_live.relationship"
// fact kind. See DecodeKubernetesLivePodTemplate for the dispatch and error
// contract. A payload missing a required edge field (relationship_type,
// from_object_id, or to_object_id) dead-letters as input_invalid rather than
// producing a directed edge with a blank endpoint or type.
func DecodeKubernetesLiveRelationship(env Envelope) (kuberneteslivev1.Relationship, error) {
	return decodeLatestMajor[kuberneteslivev1.Relationship](FactKindKubernetesLiveRelationship, env)
}

// EncodeKubernetesLiveRelationship marshals a kuberneteslivev1.Relationship
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeKubernetesLiveRelationship for schema-version-1 payloads.
func EncodeKubernetesLiveRelationship(relationship kuberneteslivev1.Relationship) (map[string]any, error) {
	payload := map[string]any{
		"relationship_type": relationship.RelationshipType,
		"from_object_id":    relationship.FromObjectID,
		"to_object_id":      relationship.ToObjectID,
	}
	addStringPtr(payload, "cluster_id", relationship.ClusterID)
	addStringPtr(payload, "from_group_version_resource", relationship.FromGroupVersionResource)
	addStringPtr(payload, "to_group_version_resource", relationship.ToGroupVersionResource)
	addStringSlice(payload, "correlation_anchors", relationship.CorrelationAnchors)
	return payload, nil
}

// DecodeKubernetesLiveWarning decodes env.Payload into the latest
// kuberneteslivev1.Warning struct for the "kubernetes_live.warning" fact
// kind. See DecodeKubernetesLivePodTemplate for the dispatch and error
// contract. A payload missing the required reason or cluster_id key
// dead-letters as input_invalid.
func DecodeKubernetesLiveWarning(env Envelope) (kuberneteslivev1.Warning, error) {
	return decodeLatestMajor[kuberneteslivev1.Warning](FactKindKubernetesLiveWarning, env)
}

// EncodeKubernetesLiveWarning marshals a kuberneteslivev1.Warning into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeKubernetesLiveWarning for schema-version-1 payloads.
func EncodeKubernetesLiveWarning(warning kuberneteslivev1.Warning) (map[string]any, error) {
	payload := map[string]any{
		"reason":     warning.Reason,
		"cluster_id": warning.ClusterID,
	}
	addStringPtr(payload, "resource_scope", warning.ResourceScope)
	addStringPtr(payload, "message", warning.Message)
	addStringSlice(payload, "correlation_anchors", warning.CorrelationAnchors)
	return payload, nil
}

func encodeKubernetesLiveContainers(containers []kuberneteslivev1.PodTemplateContainer) []map[string]any {
	out := make([]map[string]any, 0, len(containers))
	for _, container := range containers {
		payload := make(map[string]any)
		addStringPtr(payload, "name", container.Name)
		addStringPtr(payload, "image", container.Image)
		addBoolPtr(payload, "init", container.Init)
		if container.Ports != nil {
			payload["ports"] = container.Ports
		}
		addStringSlice(payload, "env_keys", container.EnvKeys)
		addBoolPtr(payload, "env_from_secret", container.EnvFromSecret)
		addStringPtr(payload, "resolved_image_digest", container.ResolvedImageDigest)
		out = append(out, payload)
	}
	return out
}
