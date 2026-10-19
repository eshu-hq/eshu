// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

// resolveWorkloadIDs runs the decision workload references through the
// handle ladder before any write: a spanning handle links nothing rather
// than one tenant. A nil WorkloadLookup keeps the legacy decisions. A
// lookup failure fails the intent closed for retry.
func (h ServiceCatalogCorrelationHandler) resolveWorkloadIDs(
	ctx context.Context,
	intent reducercontract.Intent,
	decisions []ServiceCatalogCorrelationDecision,
) ([]ServiceCatalogCorrelationDecision, error) {
	if h.WorkloadLookup == nil {
		return decisions, nil
	}
	return ResolveServiceCatalogWorkloadIDs(ctx, intent.ScopeID, intent.GenerationID, decisions, &handle.Resolver{
		ScopeRepos:  h.ScopeRepos,
		Lookup:      h.WorkloadLookup,
		Instruments: h.Instruments,
	})
}

// ResolveServiceCatalogWorkloadIDs resolves every decision's WorkloadID
// through the handle ladder before any write, restricted by the decision's
// repository hint (workload_repository_id, else the link's decoded or
// matched repository) or the scope's repositories.
//
// A full id or unique handle links that node by rewriting the decision to
// the resolved node id. A handle spanning two repositories clears the link
// — catalog evidence never attaches to one tenant — and is counted with the
// alertable structured log, never silent. A miss keeps the raw selector so
// the read-side ladder still classifies it. A nil resolver keeps every
// decision (the unwired-handler legacy path).
func ResolveServiceCatalogWorkloadIDs(
	ctx context.Context,
	scopeID string,
	generationID string,
	decisions []ServiceCatalogCorrelationDecision,
	resolver *handle.Resolver,
) ([]ServiceCatalogCorrelationDecision, error) {
	if len(decisions) == 0 {
		return decisions, nil
	}
	if resolver == nil || resolver.Lookup == nil {
		return decisions, nil
	}
	items := make([]handle.Item, 0, len(decisions))
	for _, decision := range decisions {
		if decision.WorkloadID == "" {
			continue
		}
		items = append(items, handle.Item{
			Selector: decision.WorkloadID,
			RepoID:   decision.workloadRepositoryID,
		})
	}
	resolutions, err := resolver.ResolveAll(ctx, scopeID, generationID, items)
	if err != nil {
		return nil, err
	}
	for i, decision := range decisions {
		if decision.WorkloadID == "" {
			continue
		}
		item := handle.Item{Selector: decision.WorkloadID, RepoID: decision.workloadRepositoryID}
		resolution := resolutions[item.Key()]
		switch resolution.Outcome {
		case handle.OutcomeAmbiguous:
			decisions[i].WorkloadID = ""
		case handle.OutcomeExact, handle.OutcomeUniqueByName:
			decisions[i].WorkloadID = resolution.WorkloadID
		case handle.OutcomeMiss:
			// Kept: the read-side ladder still classifies the raw selector.
		}
	}
	return decisions, nil
}
