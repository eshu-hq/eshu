// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) listSecurityAlertReconciliations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainSecurityAlerts,
		"GET /api/v0/supply-chain/security-alerts/reconciliations",
		securityAlertReconciliationsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), securityAlertReconciliationsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"provider security alert reconciliations require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			securityAlertReconciliationsCapability,
			h.profile(),
			requiredProfile(securityAlertReconciliationsCapability),
		)
		return
	}
	limit, ok := requiredSecurityAlertReconciliationLimit(w, r)
	if !ok {
		return
	}
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, securityAlertScannerFilters()) {
		return
	}
	// Empty scoped grants return the zero-row page without resolving a selector
	// or reading the reconciliation store.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptySecurityAlertReconciliationPage(w, r, limit)
		return
	}
	repositoryID, repositoryScopeIDs, ok := h.resolveSupplyChainSecurityAlertRepositorySelector(w, r, QueryParam(r, "repository_id"), securityAlertReconciliationsCapability)
	if !ok {
		return
	}
	if h.securityAlertReconciliationOutOfGrant(w, r, access, repositoryID) {
		return
	}
	filter := SecurityAlertReconciliationFilter{
		RepositoryID:               repositoryID,
		RepositoryScopeIDs:         repositoryScopeIDs,
		Provider:                   QueryParam(r, "provider"),
		PackageID:                  QueryParam(r, "package_id"),
		CVEID:                      QueryParam(r, "cve_id"),
		GHSAID:                     QueryParam(r, "ghsa_id"),
		ProviderState:              QueryParam(r, "provider_state"),
		ReconciliationStatus:       QueryParam(r, "reconciliation_status"),
		AfterReconciliationID:      QueryParam(r, "after_reconciliation_id"),
		Limit:                      limit + 1,
		AllowedSourceRepositoryIDs: access.repositorySearchIDs(),
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, securityAlertReconciliationAnchorRequiredMessage)
		return
	}
	if h.SecurityAlerts == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"provider security alert reconciliations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			securityAlertReconciliationsCapability,
			h.profile(),
			requiredProfile(securityAlertReconciliationsCapability),
		)
		return
	}

	rows, err := h.SecurityAlerts.ListSecurityAlertReconciliations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		securityAlertReconciliationsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned provider alert reconciliation facts; provider alert state and Eshu impact state remain separate",
	))
}

func requiredSecurityAlertReconciliationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > securityAlertReconciliationMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", securityAlertReconciliationMaxLimit))
		return 0, false
	}
	return limit, true
}
