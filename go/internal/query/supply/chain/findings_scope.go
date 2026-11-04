// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// Scoped-token authorization helpers for the reducer-owned vulnerability impact
// read routes (findings list, count, and inventory). These keep the empty-grant
// and out-of-grant paths free of impact, readiness, and repository-selector
// store reads so a scoped caller never probes cross-tenant evidence.

// resolveSupplyChainImpactRepositorySelector resolves a human repository
// selector under the caller's scoped grants. Out-of-grant selectors return a
// not-found response without reading the reducer impact or readiness stores,
// so a scoped caller cannot probe whether an unauthorized repository exists.
func (h *Handler) resolveSupplyChainImpactRepositorySelector(
	w http.ResponseWriter,
	r *http.Request,
	rawSelector string,
	access querycontract.RepositoryAccessFilter,
	capability string,
) (string, bool) {
	return selector.ResolveForRequestWithAccess(w, r, h.Neo4j, h.Content, rawSelector, access, capability)
}

// writeEmptyImpactFindingsPage returns the bounded zero-findings page used when
// a scoped token grants no repositories. It mirrors the populated response
// shape but performs no store reads, and reports readiness as unavailable so a
// caller cannot misread zero findings as a clean "no vulnerabilities" answer.
func (h *Handler) writeEmptyImpactFindingsPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
	profile string,
) {
	body := map[string]any{
		"findings":          []impact.FindingResult{},
		"count":             0,
		"limit":             limit,
		"truncated":         false,
		"detection_profile": profile,
		"readiness":         impact.BuildReadinessUnavailable(impact.TargetScope{}, nil, false),
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		ImpactFindingsCapability,
		querycontract.TruthBasisSemanticFacts,
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
func (h *Handler) writeEmptyImpactExplanation(w http.ResponseWriter, r *http.Request) {
	filter := impact.ExplanationFilter{}
	readiness := impact.BuildReadinessUnavailable(impact.TargetScope{}, nil, false)
	body := impact.BuildNoEvidenceExplanation(filter, readiness)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		ImpactExplanationCapability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; readiness cannot be classified without an authorized scope",
	))
}

// writeEmptyImpactCount returns the zero-count aggregate shape for an
// empty-grant scoped token without reading the aggregate store. profile is the
// detection profile the request actually selected (precise unless the caller
// asked for comprehensive); echoing a fixed value would misreport a default
// request.
func (h *Handler) writeEmptyImpactCount(w http.ResponseWriter, r *http.Request, profile string) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_findings":     0,
		"affected_findings":  0,
		"affected_exact":     0,
		"affected_derived":   0,
		"possibly_affected":  0,
		"not_affected":       0,
		"by_priority_bucket": map[string]int{},
		"by_severity":        map[string]int{},
		"detection_profile":  profile,
		"scope":              map[string]string{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		ImpactAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; aggregate totals are zero",
	))
}

// writeEmptyImpactInventory returns the empty inventory page for an empty-grant
// scoped token without reading the aggregate store. profile is the detection
// profile the request actually selected.
func (h *Handler) writeEmptyImpactInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension impact.InventoryDimension,
	limit int,
	offset int,
	profile string,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":           []impact.InventoryRow{},
		"count":             0,
		"limit":             limit,
		"offset":            offset,
		"group_by":          string(dimension),
		"detection_profile": profile,
		"truncated":         false,
		"next_offset":       nil,
		"scope":             map[string]string{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		ImpactAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; inventory buckets are empty",
	))
}

// writeEmptyImpactGroupedInventory returns the empty grouped-inventory page for
// an empty-grant scoped token without reading the grouped store: zero buckets,
// no unattributed row, and a zero distinct_findings, so the response cannot
// reveal which services, workloads, or environments exist outside the grant.
func (h *Handler) writeEmptyImpactGroupedInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension impact.GroupedInventoryDimension,
	limit int,
	offset int,
	profile string,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":           []impact.GroupedInventoryRow{},
		"count":             0,
		"limit":             limit,
		"offset":            offset,
		"group_by":          string(dimension),
		"detection_profile": profile,
		"truncated":         false,
		"next_offset":       nil,
		"distinct_findings": 0,
		"freshness":         impact.GroupedInventoryFreshness{State: impact.FreshnessLabelUnknown},
		"scope":             map[string]string{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		ImpactAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; grouped inventory buckets are empty",
	))
}
