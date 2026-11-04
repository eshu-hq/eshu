// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

const (
	// impactGroupedInventoryReadFailedMessage is the fixed 500 body for a
	// grouped-inventory store failure; the response never carries err.Error()
	// (#7674).
	impactGroupedInventoryReadFailedMessage = "supply-chain impact grouped inventory read failed"
	// impactGroupedInventoryStage names the stage timing event of the grouped
	// read, beside impact_aggregate_count and impact_aggregate_inventory.
	impactGroupedInventoryStage = "impact_aggregate_grouped_inventory"
)

// impactGroupedInventory serves GET /api/v0/supply-chain/impact/inventory for
// the service_id, workload_id, and environment dimensions (#9873). The five
// single-valued dimensions stay on AggregateStore. The grouped read has its
// own port: a missing store, or a store that reports its read model
// unavailable, answers the 503 backend_unavailable envelope and never falls
// back to another read shape.
func (h *Handler) impactGroupedInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension impact.GroupedInventoryDimension,
) {
	if h.ImpactGroupedInventory == nil {
		h.writeImpactAggregateBackendUnavailable(w, r)
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.FindingsScannerFilters()) {
		return
	}
	limit, ok := parseSupplyChainImpactAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseSupplyChainImpactAggregateOffset(w, r)
	if !ok {
		return
	}
	profile, ok := impact.RequestedProfile(w, r)
	if !ok {
		return
	}
	// Empty scoped grants return the empty grouped page without reading the
	// store or resolving a selector: zero buckets and no unattributed row, so
	// a caller cannot probe which services exist outside its grant.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyImpactGroupedInventory(w, r, dimension, limit, offset, profile)
		return
	}
	filter, ok := h.supplyChainImpactAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}

	timer := startSupplyChainQueryStage(r.Context(), h.Logger, supplyChainImpactAggregateOperation, filter.RepositoryID, impactGroupedInventoryStage)
	page, err := h.ImpactGroupedInventory.SupplyChainImpactGroupedInventory(r.Context(), filter, dimension, limit+1, offset)
	timer.Done(r.Context(), slog.Bool("error", err != nil))
	if err != nil {
		if errors.Is(err, impact.ErrGroupedInventoryReadModelUnavailable) {
			h.writeImpactAggregateBackendUnavailable(w, r)
			return
		}
		// #7549: a stale or timed-out guarded PostgreSQL reader answers the
		// retryable 503 envelope before the handler-owned 500.
		if querycontract.WriteGraphReadError(w, r, err, ImpactAggregateCapability) {
			return
		}
		writeStageFailure(w, r, timer, err, impactGroupedInventoryReadFailedMessage)
		return
	}
	rows := page.Rows
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":           normalizeGroupedInventoryRows(rows, dimension, truncated),
		"count":             len(rows),
		"limit":             limit,
		"offset":            offset,
		"group_by":          string(dimension),
		"detection_profile": requestedSupplyChainImpactAggregateProfile(filter),
		"truncated":         truncated,
		"next_offset":       NextImpactAggregateOffset(offset, limit, truncated),
		"distinct_findings": max(page.DistinctFindings, 0),
		"freshness":         normalizeGroupedInventoryFreshness(page.Freshness),
		"scope":             supplyChainImpactAggregateScope(filter),
	}, groupedInventoryTruth(h))
}

// writeImpactAggregateBackendUnavailable answers the shared 503
// backend_unavailable envelope for an aggregate read model that is not wired
// or not enabled.
func (h *Handler) writeImpactAggregateBackendUnavailable(w http.ResponseWriter, r *http.Request) {
	querycontract.WriteContractError(
		w,
		r,
		http.StatusServiceUnavailable,
		"supply-chain impact aggregates require the Postgres reducer read model",
		querycontract.ErrorCodeBackendUnavailable,
		ImpactAggregateCapability,
		h.profile(),
		querycontract.RequiredProfile(ImpactAggregateCapability),
	)
}

func groupedInventoryTruth(h *Handler) *querycontract.TruthEnvelope {
	return querycontract.BuildTruthEnvelope(
		h.profile(),
		ImpactAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved at read time from reducer-owned impact facts and runtime workload evidence; a finding counts in every group it resolves to, so bucket counts can sum above distinct_findings",
	)
}

// normalizeGroupedInventoryRows pins the grouped-bucket invariants the store
// is trusted to meet but the wire must never depend on. The unattributed
// bucket is value "" with unattributed true, completeness unattributed, and a
// missing-evidence reason; it is never complete. A completeness outside the
// closed set reads partial. A truncated page demotes every complete bucket to
// partial. Rows are copied, never mutated in place.
func normalizeGroupedInventoryRows(
	rows []impact.GroupedInventoryRow,
	dimension impact.GroupedInventoryDimension,
	truncated bool,
) []impact.GroupedInventoryRow {
	out := make([]impact.GroupedInventoryRow, 0, len(rows))
	for _, row := range rows {
		row.Dimension = dimension
		row.Value = strings.TrimSpace(row.Value)
		if row.Unattributed || row.Value == "" {
			row.Unattributed = true
			row.Value = ""
			row.Completeness = impact.GroupCompletenessUnattributed
			row.MissingEvidence = withMissingEvidence(row.MissingEvidence, impact.UnattributedMissingEvidence(dimension))
		} else {
			if !row.Completeness.Valid() || row.Completeness == impact.GroupCompletenessUnattributed {
				row.Completeness = impact.GroupCompletenessPartial
			}
			if truncated && row.Completeness == impact.GroupCompletenessComplete {
				row.Completeness = impact.GroupCompletenessPartial
			}
		}
		if row.ByImpactStatus == nil {
			row.ByImpactStatus = map[string]int{}
		}
		if row.BySeverity == nil {
			row.BySeverity = map[string]int{}
		}
		if row.MissingEvidence == nil {
			row.MissingEvidence = []string{}
		}
		out = append(out, row)
	}
	return out
}

// withMissingEvidence returns reasons with reason present exactly once at the
// front, without aliasing the input slice.
func withMissingEvidence(reasons []string, reason string) []string {
	out := make([]string, 0, len(reasons)+1)
	out = append(out, reason)
	for _, existing := range reasons {
		if existing != reason {
			out = append(out, existing)
		}
	}
	return out
}

func normalizeGroupedInventoryFreshness(f impact.GroupedInventoryFreshness) impact.GroupedInventoryFreshness {
	if strings.TrimSpace(f.State) == "" {
		f.State = impact.FreshnessLabelUnknown
	}
	return f
}
