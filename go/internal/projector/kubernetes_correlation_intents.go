// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// buildKubernetesCorrelationReducerIntent enqueues one kubernetes_correlation
// reducer intent per scope generation that observed a live Kubernetes workload.
// The pod-template fact is the trigger because it carries the workload identity
// and image references the correlation read model joins to deployment-source
// evidence. One intent per scope generation keeps the conflict domain the
// per-scope-generation reducer intent (no fan-out per workload).
func buildKubernetesCorrelationReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstOfKind(facts.KubernetesPodTemplateFactKind)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainKubernetesCorrelation,
		EntityKey:    "kubernetes_correlation:" + scopeValue.ScopeID,
		Reason:       "kubernetes live workload evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: kubernetesCorrelationSourceSystem(envelope),
	}, true
}

func kubernetesCorrelationSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
