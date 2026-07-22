// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildKubernetesNamespaceMaterializationReducerIntent enqueues one
// kubernetes_namespace_materialization intent for each scope generation that
// contains live namespace facts. The reducer handler loads every namespace fact
// in that generation, so the projector emits one scope-keyed intent rather than
// one work item per namespace. Generations without namespace facts do not enqueue
// the domain.
func buildKubernetesNamespaceMaterializationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.KubernetesNamespaceFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainKubernetesNamespaceMaterialization,
		EntityKey:    "kubernetes_namespace_materialization:" + scopeValue.ScopeID,
		Reason:       "kubernetes live namespace facts observed",
		FactID:       envelope.FactID,
		SourceSystem: kubernetesCorrelationSourceSystem(envelope),
	}, true
}
