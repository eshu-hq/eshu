// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// BuildNamespaceMaterializationReducerIntent enqueues one
// kubernetes_namespace_materialization intent for each scope generation that
// contains live namespace facts or is a complete Kubernetes live snapshot. The
// reducer handler loads every namespace fact in that generation, so the
// projector emits one scope-keyed intent rather than one work item per
// namespace. Complete snapshots enqueue even when empty so the reducer can
// retract namespaces that disappeared; partial empty snapshots never retract.
func BuildNamespaceMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, hasNamespaceFact := lookup.FirstOfKind(facts.KubernetesNamespaceFactKind)
	clusterID := strings.TrimSpace(scopeValue.Metadata["cluster_id"])
	reconcileComplete := scopeValue.ScopeKind == scope.KindCluster &&
		scopeValue.CollectorKind == scope.CollectorKubernetesLive &&
		strings.TrimSpace(generation.FreshnessHint) == "complete" &&
		clusterID != ""
	if !hasNamespaceFact && !reconcileComplete {
		return projectorintent.ReducerIntent{}, false
	}

	factID := ""
	sourceSystem := strings.TrimSpace(scopeValue.SourceSystem)
	if sourceSystem == "" {
		sourceSystem = string(scopeValue.CollectorKind)
	}
	reason := "complete kubernetes live namespace snapshot observed"
	if hasNamespaceFact {
		factID = envelope.FactID
		sourceSystem = projectorintent.SourceSystem(envelope)
		reason = "kubernetes live namespace facts observed"
	}
	var payload map[string]any
	if reconcileComplete {
		payload = map[string]any{
			"cluster_id":         clusterID,
			"reconcile_complete": true,
		}
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainKubernetesNamespaceMaterialization,
		EntityKey:    "kubernetes_namespace_materialization:" + scopeValue.ScopeID,
		Reason:       reason,
		FactID:       factID,
		SourceSystem: sourceSystem,
		Payload:      payload,
	}, true
}
