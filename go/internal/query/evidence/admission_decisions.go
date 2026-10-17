// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// AdmissionDecisionCapability keys admission decision rows in the root
	// contract capability matrix (see contract/admission_decisions.go, which
	// carries its own copy) and gates the admission decision routes via
	// querycontract.CapabilityUnsupported.
	AdmissionDecisionCapability    = "admission_decisions.list"
	admissionDecisionDefaultLimit  = 50
	admissionDecisionMaxLimit      = 200
	admissionDecisionEvidenceLimit = 20
)

// Fixed bodies of the two failed reads on GET /api/v0/evidence/admission-decisions
// (#7674): the decision page and one decision's evidence. Both read the
// admission decision read model through the guarded PostgreSQL reader, so a
// stale or timed-out reader answers the retryable 503 first. The store error
// can quote SQL and hosts, so it goes on the handler span, never into the body.
const (
	admissionDecisionReadFailedMessage         = "admission decision read failed"
	admissionDecisionEvidenceReadFailedMessage = "admission decision evidence read failed"
)

func (h *Handler) listAdmissionDecisions(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdmissionDecisions,
		"GET /api/v0/evidence/admission-decisions",
		AdmissionDecisionCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), AdmissionDecisionCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"admission decisions require the local-authoritative profile or higher",
			querycontract.ErrorCodeUnsupportedCapability,
			AdmissionDecisionCapability,
			h.profile(),
			querycontract.RequiredProfile(AdmissionDecisionCapability),
		)
		return
	}

	filter, limit, includeEvidence, ok := admissionDecisionFilterFromRequest(w, r)
	if !ok {
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	filter = admissionDecisionFilterWithRepositoryAccess(filter, access)
	if access.Empty() || !admissionDecisionReadFilterAllowed(filter) {
		h.writeEmptyAdmissionDecisionPage(w, r, limit)
		return
	}
	if h.AdmissionDecisions == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"admission decisions require the Postgres admission decision read model",
			querycontract.ErrorCodeBackendUnavailable,
			AdmissionDecisionCapability,
			h.profile(),
			querycontract.RequiredProfile(AdmissionDecisionCapability),
		)
		return
	}
	rows, err := h.AdmissionDecisions.ListAdmissionDecisions(r.Context(), filter)
	if err != nil {
		writeReadFailure(w, r, err, AdmissionDecisionCapability, admissionDecisionReadFailedMessage)
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]AdmissionDecisionResult, 0, len(rows))
	for _, row := range rows {
		result := AdmissionDecisionResultFromRow(row)
		if includeEvidence {
			evidence, err := h.AdmissionDecisions.ListAdmissionDecisionEvidence(
				r.Context(),
				row.DecisionID,
				admissionDecisionEvidenceLimit+1,
			)
			if err != nil {
				writeReadFailure(w, r, err, AdmissionDecisionCapability, admissionDecisionEvidenceReadFailedMessage)
				return
			}
			result.Evidence, result.EvidenceTruncated = boundedAdmissionDecisionEvidence(evidence)
			result.EvidenceLimit = admissionDecisionEvidenceLimit
		}
		results = append(results, result)
	}
	body := map[string]any{
		"decisions":              results,
		"count":                  len(results),
		"limit":                  limit,
		"truncated":              truncated,
		"recommended_next_calls": admissionDecisionRecommendedNextCalls(filter, truncated, includeEvidence, len(results)),
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		AdmissionDecisionCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned correlation admission decision read model",
	))
}

func admissionDecisionFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
) (AdmissionDecisionReadFilter, int, bool, bool) {
	filter := AdmissionDecisionReadFilter{
		Domain:       querycontract.QueryParam(r, "domain"),
		ScopeID:      querycontract.QueryParam(r, "scope_id"),
		GenerationID: querycontract.QueryParam(r, "generation_id"),
		AnchorKind:   querycontract.QueryParam(r, "anchor_kind"),
		AnchorID:     querycontract.QueryParam(r, "anchor_id"),
	}
	if filter.Domain == "" || filter.ScopeID == "" || filter.GenerationID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "domain, scope_id, and generation_id are required")
		return AdmissionDecisionReadFilter{}, 0, false, false
	}
	state := querycontract.QueryParam(r, "state")
	if state != "" {
		if !validAdmissionDecisionState(state) {
			querycontract.WriteError(w, http.StatusBadRequest, "state is not supported")
			return AdmissionDecisionReadFilter{}, 0, false, false
		}
		filter.State = &state
	}
	if (filter.AnchorKind == "") != (filter.AnchorID == "") {
		querycontract.WriteError(w, http.StatusBadRequest, "anchor_kind and anchor_id must be provided together")
		return AdmissionDecisionReadFilter{}, 0, false, false
	}
	limit := admissionDecisionLimit(querycontract.QueryParamInt(r, "limit", admissionDecisionDefaultLimit))
	filter.Limit = limit + 1
	return filter, limit, queryParamBool(r, "include_evidence"), true
}

func admissionDecisionLimit(limit int) int {
	if limit <= 0 {
		return admissionDecisionDefaultLimit
	}
	if limit > admissionDecisionMaxLimit {
		return admissionDecisionMaxLimit
	}
	return limit
}

func queryParamBool(r *http.Request, key string) bool {
	value := querycontract.QueryParam(r, key)
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func boundedAdmissionDecisionEvidence(
	evidence []AdmissionDecisionEvidenceRow,
) ([]AdmissionDecisionEvidenceRow, *bool) {
	truncated := len(evidence) > admissionDecisionEvidenceLimit
	if truncated {
		evidence = evidence[:admissionDecisionEvidenceLimit]
	}
	return evidence, &truncated
}

func (h *Handler) writeEmptyAdmissionDecisionPage(w http.ResponseWriter, r *http.Request, limit int) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"decisions":              []AdmissionDecisionResult{},
		"count":                  0,
		"limit":                  limit,
		"truncated":              false,
		"recommended_next_calls": []RecommendedNextCall{},
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		AdmissionDecisionCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned correlation admission decision read model",
	))
}

func admissionDecisionFilterWithRepositoryAccess(
	filter AdmissionDecisionReadFilter,
	access querycontract.RepositoryAccessFilter,
) AdmissionDecisionReadFilter {
	if !access.Scoped() {
		return filter
	}
	filter.Scoped = true
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter
}

func admissionDecisionReadFilterAllowed(filter AdmissionDecisionReadFilter) bool {
	if !filter.Scoped {
		return true
	}
	for _, allowed := range filter.AllowedScopeIDs {
		if filter.ScopeID == allowed {
			return true
		}
	}
	for _, allowed := range filter.AllowedRepositoryIDs {
		if filter.ScopeID == allowed {
			return true
		}
	}
	if filter.AnchorKind == "repository" {
		for _, allowed := range filter.AllowedRepositoryIDs {
			if filter.AnchorID == allowed {
				return true
			}
		}
	}
	return false
}

func admissionDecisionRecommendedNextCalls(
	filter AdmissionDecisionReadFilter,
	truncated bool,
	includeEvidence bool,
	count int,
) []RecommendedNextCall {
	calls := make([]RecommendedNextCall, 0, 2)
	if truncated {
		calls = append(calls, RecommendedNextCall{
			Tool:   "list_admission_decisions",
			Route:  "/api/v0/evidence/admission-decisions",
			Reason: "narrow by state, anchor_kind, and anchor_id because this page is truncated",
			Args:   admissionDecisionBaseArgs(filter),
		})
	}
	if !includeEvidence && count > 0 {
		args := admissionDecisionBaseArgs(filter)
		args["include_evidence"] = "true"
		calls = append(calls, RecommendedNextCall{
			Tool:   "list_admission_decisions",
			Route:  "/api/v0/evidence/admission-decisions",
			Reason: "include bounded evidence rows for the returned decisions",
			Args:   args,
		})
	}
	return calls
}

func admissionDecisionBaseArgs(filter AdmissionDecisionReadFilter) map[string]string {
	args := map[string]string{
		"domain":        filter.Domain,
		"scope_id":      filter.ScopeID,
		"generation_id": filter.GenerationID,
	}
	if filter.State != nil {
		args["state"] = *filter.State
	}
	if filter.AnchorKind != "" {
		args["anchor_kind"] = filter.AnchorKind
		args["anchor_id"] = filter.AnchorID
	}
	return args
}

// AdmissionDecisionResultFromRow projects one persisted admission decision
// row into the HTTP/MCP read shape. Root keeps the pre-move spelling
// through its admissionDecisionResult forwarder (see compat_evidence.go)
// for the staying packet test.
func AdmissionDecisionResultFromRow(row AdmissionDecisionReadRow) AdmissionDecisionResult {
	return AdmissionDecisionResult{
		DecisionID:          row.DecisionID,
		Domain:              row.Domain,
		State:               row.State,
		DomainState:         row.DomainState,
		ScopeID:             row.ScopeID,
		GenerationID:        row.GenerationID,
		AnchorKind:          row.AnchorKind,
		AnchorID:            row.AnchorID,
		CandidateKind:       row.CandidateKind,
		CandidateID:         row.CandidateID,
		ConfidenceScore:     row.ConfidenceScore,
		ConfidenceBucket:    row.ConfidenceBucket,
		ConfidenceBasis:     row.ConfidenceBasis,
		FreshnessState:      row.FreshnessState,
		FreshnessObservedAt: row.FreshnessObservedAt,
		FreshnessCause:      row.FreshnessCause,
		SourceHandles:       nonNilAdmissionDecisionSourceHandles(row.SourceHandles),
		RedactionState:      row.RedactionState,
		RedactionReason:     row.RedactionReason,
		CanonicalWrite:      row.CanonicalWrite,
		RecommendedAction:   row.RecommendedAction,
		PayloadVersion:      row.PayloadVersion,
		DecidedAt:           row.DecidedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

func validAdmissionDecisionState(state string) bool {
	switch state {
	case "admitted", "rejected", "ambiguous", "stale", "missing_evidence",
		"permission_hidden", "unsupported", "unsafe":
		return true
	default:
		return false
	}
}

func nonNilAdmissionDecisionSourceHandles(
	handles []AdmissionDecisionSourceHandle,
) []AdmissionDecisionSourceHandle {
	if handles == nil {
		return []AdmissionDecisionSourceHandle{}
	}
	return handles
}
