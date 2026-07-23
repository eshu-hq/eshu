// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const securityAlertReconciliationAggregateCapability = "supply_chain.security_alert_reconciliations.aggregate"

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
		securityAlertReconciliationAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), securityAlertReconciliationAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			securityAlertReconciliationAggregateCapability,
			h.profile(),
			requiredProfile(securityAlertReconciliationAggregateCapability),
		)
		return
	}
	if h.SecurityAlertAggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			securityAlertReconciliationAggregateCapability,
			h.profile(),
			requiredProfile(securityAlertReconciliationAggregateCapability),
		)
		return
	}
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, securityAlertScannerFilters()) {
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptySecurityAlertReconciliationCount(w, r)
		return
	}
	filter, ok := h.securityAlertReconciliationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	count, err := h.SecurityAlertAggregates.CountSecurityAlertReconciliations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
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
	}, BuildTruthEnvelope(
		h.profile(),
		securityAlertReconciliationAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned reconciliation facts; provider alert state stays separate from Eshu impact state",
	))
}

func (h *SupplyChainHandler) securityAlertReconciliationInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecurityAlertReconciliationAggregate,
		"GET /api/v0/supply-chain/security-alerts/reconciliations/inventory",
		securityAlertReconciliationAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), securityAlertReconciliationAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			securityAlertReconciliationAggregateCapability,
			h.profile(),
			requiredProfile(securityAlertReconciliationAggregateCapability),
		)
		return
	}
	if h.SecurityAlertAggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"security alert reconciliation aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			securityAlertReconciliationAggregateCapability,
			h.profile(),
			requiredProfile(securityAlertReconciliationAggregateCapability),
		)
		return
	}
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, securityAlertScannerFilters()) {
		return
	}

	dimension := SecurityAlertReconciliationInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = SecurityAlertReconciliationInventoryByStatus
	}
	if !isSupportedSecurityAlertReconciliationDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of reconciliation_status, provider, provider_state, repository_id, package_id")
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
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptySecurityAlertReconciliationInventory(w, r, dimension, limit, offset)
		return
	}
	filter, ok := h.securityAlertReconciliationAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}

	rows, err := h.SecurityAlertAggregates.SecurityAlertReconciliationInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
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
		"next_offset": nextSecurityAlertReconciliationAggregateOffset(offset, limit, truncated),
		"scope":       securityAlertReconciliationAggregateScope(filter),
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		securityAlertReconciliationAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned reconciliation facts; one grouped bucket per row, ordered by count desc",
	))
}

func (h *SupplyChainHandler) securityAlertReconciliationAggregateFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	access repositoryAccessFilter,
) (SecurityAlertReconciliationAggregateFilter, bool) {
	repositoryID, repositoryScopeIDs, ok := h.resolveSupplyChainSecurityAlertRepositorySelector(w, r, QueryParam(r, "repository_id"), securityAlertReconciliationAggregateCapability)
	if !ok {
		return SecurityAlertReconciliationAggregateFilter{}, false
	}
	if h.securityAlertReconciliationOutOfGrant(w, r, access, repositoryID) {
		return SecurityAlertReconciliationAggregateFilter{}, false
	}
	return SecurityAlertReconciliationAggregateFilter{
		RepositoryID:               repositoryID,
		RepositoryScopeIDs:         repositoryScopeIDs,
		Provider:                   QueryParam(r, "provider"),
		PackageID:                  QueryParam(r, "package_id"),
		CVEID:                      QueryParam(r, "cve_id"),
		GHSAID:                     QueryParam(r, "ghsa_id"),
		ProviderState:              QueryParam(r, "provider_state"),
		ReconciliationStatus:       QueryParam(r, "reconciliation_status"),
		AllowedSourceRepositoryIDs: access.repositorySearchIDs(),
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
	raw := QueryParam(r, "limit")
	if raw == "" {
		return securityAlertReconciliationAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < securityAlertReconciliationAggregateMinLimit {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > SecurityAlertReconciliationAggregateMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseSecurityAlertReconciliationAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > securityAlertReconciliationAggregateMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextSecurityAlertReconciliationAggregateOffset returns the next offset when a
// truncated page can be continued without exceeding the documented offset
// bound, and nil otherwise. Callers serialize the nil as JSON null so
// generated clients see a clean end-of-stream marker.
func nextSecurityAlertReconciliationAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > securityAlertReconciliationAggregateMaxOffset {
		return nil
	}
	return next
}
