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
	filter := normalizeAdvisoryEvidenceFilter(AdvisoryEvidenceFilter{
		CVEID:            QueryParam(r, "cve_id"),
		AdvisoryID:       QueryParam(r, "advisory_id"),
		PackageID:        QueryParam(r, "package_id"),
		Source:           QueryParam(r, "source"),
		AfterAdvisoryKey: QueryParam(r, "after_advisory_key"),
		Limit:            limit + 1,
	})
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, or package_id is required")
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
		"truncated":  truncated,
	}
	if truncated && len(rows) > 0 {
		body["next_cursor"] = map[string]string{"after_advisory_key": rows[len(rows)-1].AdvisoryKey}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		advisoryEvidenceCapability,
		TruthBasisSemanticFacts,
		"resolved from active vulnerability source facts; advisory evidence remains source-only and does not imply package, repository, image, workload, or deployment impact",
	))
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
