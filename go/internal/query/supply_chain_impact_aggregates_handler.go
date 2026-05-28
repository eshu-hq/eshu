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

	filter, ok := h.supplyChainImpactAggregateFilterFromRequest(w, r)
	if !ok {
		return
	}

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

	dimension := SupplyChainImpactInventoryDimension(QueryParam(r, "group_by"))
	if dimension == "" {
		dimension = SupplyChainImpactInventoryByImpactStatus
	}
	if !isSupportedSupplyChainImpactDimension(dimension) {
		WriteError(w, http.StatusBadRequest, "group_by must be one of impact_status, priority_bucket, severity, repository_id")
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
	filter, ok := h.supplyChainImpactAggregateFilterFromRequest(w, r)
	if !ok {
		return
	}

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
		"buckets":     rows,
		"count":       len(rows),
		"limit":       limit,
		"offset":      offset,
		"group_by":    string(dimension),
		"truncated":   truncated,
		"next_offset": nextSupplyChainImpactAggregateOffset(offset, limit, truncated),
		"scope":       supplyChainImpactAggregateScope(filter),
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
) (SupplyChainImpactAggregateFilter, bool) {
	repositoryID, ok := h.resolveSupplyChainRepositorySelector(w, r, QueryParam(r, "repository_id"))
	if !ok {
		return SupplyChainImpactAggregateFilter{}, false
	}
	return SupplyChainImpactAggregateFilter{
		CVEID:         QueryParam(r, "cve_id"),
		PackageID:     QueryParam(r, "package_id"),
		RepositoryID:  repositoryID,
		SubjectDigest: QueryParam(r, "subject_digest"),
		ImpactStatus:  QueryParam(r, "impact_status"),
	}, true
}

func supplyChainImpactAggregateScope(filter SupplyChainImpactAggregateFilter) map[string]string {
	out := map[string]string{}
	if filter.CVEID != "" {
		out["cve_id"] = filter.CVEID
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
	if filter.ImpactStatus != "" {
		out["impact_status"] = filter.ImpactStatus
	}
	return out
}

func isSupportedSupplyChainImpactDimension(d SupplyChainImpactInventoryDimension) bool {
	switch d {
	case SupplyChainImpactInventoryByImpactStatus,
		SupplyChainImpactInventoryByPriorityBucket,
		SupplyChainImpactInventoryBySeverity,
		SupplyChainImpactInventoryByRepository:
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
