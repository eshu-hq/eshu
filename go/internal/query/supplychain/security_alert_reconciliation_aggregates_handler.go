// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const SecurityAlertReconciliationAggregateCapability = "supply_chain.security_alert_reconciliations.aggregate"

// securityAlertReconciliationAggregateRoutes registers the cheap-summary
// aggregate routes alongside the existing reconciliation list route. The
// SupplyChainHandler.Mount in supply_chain.go invokes it.
func (h *SupplyChainHandler) securityAlertReconciliationAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/security-alerts/reconciliations/count", h.countSecurityAlertReconciliations)
	mux.HandleFunc("GET /api/v0/supply-chain/security-alerts/reconciliations/inventory", h.securityAlertReconciliationInventory)
}

func (h *SupplyChainHandler) countSecurityAlertReconciliations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecurityAlertReconciliationAggregate,
		"GET /api/v0/supply-chain/security-alerts/reconciliations/count",
		SecurityAlertReconciliationAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SecurityAlertReconciliationAggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SecurityAlertReconciliationAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SecurityAlertReconciliationAggregateCapability),
		)
		return
	}
	if h.SecurityAlertAggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SecurityAlertReconciliationAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SecurityAlertReconciliationAggregateCapability),
		)
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.SecurityAlertScannerFilters()) {
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptySecurityAlertReconciliationCount(w, r)
		return
	}
	filter, ok := h.securityAlertReconciliationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	count, err := h.SecurityAlertAggregates.CountSecurityAlertReconciliations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_reconciliations":    count.TotalReconciliations,
		"by_reconciliation_status": count.ByReconciliationStatus,
		"by_provider":              count.ByProvider,
		"by_provider_state":        count.ByProviderState,
		"by_source_freshness":      count.BySourceFreshness,
		"coverage": securityAlertCoverageFromFreshnessCounts(
			count.TotalReconciliations,
			count.BySourceFreshness,
		),
		"scope": securityAlertReconciliationAggregateScope(filter),
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		SecurityAlertReconciliationAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned reconciliation facts; provider alert state stays separate from Eshu impact state",
	))
}

func (h *SupplyChainHandler) securityAlertReconciliationInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecurityAlertReconciliationAggregate,
		"GET /api/v0/supply-chain/security-alerts/reconciliations/inventory",
		SecurityAlertReconciliationAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SecurityAlertReconciliationAggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SecurityAlertReconciliationAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SecurityAlertReconciliationAggregateCapability),
		)
		return
	}
	if h.SecurityAlertAggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SecurityAlertReconciliationAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SecurityAlertReconciliationAggregateCapability),
		)
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.SecurityAlertScannerFilters()) {
		return
	}

	dimension := SecurityAlertReconciliationInventoryDimension(querycontract.QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = SecurityAlertReconciliationInventoryByStatus
	}
	if !isSupportedSecurityAlertReconciliationDimension(dimension) {
		querycontract.WriteError(w, http.StatusBadRequest, "group_by must be one of reconciliation_status, provider, provider_state, repository_id, package_id")
		return
	}
	limit, ok := parseSecurityAlertReconciliationAggregateLimit(w, r)
	if !ok {
		return
	}
	offset, ok := parseSecurityAlertReconciliationAggregateOffset(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptySecurityAlertReconciliationInventory(w, r, dimension, limit, offset)
		return
	}
	filter, ok := h.securityAlertReconciliationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}

	rows, err := h.SecurityAlertAggregates.SecurityAlertReconciliationInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"buckets":     rows,
		"count":       len(rows),
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   truncated,
		"next_offset": NextSecurityAlertReconciliationAggregateOffset(offset, limit, truncated),
		"scope":       securityAlertReconciliationAggregateScope(filter),
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		SecurityAlertReconciliationAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned reconciliation facts; one grouped bucket per row, ordered by count desc",
	))
}

func (h *SupplyChainHandler) securityAlertReconciliationAggregateFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	access querycontract.RepositoryAccessFilter,
) (SecurityAlertReconciliationAggregateFilter, bool) {
	repositoryID, repositoryScopeIDs, ok := h.resolveSupplyChainSecurityAlertRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), SecurityAlertReconciliationAggregateCapability)
	if !ok {
		return SecurityAlertReconciliationAggregateFilter{}, false
	}
	if h.securityAlertReconciliationOutOfGrant(w, r, access, repositoryID) {
		return SecurityAlertReconciliationAggregateFilter{}, false
	}
	return SecurityAlertReconciliationAggregateFilter{
		RepositoryID:               repositoryID,
		RepositoryScopeIDs:         repositoryScopeIDs,
		Provider:                   querycontract.QueryParam(r, "provider"),
		PackageID:                  querycontract.QueryParam(r, "package_id"),
		CVEID:                      querycontract.QueryParam(r, "cve_id"),
		GHSAID:                     querycontract.QueryParam(r, "ghsa_id"),
		ProviderState:              querycontract.QueryParam(r, "provider_state"),
		ReconciliationStatus:       querycontract.QueryParam(r, "reconciliation_status"),
		AllowedSourceRepositoryIDs: access.RepositorySearchIDs(),
	}, true
}

func securityAlertReconciliationAggregateScope(filter SecurityAlertReconciliationAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.RepositoryID != "" {
		out["repository_id"] = filter.RepositoryID
	}
	if filter.Provider != "" {
		out["provider"] = filter.Provider
	}
	if filter.PackageID != "" {
		out["package_id"] = filter.PackageID
	}
	if filter.CVEID != "" {
		out["cve_id"] = filter.CVEID
	}
	if filter.GHSAID != "" {
		out["ghsa_id"] = filter.GHSAID
	}
	if filter.ProviderState != "" {
		out["provider_state"] = filter.ProviderState
	}
	if filter.ReconciliationStatus != "" {
		out["reconciliation_status"] = filter.ReconciliationStatus
	}
	return out
}

func isSupportedSecurityAlertReconciliationDimension(d SecurityAlertReconciliationInventoryDimension) bool {
	switch d {
	case SecurityAlertReconciliationInventoryByStatus,
		SecurityAlertReconciliationInventoryByProvider,
		SecurityAlertReconciliationInventoryByProviderState,
		SecurityAlertReconciliationInventoryByRepository,
		SecurityAlertReconciliationInventoryByPackage:
		return true
	default:
		return false
	}
}

const (
	securityAlertReconciliationAggregateDefaultLimit = 100
	securityAlertReconciliationAggregateMinLimit     = 1
	// securityAlertReconciliationAggregateMaxOffset matches the OpenAPI offset
	// bound and keeps Postgres OFFSET scans bounded. Past this point callers
	// should narrow scope (provider, repository, package) or fall back to the
	// list endpoint with anchored pagination.
	securityAlertReconciliationAggregateMaxOffset = 10000
)

func parseSecurityAlertReconciliationAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return securityAlertReconciliationAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < securityAlertReconciliationAggregateMinLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > SecurityAlertReconciliationAggregateMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseSecurityAlertReconciliationAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > securityAlertReconciliationAggregateMaxOffset {
		querycontract.WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// NextSecurityAlertReconciliationAggregateOffset returns the next offset when a
// truncated page can be continued without exceeding the documented offset
// bound, and nil otherwise. Callers serialize the nil as JSON null so
// generated clients see a clean end-of-stream marker.
func NextSecurityAlertReconciliationAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > securityAlertReconciliationAggregateMaxOffset {
		return nil
	}
	return next
}
