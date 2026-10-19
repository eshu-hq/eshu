// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

// suppressionWorkloadScopeMatches reports whether the scope's workload
// dimension matches the finding's workload evidence. A scope the ladder
// left unresolved — a handle spanning more than one repository — fails
// closed to no-match: a suppression must never silence findings in another
// tenant. Every other scope keeps the legacy list match.
func suppressionWorkloadScopeMatches(scope vulnerabilitySuppressionScope, workloadIDs []string) bool {
	if scope.WorkloadUnresolved {
		return false
	}
	return scopeListAnchorMatches(scope.WorkloadID, workloadIDs)
}

// resolveSuppressionWorkloads runs the suppression workload scopes through
// the handle ladder before evaluation: a spanning handle applies to
// neither tenant. A nil WorkloadLookup keeps the legacy scopes. A lookup
// failure fails the intent closed for retry.
func (h SupplyChainImpactHandler) resolveSuppressionWorkloads(
	ctx context.Context,
	intent reducercontract.Intent,
	suppressions []vulnerabilitySuppression,
) ([]vulnerabilitySuppression, error) {
	if h.WorkloadLookup == nil {
		return suppressions, nil
	}
	return ResolveSuppressionWorkloadScopes(ctx, suppressions, intent.ScopeID, intent.GenerationID, &handle.Resolver{
		ScopeRepos:  h.ScopeRepos,
		Lookup:      h.WorkloadLookup,
		Instruments: h.Instruments,
	})
}

// ResolveSuppressionWorkloadScopes resolves every suppression's
// Scope.WorkloadID through the handle ladder before evaluation,
// restricted by the scope's workload_repository_id (else its
// repository_id anchor) or the scope's repositories.
//
// A full id or unique handle rewrites the scope to the resolved node id.
// A handle spanning two repositories marks the scope WorkloadUnresolved —
// it applies to neither tenant — and is counted with the alertable
// structured log, never silent. A miss keeps the raw selector. A nil
// resolver keeps every scope (the unwired-handler legacy path).
func ResolveSuppressionWorkloadScopes(
	ctx context.Context,
	suppressions []vulnerabilitySuppression,
	scopeID string,
	generationID string,
	resolver *handle.Resolver,
) ([]vulnerabilitySuppression, error) {
	if len(suppressions) == 0 {
		return suppressions, nil
	}
	if resolver == nil || resolver.Lookup == nil {
		return suppressions, nil
	}
	items := make([]handle.Item, 0, len(suppressions))
	for _, suppression := range suppressions {
		if strings.TrimSpace(suppression.Scope.WorkloadID) == "" {
			continue
		}
		repoID := suppression.Scope.WorkloadRepositoryID
		if strings.TrimSpace(repoID) == "" {
			repoID = suppression.Scope.RepositoryID
		}
		items = append(items, handle.Item{
			Selector: strings.TrimSpace(suppression.Scope.WorkloadID),
			RepoID:   strings.TrimSpace(repoID),
		})
	}
	resolutions, err := resolver.ResolveAll(ctx, scopeID, generationID, items)
	if err != nil {
		return nil, err
	}
	for i := range suppressions {
		selector := strings.TrimSpace(suppressions[i].Scope.WorkloadID)
		if selector == "" {
			continue
		}
		repoID := suppressions[i].Scope.WorkloadRepositoryID
		if strings.TrimSpace(repoID) == "" {
			repoID = suppressions[i].Scope.RepositoryID
		}
		item := handle.Item{Selector: selector, RepoID: strings.TrimSpace(repoID)}
		switch resolutions[item.Key()].Outcome {
		case handle.OutcomeAmbiguous:
			suppressions[i].Scope.WorkloadUnresolved = true
		case handle.OutcomeExact, handle.OutcomeUniqueByName:
			suppressions[i].Scope.WorkloadID = resolutions[item.Key()].WorkloadID
		case handle.OutcomeMiss:
			// Kept: the raw selector fails closed against resolved finding ids.
		}
	}
	return suppressions, nil
}
