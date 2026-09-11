// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) listSecurityAlertReconciliations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainSecurityAlerts,
		"GET /api/v0/supply-chain/security-alerts/reconciliations",
		SecurityAlertReconciliationsCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SecurityAlertReconciliationsCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"provider security alert reconciliations require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SecurityAlertReconciliationsCapability,
			h.profile(),
			querycontract.RequiredProfile(SecurityAlertReconciliationsCapability),
		)
		return
	}
	limit, ok := requiredSecurityAlertReconciliationLimit(w, r)
	if !ok {
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.SecurityAlertScannerFilters()) {
		return
	}
	// Empty scoped grants return the zero-row page without resolving a selector
	// or reading the reconciliation store.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptySecurityAlertReconciliationPage(w, r, limit)
		return
	}
	repositoryID, repositoryScopeIDs, ok := h.resolveSupplyChainSecurityAlertRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), SecurityAlertReconciliationsCapability)
	if !ok {
		return
	}
	if h.securityAlertReconciliationOutOfGrant(w, r, access, repositoryID) {
		return
	}
	filter := SecurityAlertReconciliationFilter{
		RepositoryID:               repositoryID,
		RepositoryScopeIDs:         repositoryScopeIDs,
		Provider:                   querycontract.QueryParam(r, "provider"),
		PackageID:                  querycontract.QueryParam(r, "package_id"),
		CVEID:                      querycontract.QueryParam(r, "cve_id"),
		GHSAID:                     querycontract.QueryParam(r, "ghsa_id"),
		ProviderState:              querycontract.QueryParam(r, "provider_state"),
		ReconciliationStatus:       querycontract.QueryParam(r, "reconciliation_status"),
		AfterReconciliationID:      querycontract.QueryParam(r, "after_reconciliation_id"),
		Limit:                      limit + 1,
		AllowedSourceRepositoryIDs: access.RepositorySearchIDs(),
	}
	if !filter.HasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, SecurityAlertReconciliationAnchorRequiredMessage)
		return
	}
	if h.SecurityAlerts == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"provider security alert reconciliations require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SecurityAlertReconciliationsCapability,
			h.profile(),
			querycontract.RequiredProfile(SecurityAlertReconciliationsCapability),
		)
		return
	}

	rows, err := h.SecurityAlerts.ListSecurityAlertReconciliations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SecurityAlertReconciliationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SecurityAlertReconciliationResult(row))
	}
	body := map[string]any{
		"reconciliations": results,
		"count":           len(results),
		"coverage":        securityAlertCoverageForRows(results),
		"limit":           limit,
		"truncated":       truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_reconciliation_id": results[len(results)-1].ReconciliationID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		SecurityAlertReconciliationsCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned provider alert reconciliation facts; provider alert state and Eshu impact state remain separate",
	))
}

func requiredSecurityAlertReconciliationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > SecurityAlertReconciliationMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", SecurityAlertReconciliationMaxLimit))
		return 0, false
	}
	return limit, true
}
