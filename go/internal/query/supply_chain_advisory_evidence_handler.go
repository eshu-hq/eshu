// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) listAdvisoryEvidence(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdvisoryEvidence,
		"GET /api/v0/supply-chain/advisories/evidence",
		advisoryEvidenceCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), advisoryEvidenceCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"advisory evidence requires the Postgres vulnerability source fact read model",
			ErrorCodeUnsupportedCapability,
			advisoryEvidenceCapability,
			h.profile(),
			requiredProfile(advisoryEvidenceCapability),
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
	access := repositoryAccessFilterFromContext(r.Context())
	repositoryID, ok := resolveRepositorySelectorForRequestWithAccess(w, r, h.Neo4j, h.Content, QueryParam(r, "repository_id"), access, advisoryEvidenceCapability)
	if !ok {
		return
	}
	filter := normalizeAdvisoryEvidenceFilter(AdvisoryEvidenceFilter{
		CVEID:                      QueryParam(r, "cve_id"),
		AdvisoryID:                 QueryParam(r, "advisory_id"),
		PackageID:                  QueryParam(r, "package_id"),
		RepositoryID:               repositoryID,
		ServiceID:                  QueryParam(r, "service_id"),
		WorkloadID:                 QueryParam(r, "workload_id"),
		Source:                     QueryParam(r, "source"),
		AfterAdvisoryKey:           QueryParam(r, "after_advisory_key"),
		Limit:                      limit + 1,
		AllowedSourceRepositoryIDs: access.repositorySearchIDs(),
	})
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, package_id, repository_id, service_id, or workload_id is required")
		return
	}
	if h.AdvisoryEvidence == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"advisory evidence requires the Postgres vulnerability source fact read model",
			ErrorCodeBackendUnavailable,
			advisoryEvidenceCapability,
			h.profile(),
			requiredProfile(advisoryEvidenceCapability),
		)
		return
	}
	rows, err := h.AdvisoryEvidence.ListAdvisoryEvidence(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		advisoryEvidenceCapability,
		TruthBasisSemanticFacts,
		"resolved from active vulnerability source facts; repository, service, and workload scopes use reducer-owned impact findings only as bounded advisory anchors and do not imply additional package, image, workload, or deployment impact",
	))
}

func advisoryEvidenceResponseScope(filter AdvisoryEvidenceFilter) map[string]string {
	filter = normalizeAdvisoryEvidenceFilter(filter)
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
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > advisoryEvidenceMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", advisoryEvidenceMaxLimit))
		return 0, false
	}
	return limit, true
}
