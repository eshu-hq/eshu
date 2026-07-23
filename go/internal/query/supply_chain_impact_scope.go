// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization helpers for the reducer-owned vulnerability impact
// read routes (findings list, count, and inventory). These keep the empty-grant
// and out-of-grant paths free of impact, readiness, and repository-selector
// store reads so a scoped caller never probes cross-tenant evidence.

// resolveSupplyChainImpactRepositorySelector resolves a human repository
// selector under the caller's scoped grants. Out-of-grant selectors return a
// not-found response without reading the reducer impact or readiness stores,
// so a scoped caller cannot probe whether an unauthorized repository exists.
func (h *SupplyChainHandler) resolveSupplyChainImpactRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	selector string,
	access repositoryAccessFilter,
	capability string,
) (string, bool) {
	return resolveRepositorySelectorForRequestWithAccess(w, r, h.Neo4j, h.Content, selector, access, capability)
}

// writeEmptyImpactFindingsPage returns the bounded zero-findings page used when
// a scoped token grants no repositories. It mirrors the populated response
// shape but performs no store reads, and reports readiness as unavailable so a
// caller cannot misread zero findings as a clean "no vulnerabilities" answer.
func (h *SupplyChainHandler) writeEmptyImpactFindingsPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
	profile string,
) {
	body := map[string]any{
		"findings":          []SupplyChainImpactFindingResult{},
		"count":             0,
		"limit":             limit,
		"truncated":         false,
		"detection_profile": profile,
		"readiness":         BuildSupplyChainImpactReadinessUnavailable(SupplyChainImpactTargetScope{}, nil, false),
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactFindingsCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; readiness cannot be classified without an authorized scope",
	))
}

// writeEmptyImpactExplanation returns the bounded no-evidence explanation
// shape used when a scoped token grants no repositories, without reading the
// reducer impact-explanation or readiness stores (#5167 W5). It mirrors
// writeEmptyImpactFindingsPage: the response shape matches a real "no
// evidence" outcome so a caller cannot distinguish an empty grant from a
// scope that legitimately has no findings, and readiness is reported
// unavailable rather than falsely clean.
func (h *SupplyChainHandler) writeEmptyImpactExplanation(w http.ResponseWriter, r *http.Request) {
	filter := SupplyChainImpactExplanationFilter{}
	readiness := BuildSupplyChainImpactReadinessUnavailable(SupplyChainImpactTargetScope{}, nil, false)
	body := BuildSupplyChainImpactNoEvidenceExplanation(filter, readiness)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactExplanationCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; readiness cannot be classified without an authorized scope",
	))
}

// writeEmptyImpactCount returns the zero-count aggregate shape for an
// empty-grant scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptyImpactCount(w http.ResponseWriter, r *http.Request) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_findings":     0,
		"affected_findings":  0,
		"affected_exact":     0,
		"affected_derived":   0,
		"possibly_affected":  0,
		"not_affected":       0,
		"by_priority_bucket": map[string]int{},
		"by_severity":        map[string]int{},
		"detection_profile":  SupplyChainImpactProfileComprehensive,
		"scope":              map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactAggregateCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; aggregate totals are zero",
	))
}

// writeEmptyImpactInventory returns the empty inventory page for an empty-grant
// scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptyImpactInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension SupplyChainImpactInventoryDimension,
	limit int,
	offset int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":           []SupplyChainImpactInventoryRow{},
		"count":             0,
		"limit":             limit,
		"offset":            offset,
		"group_by":          string(dimension),
		"detection_profile": SupplyChainImpactProfileComprehensive,
		"truncated":         false,
		"next_offset":       nil,
		"scope":             map[string]string{},
	}, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactAggregateCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; inventory buckets are empty",
	))
}
