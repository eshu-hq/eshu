// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// listAdvisoryCatalog returns a bounded, browsable page of the known
// vulnerability-intelligence catalog from active vulnerability source facts.
//
// Unlike GET /api/v0/supply-chain/advisories/evidence, this surface needs no
// advisory, package, repository, service, or workload anchor: it lists canonical
// advisories so the console can browse CVE intelligence that is not yet
// reachable in any indexed service. Rows are summary-only source intelligence
// and do not imply repository, image, workload, or deployment impact; service
// reachability remains the separate supply-chain impact findings surface.
//
// GET /api/v0/supply-chain/advisories
func (h *SupplyChainHandler) listAdvisoryCatalog(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAdvisoryCatalog,
		"GET /api/v0/supply-chain/advisories",
		advisoryCatalogCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), advisoryCatalogCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"advisory catalog requires the Postgres vulnerability source fact read model",
			ErrorCodeUnsupportedCapability,
			advisoryCatalogCapability,
			h.profile(),
			requiredProfile(advisoryCatalogCapability),
		)
		return
	}
	limit, ok := requiredAdvisoryCatalogLimit(w, r)
	if !ok {
		return
	}
	kevOnly, ok := parseAdvisoryCatalogKEVOnly(w, r)
	if !ok {
		return
	}
	afterCVSS, afterKey, ok := parseAdvisoryCatalogCursor(w, r)
	if !ok {
		return
	}
	if h.AdvisoryCatalog == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"advisory catalog requires the Postgres vulnerability source fact read model",
			ErrorCodeBackendUnavailable,
			advisoryCatalogCapability,
			h.profile(),
			requiredProfile(advisoryCatalogCapability),
		)
		return
	}
	filter := AdvisoryCatalogFilter{
		Severity:         QueryParam(r, "severity"),
		Ecosystem:        QueryParam(r, "ecosystem"),
		Query:            QueryParam(r, "q"),
		KEVOnly:          kevOnly,
		AfterCVSS:        afterCVSS,
		AfterAdvisoryKey: afterKey,
		Limit:            limit + 1,
	}
	page, err := h.AdvisoryCatalog.ListAdvisoryCatalog(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := page.Rows
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	body := map[string]any{
		"advisories": rows,
		"count":      len(rows),
		"limit":      limit,
		"scope":      advisoryCatalogResponseScope(filter),
		"truncated":  truncated,
	}
	if truncated && len(rows) > 0 {
		last := rows[len(rows)-1]
		body["next_cursor"] = map[string]any{
			"after_cvss":         last.CVSSScore,
			"after_advisory_key": last.AdvisoryKey,
		}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		advisoryCatalogCapability,
		TruthBasisSemanticFacts,
		"resolved from active vulnerability source facts; rows are known CVE intelligence and do not imply repository, image, workload, or deployment impact, which remains the separate supply-chain impact findings surface",
	))
}

// advisoryCatalogResponseScope echoes the applied catalog filters so callers can
// confirm the browse scope and detect dropped filters.
func advisoryCatalogResponseScope(filter AdvisoryCatalogFilter) map[string]any {
	scope := map[string]any{}
	if filter.Severity != "" {
		scope["severity"] = filter.Severity
	}
	if filter.Ecosystem != "" {
		scope["ecosystem"] = filter.Ecosystem
	}
	if filter.Query != "" {
		scope["q"] = filter.Query
	}
	if filter.KEVOnly {
		scope["kev"] = true
	}
	return scope
}

// requiredAdvisoryCatalogLimit enforces an explicit, bounded page size so an
// unscoped catalog browse cannot return the whole fact corpus.
func requiredAdvisoryCatalogLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > advisoryCatalogMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", advisoryCatalogMaxLimit))
		return 0, false
	}
	return limit, true
}

// parseAdvisoryCatalogKEVOnly parses the optional kev boolean. Default false so
// the catalog lists all known advisories; anything other than true/false is a
// 400.
func parseAdvisoryCatalogKEVOnly(w http.ResponseWriter, r *http.Request) (bool, bool) {
	raw := QueryParam(r, "kev")
	if raw == "" {
		return false, true
	}
	switch raw {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		WriteError(w, http.StatusBadRequest, "kev must be true or false")
		return false, false
	}
}

// parseAdvisoryCatalogCursor reads the optional keyset cursor. Both after_cvss
// and after_advisory_key must be supplied together; supplying one without the
// other is a 400 so callers cannot request a non-deterministic continuation.
func parseAdvisoryCatalogCursor(w http.ResponseWriter, r *http.Request) (float64, string, bool) {
	rawCVSS := QueryParam(r, "after_cvss")
	rawKey := QueryParam(r, "after_advisory_key")
	if rawCVSS == "" && rawKey == "" {
		return 0, "", true
	}
	if rawCVSS == "" || rawKey == "" {
		WriteError(w, http.StatusBadRequest, "after_cvss and after_advisory_key must be provided together")
		return 0, "", false
	}
	cvss, err := strconv.ParseFloat(rawCVSS, 64)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "after_cvss must be a number")
		return 0, "", false
	}
	return cvss, rawKey, true
}
