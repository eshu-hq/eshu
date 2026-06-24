// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization helpers for the graph-backed infra resource
// aggregate read routes (count, inventory). A scoped token's grant set bounds
// the aggregate to resources attributable to granted repositories via
// infraResourceScopePredicate. Empty-grant scoped tokens return the bounded
// zero/empty shape without reading the graph, so an authenticated-but-ungranted
// token never triggers a whole-graph scan.

// writeEmptyInfraResourceCount returns the zero-count aggregate shape for an
// empty-grant scoped token without reading the graph.
func (h *InfraHandler) writeEmptyInfraResourceCount(w http.ResponseWriter, r *http.Request) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_resources": 0,
		"by_provider":     map[string]int{},
		"by_environment":  map[string]int{},
		"by_label":        map[string]int{},
		"scope":           map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		infraResourceAggregateCapability,
		TruthBasisAuthoritativeGraph,
		"scoped token grants authorize no repositories; aggregate totals are zero",
	))
}

// writeEmptyInfraResourceInventory returns the empty inventory page for an
// empty-grant scoped token without reading the graph.
func (h *InfraHandler) writeEmptyInfraResourceInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension InfraResourceInventoryDimension,
	limit int,
	offset int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":     []InfraResourceInventoryRow{},
		"count":       0,
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   false,
		"next_offset": nil,
		"scope":       map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		infraResourceAggregateCapability,
		TruthBasisAuthoritativeGraph,
		"scoped token grants authorize no repositories; inventory buckets are empty",
	))
}

// applyInfraResourceAggregateAccess copies a scoped-token's granted repository
// and ingestion-scope ids into the filter so the store binds the
// repository-anchored predicate. Shared / admin / local callers leave the
// filter unrestricted.
func applyInfraResourceAggregateAccess(filter InfraResourceAggregateFilter, access repositoryAccessFilter) InfraResourceAggregateFilter {
	if !access.scoped() {
		return filter
	}
	filter.AllowedRepositoryIDs = access.grantedRepositoryIDs()
	filter.AllowedScopeIDs = access.grantedScopeIDs()
	return filter
}
