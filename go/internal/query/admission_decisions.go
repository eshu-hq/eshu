// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	admissionDecisionCapability    = "admission_decisions.list"
	admissionDecisionDefaultLimit  = 50
	admissionDecisionMaxLimit      = 200
	admissionDecisionEvidenceLimit = 20
)

func (h *EvidenceHandler) listAdmissionDecisions(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdmissionDecisions,
		"GET /api/v0/evidence/admission-decisions",
		admissionDecisionCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), admissionDecisionCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"admission decisions require the local-authoritative profile or higher",
			ErrorCodeUnsupportedCapability,
			admissionDecisionCapability,
			h.profile(),
			requiredProfile(admissionDecisionCapability),
		)
		return
	}

	filter, limit, includeEvidence, ok := admissionDecisionFilterFromRequest(w, r)
	if !ok {
		return
	}
	access := repositoryAccessFilterFromContext(r.Context())
	filter = admissionDecisionFilterWithRepositoryAccess(filter, access)
	if access.empty() || !admissionDecisionReadFilterAllowed(filter) {
		h.writeEmptyAdmissionDecisionPage(w, r, limit)
		return
	}
	if h.AdmissionDecisions == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"admission decisions require the Postgres admission decision read model",
			ErrorCodeBackendUnavailable,
			admissionDecisionCapability,
			h.profile(),
			requiredProfile(admissionDecisionCapability),
		)
		return
	}
	rows, err := h.AdmissionDecisions.ListAdmissionDecisions(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]AdmissionDecisionResult, 0, len(rows))
	for _, row := range rows {
		result := admissionDecisionResult(row)
		if includeEvidence {
			evidence, err := h.AdmissionDecisions.ListAdmissionDecisionEvidence(
				r.Context(),
				row.DecisionID,
				admissionDecisionEvidenceLimit+1,
			)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, err.Error())
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		admissionDecisionCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned correlation admission decision read model",
	))
}

func admissionDecisionFilterFromRequest(
	w http.ResponseWriter,
	r *http.Request,
) (AdmissionDecisionReadFilter, int, bool, bool) {
	filter := AdmissionDecisionReadFilter{
		Domain:       QueryParam(r, "domain"),
		ScopeID:      QueryParam(r, "scope_id"),
		GenerationID: QueryParam(r, "generation_id"),
		AnchorKind:   QueryParam(r, "anchor_kind"),
		AnchorID:     QueryParam(r, "anchor_id"),
	}
	if filter.Domain == "" || filter.ScopeID == "" || filter.GenerationID == "" {
		WriteError(w, http.StatusBadRequest, "domain, scope_id, and generation_id are required")
		return AdmissionDecisionReadFilter{}, 0, false, false
	}
	state := QueryParam(r, "state")
	if state != "" {
		if !validAdmissionDecisionState(state) {
			WriteError(w, http.StatusBadRequest, "state is not supported")
			return AdmissionDecisionReadFilter{}, 0, false, false
		}
		filter.State = &state
	}
	if (filter.AnchorKind == "") != (filter.AnchorID == "") {
		WriteError(w, http.StatusBadRequest, "anchor_kind and anchor_id must be provided together")
		return AdmissionDecisionReadFilter{}, 0, false, false
	}
	limit := admissionDecisionLimit(QueryParamInt(r, "limit", admissionDecisionDefaultLimit))
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
	value := QueryParam(r, key)
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

func (h *EvidenceHandler) writeEmptyAdmissionDecisionPage(w http.ResponseWriter, r *http.Request, limit int) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"decisions":              []AdmissionDecisionResult{},
		"count":                  0,
		"limit":                  limit,
		"truncated":              false,
		"recommended_next_calls": []RecommendedNextCall{},
	}, BuildTruthEnvelope(
		h.profile(),
		admissionDecisionCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned correlation admission decision read model",
	))
}

func admissionDecisionFilterWithRepositoryAccess(
	filter AdmissionDecisionReadFilter,
	access repositoryAccessFilter,
) AdmissionDecisionReadFilter {
	if !access.scoped() {
		return filter
	}
	filter.Scoped = true
	filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
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

func admissionDecisionResult(row AdmissionDecisionReadRow) AdmissionDecisionResult {
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
