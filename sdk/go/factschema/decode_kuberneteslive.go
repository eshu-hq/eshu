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
	return encodeToPayload(podTemplate)
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
	return encodeToPayload(relationship)
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
	return encodeToPayload(warning)
}
