// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coverage

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	observabilityCoverageCorrelationMaxLimit = 200
)

// Handler exposes reducer-owned observability coverage
// correlation reads: whether a monitored cloud resource or service has alarm,
// dashboard, log, or trace coverage, and which coverage gaps remain.
type Handler struct {
	Content      querycontract.ContentStore
	Correlations ObservabilityCorrelationStore
	Profile      querycontract.QueryProfile
}

// CorrelationResult is one reducer-owned observability
// coverage correlation row. Field order and types mirror
// CorrelationRow so handler code converts directly.
type CorrelationResult struct {
	CorrelationID          string   `json:"correlation_id"`
	Provider               string   `json:"provider,omitempty"`
	CoverageSignal         string   `json:"coverage_signal,omitempty"`
	ObservabilityObjectRef string   `json:"observability_object_ref,omitempty"`
	ObservabilityUID       string   `json:"observability_resource_uid,omitempty"`
	TargetUID              string   `json:"target_uid,omitempty"`
	TargetServiceRef       string   `json:"target_service_ref,omitempty"`
	Outcome                string   `json:"outcome"`
	Reason                 string   `json:"reason,omitempty"`
	CoverageStatus         string   `json:"coverage_status,omitempty"`
	ProvenanceOnly         bool     `json:"provenance_only"`
	ResolutionMode         string   `json:"resolution_mode,omitempty"`
	SourceClass            string   `json:"source_class,omitempty"`
	SourceClasses          []string `json:"source_classes,omitempty"`
	SourceKind             string   `json:"source_kind,omitempty"`
	SourceKinds            []string `json:"source_kinds,omitempty"`
	SourceOutcome          string   `json:"source_outcome,omitempty"`
	SourceOutcomes         []string `json:"source_outcomes,omitempty"`
	ResourceClass          string   `json:"resource_class,omitempty"`
	FreshnessState         string   `json:"freshness_state,omitempty"`
	CandidateTargetUIDs    []string `json:"candidate_target_uids,omitempty"`
	EvidenceFactIDs        []string `json:"evidence_fact_ids,omitempty"`
}

// Mount registers observability coverage query routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/observability/coverage/correlations", h.listCorrelations)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) listCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryObservabilityCoverageCorrelations,
		"GET /api/v0/observability/coverage/correlations",
		Capability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), Capability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"observability coverage correlations require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}
	limit, ok := requiredObservabilityCoverageCorrelationLimit(w, r)
	if !ok {
		return
	}
	filter := CorrelationFilter{
		ScopeID:                querycontract.QueryParam(r, "scope_id"),
		Provider:               querycontract.QueryParam(r, "provider"),
		CoverageSignal:         querycontract.QueryParam(r, "coverage_signal"),
		ObservabilityObjectRef: querycontract.QueryParam(r, "observability_object_ref"),
		TargetUID:              querycontract.QueryParam(r, "target_uid"),
		TargetServiceRef:       querycontract.QueryParam(r, "target_service_ref"),
		Outcome:                querycontract.QueryParam(r, "outcome"),
		CoverageStatus:         querycontract.QueryParam(r, "coverage_status"),
		SourceClass:            querycontract.QueryParam(r, "source_class"),
		ResourceClass:          querycontract.QueryParam(r, "resource_class"),
		AfterCorrelationID:     querycontract.QueryParam(r, "after_correlation_id"),
		Limit:                  limit + 1,
	}
	if !filter.hasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id, provider, coverage_signal, observability_object_ref, target_uid, or target_service_ref is required")
		return
	}
	// Access scoping (#5167 Group B): reducer_observability_coverage_correlation
	// facts carry no repository grant of their own, and hasScope() above
	// accepts any single anchor, so an unscoped filter could otherwise fan out
	// across every tenant's ingestion scope. A scoped caller with no granted
	// repository or ingestion scope never reaches the store (#5137
	// LiveActivityStore precedent); a granted scoped caller's rows are
	// additionally bound to its grant in ListObservabilityCoverageCorrelations.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyObservabilityCoverageCorrelations(w, r, limit)
		return
	}
	filter.AllScopes = !access.Scoped()
	filter.AllowedRepositoryIDs = access.GrantedRepositoryIDs()
	filter.AllowedScopeIDs = access.GrantedScopeIDs()
	if h.Correlations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"observability coverage correlations require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			Capability,
			h.profile(),
			querycontract.RequiredProfile(Capability),
		)
		return
	}

	rows, err := h.Correlations.ListObservabilityCoverageCorrelations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]CorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, CorrelationResult(row))
	}
	body := map[string]any{
		"correlations": results,
		"count":        len(results),
		"limit":        limit,
		"truncated":    truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_correlation_id": results[len(results)-1].CorrelationID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned observability coverage correlation facts; coverage is structural correlation, not a health assertion from telemetry values",
	))
}

// writeEmptyObservabilityCoverageCorrelations returns the bounded empty
// correlations page for a scoped caller with no granted repository or
// ingestion scope, without querying Postgres (#5167 Group B, #5137
// LiveActivityStore precedent).
func (h *Handler) writeEmptyObservabilityCoverageCorrelations(w http.ResponseWriter, r *http.Request, limit int) {
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"correlations": []CorrelationResult{},
		"count":        0,
		"limit":        limit,
		"truncated":    false,
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		Capability,
		querycontract.TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; observability coverage correlations are empty",
	))
}

func requiredObservabilityCoverageCorrelationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > observabilityCoverageCorrelationMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", observabilityCoverageCorrelationMaxLimit))
		return 0, false
	}
	return limit, true
}
