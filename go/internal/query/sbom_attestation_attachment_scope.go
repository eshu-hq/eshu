// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Scoped-token authorization helpers for the reducer-owned SBOM/attestation
// attachment read routes (list, count, inventory). Empty-grant and out-of-grant
// paths perform no store or repository-selector reads; the grant set overlaps
// each fact's repository_ids and the missing-evidence probe stays grant-bounded.

// resolveSBOMAttachmentRepositorySelector resolves a human repository selector
// under the caller's scoped grants. Out-of-grant selectors return a not-found
// response without reading the attachment or aggregate stores.
func (h *SupplyChainHandler) resolveSBOMAttachmentRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	selector string,
	access repositoryAccessFilter,
	capability string,
) (string, bool) {
	return resolveRepositorySelectorForRequestWithAccess(w, r, h.Neo4j, h.Content, selector, access, capability)
}

// writeEmptySBOMAttachmentPage returns the bounded zero-attachments page for an
// empty-grant scoped token without reading the attachment store.
func (h *SupplyChainHandler) writeEmptySBOMAttachmentPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	body := map[string]any{
		"attachments": []SBOMAttestationAttachmentResult{},
		"count":       0,
		"limit":       limit,
		"truncated":   false,
	}
	attachCollectorListReadiness(r.Context(), body, h.CollectorReadiness, scope.CollectorSBOMAttestation, 0, false)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		sbomAttestationAttachmentsCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; no SBOM or attestation attachments are attributable",
	))
}

// writeEmptySBOMAttachmentCount returns the zero-count aggregate shape for an
// empty-grant scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptySBOMAttachmentCount(w http.ResponseWriter, r *http.Request) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_attachments":    0,
		"by_attachment_status": map[string]int{},
		"by_artifact_kind":     map[string]int{},
		"scope":                map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		sbomAttestationAttachmentAggregateCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; aggregate totals are zero",
	))
}

// writeEmptySBOMAttachmentInventory returns the empty inventory page for an
// empty-grant scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptySBOMAttachmentInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension SBOMAttestationAttachmentInventoryDimension,
	limit int,
	offset int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":     []SBOMAttestationAttachmentInventoryRow{},
		"count":       0,
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   false,
		"next_offset": nil,
		"scope":       map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		sbomAttestationAttachmentAggregateCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; inventory buckets are empty",
	))
}
