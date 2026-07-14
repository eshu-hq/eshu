// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildKubernetesWorkloadMaterializationReducerIntent enqueues one
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
// pod-template in the generation. It mirrors buildKubernetesCorrelationReducerIntent,
// which already enqueues the edge domain from the same trigger fact.
func buildKubernetesWorkloadMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	return buildKubernetesPodTemplateReducerIntent(
		scopeValue, generation, index,
		reducer.DomainKubernetesWorkloadMaterialization,
		kubernetesWorkloadMaterializationAcceptanceUnit(scopeValue),
	)
}

// kubernetesWorkloadMaterializationAcceptanceUnit is the readiness acceptance
// unit (reducer-intent entity_key) under which the workload-materialization
// domain publishes its kubernetes_workload_uid canonical-nodes phase. The edge
// domain reuses this exact value so the claim-query readiness gate matches; see
// buildKubernetesCorrelationMaterializationReducerIntent.
func kubernetesWorkloadMaterializationAcceptanceUnit(scopeValue scope.IngestionScope) string {
	return "kubernetes_workload_materialization:" + scopeValue.ScopeID
}

// buildKubernetesPodTemplateReducerIntent builds one scope-keyed reducer intent
// for an additive Kubernetes domain triggered by the live-workload pod-template
// fact. The two pod-template-driven domains (workload-node materialization and
// the RUNS_IMAGE edge) share an identical trigger, reason, and source system and
// differ only by domain and acceptance unit, so they funnel through this helper.
// Returns ok=false when the generation observed no live workload.
func buildKubernetesPodTemplateReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
	domain reducer.Domain,
	entityKey string,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.KubernetesPodTemplateFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       domain,
		EntityKey:    entityKey,
		Reason:       "kubernetes live workload pod-template facts observed",
		FactID:       envelope.FactID,
		SourceSystem: kubernetesCorrelationSourceSystem(envelope),
	}, true
}

// buildKubernetesCorrelationMaterializationReducerIntent enqueues one
// kubernetes_correlation_materialization reducer intent per scope generation that
// observed a live Kubernetes workload. That additive graph-write domain promotes
// the exact image correlation decisions into canonical RUNS_IMAGE edges between
// the live KubernetesWorkload node and the digest-addressed OCI source node. Like
// the workload-node materialization intent above, it had no projector builder, so
// the handler was registered and wired but never received an intent and no
// RUNS_IMAGE edge ever formed. It gates on the canonical-nodes-committed
// readiness phase, so it safely resolves in a later drain once the workload and
// OCI nodes commit.
func buildKubernetesCorrelationMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	// The readiness gate matches this intent's acceptance unit (its entity_key)
	// against the kubernetes_workload_uid canonical-nodes phase the
	// workload-materialization domain publishes. That phase's acceptance unit is
	// the workload intent's entity_key, so the edge intent must carry the SAME key
	// (the node domain's), not its own — mirroring how workload_cloud_relationship
	// keys off "aws_resource_materialization:<scope>". Using a distinct key here
	// leaves the edge permanently unclaimable (the gate never matches).
	return buildKubernetesPodTemplateReducerIntent(
		scopeValue, generation, index,
		reducer.DomainKubernetesCorrelationMaterialization,
		kubernetesWorkloadMaterializationAcceptanceUnit(scopeValue),
	)
}
