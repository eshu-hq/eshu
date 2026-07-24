// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Scoped-token authorization helpers for the reducer-owned container image
// identity read routes (list, count, inventory). Empty-grant and out-of-grant
// paths perform no store or repository-selector reads, and the grant set is the
// union of granted repository and ingestion-scope ids that the SQL overlaps
// against each fact's source_repository_ids.

// resolveContainerImageSourceRepositorySelector resolves a human source
// repository selector under the caller's scoped grants. Out-of-grant selectors
// return a not-found response without reading the identity or aggregate stores.
func (h *SupplyChainHandler) resolveContainerImageSourceRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	selector string,
	access repositoryAccessFilter,
	capability string,
) (string, bool) {
	return resolveRepositorySelectorForRequestWithAccess(w, r, h.Neo4j, h.Content, selector, access, capability)
}

// writeEmptyContainerImageIdentityPage returns the bounded zero-identities page
// for an empty-grant scoped token without reading the identity store.
func (h *SupplyChainHandler) writeEmptyContainerImageIdentityPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	body := map[string]any{
		"identities": []ContainerImageIdentityResult{},
		"count":      0,
		"limit":      limit,
		"truncated":  false,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorOCIRegistry, 0, false)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		containerImageIdentitiesCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; no container image identities are attributable",
	))
}

// writeEmptyContainerImageIdentityCount returns the zero-count aggregate shape
// for an empty-grant scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptyContainerImageIdentityCount(w http.ResponseWriter, r *http.Request) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_identities":     0,
		"by_outcome":           map[string]int{},
		"by_identity_strength": map[string]int{},
		"scope":                map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		containerImageIdentityAggregateCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; aggregate totals are zero",
	))
}

// writeEmptyContainerImageIdentityInventory returns the empty inventory page for
// an empty-grant scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptyContainerImageIdentityInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension ContainerImageIdentityInventoryDimension,
	limit int,
	offset int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":     []ContainerImageIdentityInventoryRow{},
		"count":       0,
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   false,
		"next_offset": nil,
		"scope":       map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		containerImageIdentityAggregateCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; inventory buckets are empty",
	))
}
