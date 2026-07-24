// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const supplyChainImpactAggregateCapability = "supply_chain.impact_findings.aggregate"

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
		supplyChainImpactAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), supplyChainImpactAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			supplyChainImpactAggregateCapability,
			h.profile(),
			requiredProfile(supplyChainImpactAggregateCapability),
		)
		return
	}
	if h.ImpactAggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			supplyChainImpactAggregateCapability,
			h.profile(),
			requiredProfile(supplyChainImpactAggregateCapability),
		)
		return
	}
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, impactFindingsScannerFilters()) {
		return
	}

	// Empty scoped grants return the zero-count shape without reading the
	// aggregate store or resolving a repository selector.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
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
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
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
	}, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned impact facts; severity buckets derived from CVSS score",
	))
}

func (h *SupplyChainHandler) impactInventory(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactAggregate,
		"GET /api/v0/supply-chain/impact/inventory",
		supplyChainImpactAggregateCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), supplyChainImpactAggregateCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact aggregates require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			supplyChainImpactAggregateCapability,
			h.profile(),
			requiredProfile(supplyChainImpactAggregateCapability),
		)
		return
	}
	if h.ImpactAggregates == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact aggregates require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			supplyChainImpactAggregateCapability,
			h.profile(),
			requiredProfile(supplyChainImpactAggregateCapability),
		)
		return
	}
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, impactFindingsScannerFilters()) {
		return
	}

	dimension := SupplyChainImpactInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = SupplyChainImpactInventoryByImpactStatus
	}
	if !isSupportedSupplyChainImpactDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of impact_status, priority_bucket, severity, repository_id, ecosystem")
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
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
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
		WriteError(w, http.StatusInternalServerError, err.Error())
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
		"next_offset":       nextSupplyChainImpactAggregateOffset(offset, limit, truncated),
		"scope":             supplyChainImpactAggregateScope(filter),
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactAggregateCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned impact facts; one grouped bucket per row, ordered by count desc",
	))
}

func (h *SupplyChainHandler) supplyChainImpactAggregateFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	access repositoryAccessFilter,
) (SupplyChainImpactAggregateFilter, bool) {
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, QueryParam(r, "repository_id"), access, supplyChainImpactAggregateCapability)
	if !ok {
		return SupplyChainImpactAggregateFilter{}, false
	}
	profile, ok := requestedSupplyChainImpactProfile(w, r)
	if !ok {
		return SupplyChainImpactAggregateFilter{}, false
	}
	advisoryID := QueryParam(r, "advisory_id")
	if advisoryID == "" {
		advisoryID = firstNonEmptyQueryParam(r, "ghsa_id", "osv_id")
	}
	severity, ok := parseSupplyChainScannerSeverity(w, r)
	if !ok {
		return SupplyChainImpactAggregateFilter{}, false
	}
	priorityBucket := QueryParam(r, "priority_bucket")
	if priorityBucket != "" && !validSupplyChainImpactPriorityBucket(priorityBucket) {
		WriteError(w, http.StatusBadRequest, "priority_bucket must be critical, high, medium, low, or informational")
		return SupplyChainImpactAggregateFilter{}, false
	}
	minPriorityScore, err := optionalSupplyChainImpactMinPriorityScore(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return SupplyChainImpactAggregateFilter{}, false
	}
	suppressionState := QueryParam(r, "suppression_state")
	if suppressionState != "" && !isSupportedSupplyChainSuppressionState(suppressionState) {
		WriteError(w, http.StatusBadRequest, "suppression_state must be one of active, not_affected, accepted_risk, false_positive, ignored, expired, provider_dismissed, scope_mismatch")
		return SupplyChainImpactAggregateFilter{}, false
	}
	includeSuppressed, ok := parseSupplyChainImpactIncludeSuppressed(w, r)
	if !ok {
		return SupplyChainImpactAggregateFilter{}, false
	}
	filter := SupplyChainImpactAggregateFilter{
		CVEID:             QueryParam(r, "cve_id"),
		AdvisoryID:        advisoryID,
		PackageID:         QueryParam(r, "package_id"),
		RepositoryID:      repositoryID,
		SubjectDigest:     QueryParam(r, "subject_digest"),
		ImageRef:          QueryParam(r, "image_ref"),
		ImpactStatus:      QueryParam(r, "impact_status"),
		Ecosystem:         QueryParam(r, "ecosystem"),
		WorkloadID:        QueryParam(r, "workload_id"),
		ServiceID:         QueryParam(r, "service_id"),
		Environment:       QueryParam(r, "environment"),
		Severity:          severity,
		DetectionProfile:  filterProfile(profile),
		PriorityBucket:    priorityBucket,
		MinPriorityScore:  minPriorityScore,
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
	}
	if access.scoped() {
		filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
		filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	}
	return filter, true
}

func requestedSupplyChainImpactAggregateProfile(filter SupplyChainImpactAggregateFilter) string {
	if filter.DetectionProfile == SupplyChainImpactProfilePrecise {
		return SupplyChainImpactProfilePrecise
	}
	return SupplyChainImpactProfileComprehensive
}

func supplyChainImpactAggregateScope(filter SupplyChainImpactAggregateFilter) map[string]string {
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

func isSupportedSupplyChainImpactDimension(d SupplyChainImpactInventoryDimension) bool {
	switch d {
	case SupplyChainImpactInventoryByImpactStatus,
		SupplyChainImpactInventoryByPriorityBucket,
		SupplyChainImpactInventoryBySeverity,
		SupplyChainImpactInventoryByRepository,
		SupplyChainImpactInventoryByEcosystem:
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
	raw := QueryParam(r, "limit")
	if raw == "" {
		return supplyChainImpactAggregateDefaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed < supplyChainImpactAggregateMinLimit {
		WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, false
	}
	if parsed > SupplyChainImpactAggregateMaxLimit {
		WriteError(w, http.StatusBadRequest, "limit exceeds maximum")
		return 0, false
	}
	return parsed, true
}

func parseSupplyChainImpactAggregateOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	if parsed > supplyChainImpactAggregateMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return 0, false
	}
	return parsed, true
}

// nextSupplyChainImpactAggregateOffset returns the next offset when a truncated
// page can be continued without exceeding the documented offset bound, and nil
// otherwise. Callers serialize the nil as JSON null so generated clients see a
// clean end-of-stream marker instead of an out-of-contract integer.
func nextSupplyChainImpactAggregateOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	next := offset + limit
	if next > supplyChainImpactAggregateMaxOffset {
		return nil
	}
	return next
}
