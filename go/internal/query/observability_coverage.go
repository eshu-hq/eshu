// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	observabilityCoverageCorrelationsCapability = "observability.coverage.correlations.list"
	observabilityCoverageCorrelationMaxLimit    = 200
)

// ObservabilityCoverageHandler exposes reducer-owned observability coverage
// correlation reads: whether a monitored cloud resource or service has alarm,
// dashboard, log, or trace coverage, and which coverage gaps remain.
type ObservabilityCoverageHandler struct {
	Content      ContentStore
	Correlations ObservabilityCoverageCorrelationStore
	Profile      QueryProfile
}

// ObservabilityCoverageCorrelationResult is one reducer-owned observability
// coverage correlation row. Field order and types mirror
// ObservabilityCoverageCorrelationRow so handler code converts directly.
type ObservabilityCoverageCorrelationResult struct {
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
func (h *ObservabilityCoverageHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/observability/coverage/correlations", h.listCorrelations)
}

func (h *ObservabilityCoverageHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *ObservabilityCoverageHandler) listCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryObservabilityCoverageCorrelations,
		"GET /api/v0/observability/coverage/correlations",
		observabilityCoverageCorrelationsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), observabilityCoverageCorrelationsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"observability coverage correlations require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			observabilityCoverageCorrelationsCapability,
			h.profile(),
			requiredProfile(observabilityCoverageCorrelationsCapability),
		)
		return
	}
	limit, ok := requiredObservabilityCoverageCorrelationLimit(w, r)
	if !ok {
		return
	}
	filter := ObservabilityCoverageCorrelationFilter{
		ScopeID:                QueryParam(r, "scope_id"),
		Provider:               QueryParam(r, "provider"),
		CoverageSignal:         QueryParam(r, "coverage_signal"),
		ObservabilityObjectRef: QueryParam(r, "observability_object_ref"),
		TargetUID:              QueryParam(r, "target_uid"),
		TargetServiceRef:       QueryParam(r, "target_service_ref"),
		Outcome:                QueryParam(r, "outcome"),
		CoverageStatus:         QueryParam(r, "coverage_status"),
		SourceClass:            QueryParam(r, "source_class"),
		ResourceClass:          QueryParam(r, "resource_class"),
		AfterCorrelationID:     QueryParam(r, "after_correlation_id"),
		Limit:                  limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id, provider, coverage_signal, observability_object_ref, target_uid, or target_service_ref is required")
		return
	}
	if h.Correlations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"observability coverage correlations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			observabilityCoverageCorrelationsCapability,
			h.profile(),
			requiredProfile(observabilityCoverageCorrelationsCapability),
		)
		return
	}

	rows, err := h.Correlations.ListObservabilityCoverageCorrelations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]ObservabilityCoverageCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, ObservabilityCoverageCorrelationResult(row))
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		observabilityCoverageCorrelationsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned observability coverage correlation facts; coverage is structural correlation, not a health assertion from telemetry values",
	))
}

func requiredObservabilityCoverageCorrelationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > observabilityCoverageCorrelationMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", observabilityCoverageCorrelationMaxLimit))
		return 0, false
	}
	return limit, true
}
