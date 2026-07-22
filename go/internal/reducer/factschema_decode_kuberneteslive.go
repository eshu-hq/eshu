// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// decodeKubernetesLivePodTemplate decodes one kubernetes_live.pod_template
// envelope into the typed kuberneteslivev1.PodTemplate struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required field (object_id) or is otherwise
// malformed. It is the single decode site for the kubernetes_live.pod_template
// kind on the reducer side: every handler that consumes pod-template facts
// decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string KubernetesWorkload node
// identity or a whole-intent abort.
//
// This wrapper lives in a per-family factschema_decode_kuberneteslive.go file.
// The Contract System v1 §6 gate-2 payload-usage manifest globs the reducer
// dir's factschema_decode*.go files for decode seams
// (go/internal/payloadusage), so a per-family file is discovered and gated
// the same as the main file; keeping each family's decode wrappers in its own
// file keeps the diff of a new family self-contained.
func decodeKubernetesLivePodTemplate(env facts.Envelope) (kuberneteslivev1.PodTemplate, error) {
	podTemplate, err := factschema.DecodeKubernetesLivePodTemplate(factschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.PodTemplate{}, newFactDecodeError(factschema.FactKindKubernetesLivePodTemplate, err)
	}
	return podTemplate, nil
}

// decodeKubernetesLiveRelationship decodes one kubernetes_live.relationship
// envelope into the typed kuberneteslivev1.Relationship struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing a required field (relationship_type, from_object_id,
// to_object_id) or is otherwise malformed. It is the single decode site for
// the kubernetes_live.relationship kind on the reducer side.
func decodeKubernetesLiveRelationship(env facts.Envelope) (kuberneteslivev1.Relationship, error) {
	relationship, err := factschema.DecodeKubernetesLiveRelationship(factschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.Relationship{}, newFactDecodeError(factschema.FactKindKubernetesLiveRelationship, err)
	}
	return relationship, nil
}

// decodeKubernetesLiveWarning decodes one kubernetes_live.warning envelope
// into the typed kuberneteslivev1.Warning struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (reason, cluster_id) or is otherwise malformed. It is the
// single decode site for the kubernetes_live.warning kind on the reducer
// side.
func decodeKubernetesLiveWarning(env facts.Envelope) (kuberneteslivev1.Warning, error) {
	warning, err := factschema.DecodeKubernetesLiveWarning(factschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.Warning{}, newFactDecodeError(factschema.FactKindKubernetesLiveWarning, err)
	}
	return warning, nil
}

// decodeKubernetesLiveNamespace decodes one kubernetes_live.namespace
// envelope into the typed kuberneteslivev1.Namespace struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required field (object_id) or is otherwise
// malformed. It is the single decode site for the kubernetes_live.namespace
// kind on the reducer side (issue #5434).
func decodeKubernetesLiveNamespace(env facts.Envelope) (kuberneteslivev1.Namespace, error) {
	namespace, err := factschema.DecodeKubernetesLiveNamespace(factschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.Namespace{}, newFactDecodeError(factschema.FactKindKubernetesLiveNamespace, err)
	}
	return namespace, nil
}
