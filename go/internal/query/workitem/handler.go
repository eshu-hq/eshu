// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Handler exposes source-only work-item evidence read surfaces.
//
// Package query keeps this type available as WorkItemHandler through a type
// alias in work_item_alias.go so existing wiring, callers, and tests compile
// unchanged (#6642).
type Handler struct {
	Evidence EvidenceStore
	Profile  querycontract.QueryProfile
}

// Mount registers work-item evidence routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/work-items/evidence", h.listWorkItemEvidence)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) listWorkItemEvidence(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryWorkItemEvidence,
		"GET /api/v0/work-items/evidence",
		EvidenceCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), EvidenceCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"work-item evidence requires active work-item source facts",
			querycontract.ErrorCodeUnsupportedCapability,
			EvidenceCapability,
			h.profile(),
			querycontract.RequiredProfile(EvidenceCapability),
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
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyWorkItemEvidencePage(w, r, limit)
		return
	}
	observedAfter, ok := parseOptionalWorkItemEvidenceTime(w, r, "observed_after")
	if !ok {
		return
	}
	filter := normalizeWorkItemEvidenceFilter(EvidenceFilter{
		ScopeID:              querycontract.QueryParam(r, "scope_id"),
		ProjectKey:           querycontract.QueryParam(r, "project_key"),
		WorkItemKey:          querycontract.QueryParam(r, "work_item_key"),
		ProviderWorkItemID:   querycontract.QueryParam(r, "provider_work_item_id"),
		ExternalURL:          querycontract.QueryParam(r, "external_url"),
		URLFingerprint:       querycontract.QueryParam(r, "url_fingerprint"),
		ObservedAfter:        observedAfter,
		AfterFactID:          querycontract.QueryParam(r, "after_fact_id"),
		Limit:                limit + 1,
		AllowedRepositoryIDs: access.RepositorySearchIDs(),
	})
	if !filter.hasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id, project_key, work_item_key, provider_work_item_id, external_url, url_fingerprint, or observed_after is required")
		return
	}
	if h.Evidence == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"work-item evidence requires active work-item source facts",
			querycontract.ErrorCodeBackendUnavailable,
			EvidenceCapability,
			h.profile(),
			querycontract.RequiredProfile(EvidenceCapability),
		)
		return
	}

	page, err := h.Evidence.ListWorkItemEvidence(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Truncated and the next cursor come from page (derived from the raw
	// fetched fact count), never from len(page.Rows): a fact dropped mid-page
	// by a failed typed decode must not make a truncated page look complete or
	// hide the evidence beyond it (#4733).
	rows := page.Rows
	truncated := page.Truncated
	span.SetAttributes(workItemEvidenceSpanAttributes(rows, truncated)...)
	body := map[string]any{
		"evidence":         rows,
		"count":            len(rows),
		"limit":            limit,
		"truncated":        truncated,
		"missing_evidence": len(rows) == 0,
		"states":           summarizeWorkItemEvidenceStates(rows),
	}
	if truncated {
		body["next_cursor"] = map[string]string{"after_fact_id": page.NextCursorFactID}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		EvidenceCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from active work-item source facts; Jira evidence remains source-only and does not verify pull request, commit, deployment, incident, runtime artifact, image, version, or service identity",
	))
}

func requiredWorkItemEvidenceLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > evidenceMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", evidenceMaxLimit))
		return 0, false
	}
	return limit, true
}

func parseOptionalWorkItemEvidenceTime(w http.ResponseWriter, r *http.Request, key string) (time.Time, bool) {
	raw := querycontract.QueryParam(r, key)
	if raw == "" {
		return time.Time{}, true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, key+" must be RFC3339")
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
