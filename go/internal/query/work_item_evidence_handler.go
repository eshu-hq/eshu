// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// WorkItemHandler exposes source-only work-item evidence read surfaces.
type WorkItemHandler struct {
	Evidence WorkItemEvidenceStore
	Profile  QueryProfile
}

// Mount registers work-item evidence routes.
func (h *WorkItemHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/work-items/evidence", h.listWorkItemEvidence)
}

func (h *WorkItemHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *WorkItemHandler) listWorkItemEvidence(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryWorkItemEvidence,
		"GET /api/v0/work-items/evidence",
		workItemEvidenceCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), workItemEvidenceCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"work-item evidence requires active work-item source facts",
			ErrorCodeUnsupportedCapability,
			workItemEvidenceCapability,
			h.profile(),
			requiredProfile(workItemEvidenceCapability),
		)
		return
	}
	limit, ok := requiredWorkItemEvidenceLimit(w, r)
	if !ok {
		return
	}
	// Resolve scoped-token grants before the store read. An empty grant returns
	// the bounded zero-evidence page without touching the work-item store; a
	// non-empty grant binds the linked_repository_id intersection predicate so a
	// scoped caller observes only work items whose durable repository link is
	// granted. Shared, admin, and local callers carry no grant set and keep the
	// unscoped read path.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyWorkItemEvidencePage(w, r, limit)
		return
	}
	observedAfter, ok := parseOptionalWorkItemEvidenceTime(w, r, "observed_after")
	if !ok {
		return
	}
	filter := normalizeWorkItemEvidenceFilter(WorkItemEvidenceFilter{
		ScopeID:              QueryParam(r, "scope_id"),
		ProjectKey:           QueryParam(r, "project_key"),
		WorkItemKey:          QueryParam(r, "work_item_key"),
		ProviderWorkItemID:   QueryParam(r, "provider_work_item_id"),
		ExternalURL:          QueryParam(r, "external_url"),
		URLFingerprint:       QueryParam(r, "url_fingerprint"),
		ObservedAfter:        observedAfter,
		AfterFactID:          QueryParam(r, "after_fact_id"),
		Limit:                limit + 1,
		AllowedRepositoryIDs: access.repositorySearchIDs(),
	})
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id, project_key, work_item_key, provider_work_item_id, external_url, url_fingerprint, or observed_after is required")
		return
	}
	if h.Evidence == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"work-item evidence requires active work-item source facts",
			ErrorCodeBackendUnavailable,
			workItemEvidenceCapability,
			h.profile(),
			requiredProfile(workItemEvidenceCapability),
		)
		return
	}

	rows, err := h.Evidence.ListWorkItemEvidence(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	span.SetAttributes(workItemEvidenceSpanAttributes(rows, truncated)...)
	body := map[string]any{
		"evidence":         rows,
		"count":            len(rows),
		"limit":            limit,
		"truncated":        truncated,
		"missing_evidence": len(rows) == 0,
		"states":           summarizeWorkItemEvidenceStates(rows),
	}
	if truncated && len(rows) > 0 {
		body["next_cursor"] = map[string]string{"after_fact_id": rows[len(rows)-1].FactID}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		workItemEvidenceCapability,
		TruthBasisSemanticFacts,
		"resolved from active work-item source facts; Jira evidence remains source-only and does not verify pull request, commit, deployment, incident, runtime artifact, image, version, or service identity",
	))
}

func requiredWorkItemEvidenceLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > workItemEvidenceMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", workItemEvidenceMaxLimit))
		return 0, false
	}
	return limit, true
}

func parseOptionalWorkItemEvidenceTime(w http.ResponseWriter, r *http.Request, key string) (time.Time, bool) {
	raw := QueryParam(r, key)
	if raw == "" {
		return time.Time{}, true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, key+" must be RFC3339")
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
