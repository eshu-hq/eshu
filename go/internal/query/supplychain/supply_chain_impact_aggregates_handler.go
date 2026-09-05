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

const SupplyChainImpactAggregateCapability = "supply_chain.impact_findings.aggregate"

// supplyChainImpactAggregateRoutes registers the cheap-summary aggregate routes
// alongside the existing impact findings list route. Mount is the file-local
// installer; the SupplyChainHandler.Mount in supply_chain.go invokes it.
func (h *SupplyChainHandler) supplyChainImpactAggregateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings/count", h.countImpactFindings)
	mux.HandleFunc("GET /api/v0/supply-chain/impact/inventory", h.impactInventory)
}

func (h *SupplyChainHandler) countImpactFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactAggregate,
		"GET /api/v0/supply-chain/impact/findings/count",
		SupplyChainImpactAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SupplyChainImpactAggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SupplyChainImpactAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactAggregateCapability),
		)
		return
	}
	if h.ImpactAggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SupplyChainImpactAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactAggregateCapability),
		)
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.ImpactFindingsScannerFilters()) {
		return
	}

	// Empty scoped grants return the zero-count shape without reading the
	// aggregate store or resolving a repository selector.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyImpactCount(w, r)
		return
	}
	filter, ok := h.supplyChainImpactAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	profile := requestedSupplyChainImpactAggregateProfile(filter)

	count, err := h.ImpactAggregates.CountSupplyChainImpactFindings(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"total_findings":     count.TotalFindings,
		"affected_findings":  count.AffectedFindings,
		"affected_exact":     count.AffectedExact,
		"affected_derived":   count.AffectedDerived,
		"possibly_affected":  count.PossiblyAffected,
		"not_affected":       count.NotAffected,
		"by_priority_bucket": count.ByPriorityBucket,
		"by_severity":        count.BySeverity,
		"detection_profile":  profile,
		"scope":              supplyChainImpactAggregateScope(filter),
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		SupplyChainImpactAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned impact facts; severity buckets derived from CVSS score",
	))
}

func (h *SupplyChainHandler) impactInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactAggregate,
		"GET /api/v0/supply-chain/impact/inventory",
		SupplyChainImpactAggregateCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SupplyChainImpactAggregateCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SupplyChainImpactAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactAggregateCapability),
		)
		return
	}
	if h.ImpactAggregates == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact aggregates require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SupplyChainImpactAggregateCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactAggregateCapability),
		)
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.ImpactFindingsScannerFilters()) {
		return
	}

	dimension := impact.SupplyChainImpactInventoryDimension(querycontract.QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = impact.SupplyChainImpactInventoryByImpactStatus
	}
	if !isSupportedSupplyChainImpactDimension(dimension) {
		querycontract.WriteError(w, http.StatusBadRequest, "group_by must be one of impact_status, priority_bucket, severity, repository_id, ecosystem")
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
	// Empty scoped grants return the empty inventory page without reading the
	// aggregate store or resolving a repository selector.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyImpactInventory(w, r, dimension, limit, offset)
		return
	}
	filter, ok := h.supplyChainImpactAggregateFilterFromRequest(w, r, access)
	if !ok {
		return
	}
	profile := requestedSupplyChainImpactAggregateProfile(filter)

	rows, err := h.ImpactAggregates.SupplyChainImpactInventory(r.Context(), filter, dimension, limit+1, offset)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"buckets":           rows,
		"count":             len(rows),
		"limit":             limit,
		"offset":            offset,
		"group_by":          string(dimension),
		"detection_profile": profile,
		"truncated":         truncated,
		"next_offset":       NextSupplyChainImpactAggregateOffset(offset, limit, truncated),
		"scope":             supplyChainImpactAggregateScope(filter),
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		SupplyChainImpactAggregateCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned impact facts; one grouped bucket per row, ordered by count desc",
	))
}

func (h *SupplyChainHandler) supplyChainImpactAggregateFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	access querycontract.RepositoryAccessFilter,
) (impact.SupplyChainImpactAggregateFilter, bool) {
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), access, SupplyChainImpactAggregateCapability)
	if !ok {
		return impact.SupplyChainImpactAggregateFilter{}, false
	}
	profile, ok := impact.RequestedSupplyChainImpactProfile(w, r)
	if !ok {
		return impact.SupplyChainImpactAggregateFilter{}, false
	}
	advisoryID := querycontract.QueryParam(r, "advisory_id")
	if advisoryID == "" {
		advisoryID = impact.FirstNonEmptyQueryParam(r, "ghsa_id", "osv_id")
	}
	severity, ok := impact.ParseSupplyChainScannerSeverity(w, r)
	if !ok {
		return impact.SupplyChainImpactAggregateFilter{}, false
	}
	priorityBucket := querycontract.QueryParam(r, "priority_bucket")
	if priorityBucket != "" && !impact.ValidSupplyChainImpactPriorityBucket(priorityBucket) {
		querycontract.WriteError(w, http.StatusBadRequest, "priority_bucket must be critical, high, medium, low, or informational")
		return impact.SupplyChainImpactAggregateFilter{}, false
	}
	minPriorityScore, err := impact.OptionalSupplyChainImpactMinPriorityScore(r)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return impact.SupplyChainImpactAggregateFilter{}, false
	}
	suppressionState := querycontract.QueryParam(r, "suppression_state")
	if suppressionState != "" && !impact.IsSupportedSupplyChainSuppressionState(suppressionState) {
		querycontract.WriteError(w, http.StatusBadRequest, "suppression_state must be one of active, not_affected, accepted_risk, false_positive, ignored, expired, provider_dismissed, scope_mismatch")
		return impact.SupplyChainImpactAggregateFilter{}, false
	}
	includeSuppressed, ok := impact.ParseSupplyChainImpactIncludeSuppressed(w, r)
	if !ok {
		return impact.SupplyChainImpactAggregateFilter{}, false
	}
	filter := impact.SupplyChainImpactAggregateFilter{
		CVEID:             querycontract.QueryParam(r, "cve_id"),
		AdvisoryID:        advisoryID,
		PackageID:         querycontract.QueryParam(r, "package_id"),
		RepositoryID:      repositoryID,
		SubjectDigest:     querycontract.QueryParam(r, "subject_digest"),
		ImageRef:          querycontract.QueryParam(r, "image_ref"),
		ImpactStatus:      querycontract.QueryParam(r, "impact_status"),
		Ecosystem:         querycontract.QueryParam(r, "ecosystem"),
		WorkloadID:        querycontract.QueryParam(r, "workload_id"),
		ServiceID:         querycontract.QueryParam(r, "service_id"),
		Environment:       querycontract.QueryParam(r, "environment"),
		Severity:          severity,
		DetectionProfile:  impact.FilterProfile(profile),
		PriorityBucket:    priorityBucket,
		MinPriorityScore:  minPriorityScore,
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
	}
	if access.Scoped() {
		filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
		filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	}
	return filter, true
}

func requestedSupplyChainImpactAggregateProfile(filter impact.SupplyChainImpactAggregateFilter) string {
	if filter.DetectionProfile == impact.SupplyChainImpactProfilePrecise {
		return impact.SupplyChainImpactProfilePrecise
	}
	return impact.SupplyChainImpactProfileComprehensive
}

func supplyChainImpactAggregateScope(filter impact.SupplyChainImpactAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.CVEID != "" {
		out["cve_id"] = filter.CVEID
	}
	if filter.AdvisoryID != "" {
		out["advisory_id"] = filter.AdvisoryID
	}
	if filter.PackageID != "" {
		out["package_id"] = filter.PackageID
	}
	if filter.RepositoryID != "" {
		out["repository_id"] = filter.RepositoryID
	}
	if filter.SubjectDigest != "" {
		out["subject_digest"] = filter.SubjectDigest
	}
	if filter.ImageRef != "" {
		out["image_ref"] = filter.ImageRef
	}
	if filter.ImpactStatus != "" {
		out["impact_status"] = filter.ImpactStatus
	}
	if filter.Ecosystem != "" {
		out["ecosystem"] = filter.Ecosystem
	}
	if filter.WorkloadID != "" {
		out["workload_id"] = filter.WorkloadID
	}
	if filter.ServiceID != "" {
		out["service_id"] = filter.ServiceID
	}
	if filter.Environment != "" {
		out["environment"] = filter.Environment
	}
	if filter.Severity != "" {
		out["severity"] = filter.Severity
	}
	out["profile"] = requestedSupplyChainImpactAggregateProfile(filter)
	if filter.PriorityBucket != "" {
		out["priority_bucket"] = filter.PriorityBucket
	}
	if filter.MinPriorityScore > 0 {
		out["min_priority_score"] = strconv.Itoa(filter.MinPriorityScore)
	}
	if filter.SuppressionState != "" {
		out["suppression_state"] = filter.SuppressionState
	}
	if filter.IncludeSuppressed {
		out["include_suppressed"] = strconv.FormatBool(filter.IncludeSuppressed)
	}
	return out
}

func isSupportedSupplyChainImpactDimension(d impact.SupplyChainImpactInventoryDimension) bool {
	switch d {
	case impact.SupplyChainImpactInventoryByImpactStatus,
		impact.SupplyChainImpactInventoryByPriorityBucket,
		impact.SupplyChainImpactInventoryBySeverity,
		impact.SupplyChainImpactInventoryByRepository,
		impact.SupplyChainImpactInventoryByEcosystem:
		return true
	default:
		return false
	}
}

const (
	supplyChainImpactAggregateDefaultLimit = 100
	supplyChainImpactAggregateMinLimit     = 1
	// supplyChainImpactAggregateMaxOffset matches the OpenAPI offset bound and
	// keeps Postgres OFFSET scans bounded; the page-and-iterate pattern this
	// aggregate replaces would have to fall back to the list endpoint past this
	// point.
	supplyChainImpactAggregateMaxOffset = 10000
)

func parseSupplyChainImpactAggregateLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return supplyChainImpactAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < supplyChainImpactAggregateMinLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > impact.SupplyChainImpactAggregateMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseSupplyChainImpactAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		querycontract.WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > supplyChainImpactAggregateMaxOffset {
		querycontract.WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// NextSupplyChainImpactAggregateOffset returns the next offset when a truncated
// page can be continued without exceeding the documented offset bound, and nil
// otherwise. Callers serialize the nil as JSON null so generated clients see a
// clean end-of-stream marker instead of an out-of-contract integer.
func NextSupplyChainImpactAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > supplyChainImpactAggregateMaxOffset {
		return nil
	}
	return next
}
