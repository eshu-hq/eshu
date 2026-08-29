// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildWorkloadMaterializationReducerIntent builds one
// kubernetes_workload_materialization reducer intent per scope generation that
// observed a live Kubernetes workload. The pod-template fact is the trigger: the
// additive domain materializes those facts into canonical KubernetesWorkload
// nodes (keyed by the collector-emitted object_id), and the live-workload image
// edge slice (#388, kubernetes_correlation -> RUNS_IMAGE) gates on the
// kubernetes_workload_uid canonical-nodes phase that materialization publishes.
//
// Without this builder the handler is registered and wired but never receives an
// intent, so no KubernetesWorkload node is ever committed and the RUNS_IMAGE edge
// can never resolve. One intent per scope generation matches the per-scope
// conflict domain (no per-workload fan-out); the handler's FactLoader reads every
// pod-template in the generation. It mirrors BuildCorrelationReducerIntent,
// which returns the edge-domain intent from the same trigger fact.
func BuildWorkloadMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	return buildPodTemplateReducerIntent(
		scopeID, generationID, lookup,
		reducer.DomainKubernetesWorkloadMaterialization,
		workloadMaterializationAcceptanceUnit(scopeID),
	)
}

// workloadMaterializationAcceptanceUnit is the readiness acceptance
// unit (reducer-intent entity_key) under which the workload-materialization
// domain publishes its kubernetes_workload_uid canonical-nodes phase. The edge
// domain reuses this exact value so the claim-query readiness gate matches; see
// BuildCorrelationMaterializationReducerIntent.
func workloadMaterializationAcceptanceUnit(scopeID string) string {
	return "kubernetes_workload_materialization:" + scopeID
}

// buildPodTemplateReducerIntent builds one scope-keyed reducer intent
// for an additive Kubernetes domain triggered by the live-workload pod-template
// fact. The two pod-template-driven domains (workload-node materialization and
// the RUNS_IMAGE edge) share an identical trigger, reason, and source system and
// differ only by domain and acceptance unit, so they funnel through this helper.
// Returns ok=false when the generation observed no live workload.
func buildPodTemplateReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
	domain reducer.Domain,
	entityKey string,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.KubernetesPodTemplateFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       domain,
		EntityKey:    entityKey,
		Reason:       "kubernetes live workload pod-template facts observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

// BuildCorrelationMaterializationReducerIntent builds one
// kubernetes_correlation_materialization reducer intent per scope generation that
// observed a live Kubernetes workload. That additive graph-write domain promotes
// the exact image correlation decisions into canonical RUNS_IMAGE edges between
// the live KubernetesWorkload node and the digest-addressed OCI source node. Like
// the workload-node materialization intent above, it had no projector builder, so
// the handler was registered and wired but never received an intent and no
// RUNS_IMAGE edge ever formed. It gates on the canonical-nodes-committed
// readiness phase, so it safely resolves in a later drain once the workload and
// OCI nodes commit.
func BuildCorrelationMaterializationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	// The readiness gate matches this intent's acceptance unit (its entity_key)
	// against the kubernetes_workload_uid canonical-nodes phase the
	// workload-materialization domain publishes. That phase's acceptance unit is
	// the workload intent's entity_key, so the edge intent must carry the SAME key
	// (the node domain's), not its own — mirroring how workload_cloud_relationship
	// keys off "aws_resource_materialization:<scope>". Using a distinct key here
	// leaves the edge permanently unclaimable (the gate never matches).
	return buildPodTemplateReducerIntent(
		scopeID, generationID, lookup,
		reducer.DomainKubernetesCorrelationMaterialization,
		workloadMaterializationAcceptanceUnit(scopeID),
	)
}
