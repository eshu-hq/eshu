// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
)

// Scoped-token authorization helpers for the reducer-owned provider
// security-alert reconciliation read routes (list, count, inventory).
//
// Reconciliation facts carry a git repository_id plus provider keys
// (provider_repository_id, scope_id). The provider-scope selector resolution is
// left intact; scoped enforcement is layered on as (a) an empty-grant
// short-circuit, (b) a post-resolution grant check that fails out-of-grant
// repository selectors before the reconciliation store read, and (c) the SQL
// grant predicate that bounds rows for non-repository anchors.

// securityAlertReconciliationOutOfGrant reports whether a scoped token resolved
// a repository selector outside its grants. When it returns true it has already
// written a not-found response, and the caller must not read the reconciliation
// store. The original selector (not the resolved id) is echoed so no
// out-of-grant repository id leaks.
func (h *SupplyChainHandler) securityAlertReconciliationOutOfGrant(
	w http.ResponseWriter,
	r *http.Request,
	access querycontract.RepositoryAccessFilter,
	repositoryID string,
) bool {
	if !access.Scoped() || repositoryID == "" || access.AllowsRepositoryID(repositoryID) {
		return false
	}
	selector := querycontract.QueryParam(r, "repository_id")
	querycontract.WriteError(w, http.StatusNotFound, queryselector.NotFoundError{Selector: selector}.Error())
	return true
}

// writeEmptySecurityAlertReconciliationPage returns the bounded zero-row list
// page for an empty-grant scoped token without reading the reconciliation store.
func (h *SupplyChainHandler) writeEmptySecurityAlertReconciliationPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	results := []SecurityAlertReconciliationResult{}
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"reconciliations": results,
		"count":           0,
		"coverage":        securityAlertCoverageForRows(results),
		"limit":           limit,
		"truncated":       false,
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		SecurityAlertReconciliationsCapability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; no provider alert reconciliations are attributable",
	))
}

// writeEmptySecurityAlertReconciliationCount returns the zero-count aggregate
// shape for an empty-grant scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptySecurityAlertReconciliationCount(w http.ResponseWriter, r *http.Request) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_reconciliations":    0,
		"by_reconciliation_status": map[string]int{},
		"by_provider":              map[string]int{},
		"by_provider_state":        map[string]int{},
		"by_source_freshness":      map[string]int{},
		"coverage":                 securityAlertCoverageFromFreshnessCounts(0, map[string]int{}),
		"scope":                    map[string]string{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		SecurityAlertReconciliationAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; aggregate totals are zero",
	))
}

// writeEmptySecurityAlertReconciliationInventory returns the empty inventory
// page for an empty-grant scoped token without reading the aggregate store.
func (h *SupplyChainHandler) writeEmptySecurityAlertReconciliationInventory(
	w http.ResponseWriter,
	r *http.Request,
	dimension SecurityAlertReconciliationInventoryDimension,
	limit int,
	offset int,
) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"buckets":     []SecurityAlertReconciliationInventoryRow{},
		"count":       0,
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   false,
		"next_offset": nil,
		"scope":       map[string]string{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		SecurityAlertReconciliationAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; inventory buckets are empty",
	))
}

const (
	securityAlertCoverageComplete         = "complete"
	securityAlertCoverageTargetIncomplete = "target_incomplete"
	securityAlertSourceFreshnessPartial   = "partial"
)

type securityAlertReconciliationCoverageSummary struct {
	State          string `json:"state"`
	PartialRows    int    `json:"partial_rows"`
	RowsConsidered int    `json:"rows_considered"`
}

func securityAlertCoverageForRows(
	rows []SecurityAlertReconciliationResult,
) securityAlertReconciliationCoverageSummary {
	partialRows := 0
	for _, row := range rows {
		if strings.EqualFold(row.SourceFreshness, securityAlertSourceFreshnessPartial) {
			partialRows++
		}
	}
	return securityAlertCoverageFromCounts(len(rows), partialRows)
}

func securityAlertCoverageFromFreshnessCounts(
	totalRows int,
	counts map[string]int,
) securityAlertReconciliationCoverageSummary {
	partialRows := 0
	for freshness, count := range counts {
		if strings.EqualFold(freshness, securityAlertSourceFreshnessPartial) {
			partialRows += count
		}
	}
	return securityAlertCoverageFromCounts(totalRows, partialRows)
}

func securityAlertCoverageFromCounts(
	totalRows int,
	partialRows int,
) securityAlertReconciliationCoverageSummary {
	state := securityAlertCoverageComplete
	if partialRows > 0 {
		state = securityAlertCoverageTargetIncomplete
	}
	return securityAlertReconciliationCoverageSummary{
		State:          state,
		PartialRows:    partialRows,
		RowsConsidered: totalRows,
	}
}
