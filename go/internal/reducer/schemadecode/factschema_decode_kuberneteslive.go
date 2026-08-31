// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// DecodeKubernetesLivePodTemplate decodes one kubernetes_live.pod_template
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
func DecodeKubernetesLivePodTemplate(env facts.Envelope) (kuberneteslivev1.PodTemplate, error) {
	podTemplate, err := factschema.DecodeKubernetesLivePodTemplate(FactschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.PodTemplate{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesLivePodTemplate, err)
	}
	return podTemplate, nil
}

// DecodeKubernetesLiveRelationship decodes one kubernetes_live.relationship
// envelope into the typed kuberneteslivev1.Relationship struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing a required field (relationship_type, from_object_id,
// to_object_id) or is otherwise malformed. It is the single decode site for
// the kubernetes_live.relationship kind on the reducer side.
func DecodeKubernetesLiveRelationship(env facts.Envelope) (kuberneteslivev1.Relationship, error) {
	relationship, err := factschema.DecodeKubernetesLiveRelationship(FactschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.Relationship{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesLiveRelationship, err)
	}
	return relationship, nil
}

// DecodeKubernetesLiveWarning decodes one kubernetes_live.warning envelope
// into the typed kuberneteslivev1.Warning struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (reason, cluster_id) or is otherwise malformed. It is the
// single decode site for the kubernetes_live.warning kind on the reducer
// side.
func DecodeKubernetesLiveWarning(env facts.Envelope) (kuberneteslivev1.Warning, error) {
	warning, err := factschema.DecodeKubernetesLiveWarning(FactschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.Warning{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesLiveWarning, err)
	}
	return warning, nil
}

// DecodeKubernetesLiveNamespace decodes one kubernetes_live.namespace
// envelope into the typed kuberneteslivev1.Namespace struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required field (object_id) or is otherwise
// malformed. It is the single decode site for the kubernetes_live.namespace
// kind on the reducer side (issue #5434).
func DecodeKubernetesLiveNamespace(env facts.Envelope) (kuberneteslivev1.Namespace, error) {
	namespace, err := factschema.DecodeKubernetesLiveNamespace(FactschemaEnvelope(env))
	if err != nil {
		return kuberneteslivev1.Namespace{}, factdecode.NewFactDecodeError(factschema.FactKindKubernetesLiveNamespace, err)
	}
	return namespace, nil
}
