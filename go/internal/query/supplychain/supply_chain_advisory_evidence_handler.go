// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/advisory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) listAdvisoryEvidence(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdvisoryEvidence,
		"GET /api/v0/supply-chain/advisories/evidence",
		advisory.AdvisoryEvidenceCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), advisory.AdvisoryEvidenceCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"advisory evidence requires the Postgres vulnerability source fact read model",
			querycontract.ErrorCodeUnsupportedCapability,
			advisory.AdvisoryEvidenceCapability,
			h.profile(),
			querycontract.RequiredProfile(advisory.AdvisoryEvidenceCapability),
		)
		return
	}
	limit, ok := requiredAdvisoryEvidenceLimit(w, r)
	if !ok {
		return
	}
	// Advisory evidence facts are global CVE/advisory data with no repository
	// of their own, so the bare cve_id/advisory_id/package_id path is public.
	// The repository-anchored path resolves under scoped grants: an out-of-grant
	// repository selector fails as not-found, and the grant set is intersected
	// with the impact findings that derive advisory anchors so a scoped caller
	// only learns advisories affecting its own repositories.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	repositoryID, ok := queryselector.ResolveForRequestWithAccess(w, r, h.Neo4j, h.Content, querycontract.QueryParam(r, "repository_id"), access, advisory.AdvisoryEvidenceCapability)
	if !ok {
		return
	}
	filter := advisory.NormalizeAdvisoryEvidenceFilter(advisory.AdvisoryEvidenceFilter{
		CVEID:                      querycontract.QueryParam(r, "cve_id"),
		AdvisoryID:                 querycontract.QueryParam(r, "advisory_id"),
		PackageID:                  querycontract.QueryParam(r, "package_id"),
		RepositoryID:               repositoryID,
		ServiceID:                  querycontract.QueryParam(r, "service_id"),
		WorkloadID:                 querycontract.QueryParam(r, "workload_id"),
		Source:                     querycontract.QueryParam(r, "source"),
		AfterAdvisoryKey:           querycontract.QueryParam(r, "after_advisory_key"),
		Limit:                      limit + 1,
		AllowedSourceRepositoryIDs: access.RepositorySearchIDs(),
	})
	if !filter.HasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, package_id, repository_id, service_id, or workload_id is required")
		return
	}
	if h.AdvisoryEvidence == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"advisory evidence requires the Postgres vulnerability source fact read model",
			querycontract.ErrorCodeBackendUnavailable,
			advisory.AdvisoryEvidenceCapability,
			h.profile(),
			querycontract.RequiredProfile(advisory.AdvisoryEvidenceCapability),
		)
		return
	}
	rows, err := h.AdvisoryEvidence.ListAdvisoryEvidence(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"advisories": rows,
		"count":      len(rows),
		"limit":      limit,
		"scope":      advisoryEvidenceResponseScope(filter),
		"truncated":  truncated,
	}
	if truncated && len(rows) > 0 {
		body["next_cursor"] = map[string]string{"after_advisory_key": rows[len(rows)-1].AdvisoryKey}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		advisory.AdvisoryEvidenceCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from active vulnerability source facts; repository, service, and workload scopes use reducer-owned impact findings only as bounded advisory anchors and do not imply additional package, image, workload, or deployment impact",
	))
}

func advisoryEvidenceResponseScope(filter advisory.AdvisoryEvidenceFilter) map[string]string {
	filter = advisory.NormalizeAdvisoryEvidenceFilter(filter)
	scope := make(map[string]string, 6)
	if filter.CVEID != "" {
		scope["cve_id"] = filter.CVEID
	}
	if filter.AdvisoryID != "" {
		scope["advisory_id"] = filter.AdvisoryID
	}
	if filter.PackageID != "" {
		scope["package_id"] = filter.PackageID
	}
	if filter.RepositoryID != "" {
		scope["repository_id"] = filter.RepositoryID
	}
	if filter.ServiceID != "" {
		scope["service_id"] = filter.ServiceID
	}
	if filter.WorkloadID != "" {
		scope["workload_id"] = filter.WorkloadID
	}
	return scope
}

func requiredAdvisoryEvidenceLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > advisory.AdvisoryEvidenceMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", advisory.AdvisoryEvidenceMaxLimit))
		return 0, false
	}
	return limit, true
}
