// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetes

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BuildCorrelationReducerIntent enqueues one kubernetes_correlation
// reducer intent per scope generation that observed a live Kubernetes workload.
// The pod-template fact is the trigger because it carries the workload identity
// and image references the correlation read model joins to deployment-source
// evidence. One intent per scope generation keeps the conflict domain the
// per-scope-generation reducer intent (no fan-out per workload).
func BuildCorrelationReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstOfKind(facts.KubernetesPodTemplateFactKind)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainKubernetesCorrelation,
		EntityKey:    "kubernetes_correlation:" + scopeID,
		Reason:       "kubernetes live workload evidence observed",
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}
