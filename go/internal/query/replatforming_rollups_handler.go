// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// replatformingRollupsCapability is the provider-neutral capability for bounded
// replatforming drift and readiness rollups aggregated by account, environment,
// and service. Lightweight local runtime cannot materialize the reducer-owned
// drift and IaC evidence the rollup needs, so that profile returns
// unsupported_capability rather than a downgraded answer.
const replatformingRollupsCapability = "replatforming.rollups.readiness"

func (h *IaCHandler) handleReplatformingRollups(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryReplatformingRollups,
		"POST /api/v0/replatforming/rollups",
		replatformingRollupsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), replatformingRollupsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"replatforming rollups require reducer-materialized AWS runtime drift and IaC findings",
			ErrorCodeUnsupportedCapability,
			replatformingRollupsCapability,
			h.profile(),
			requiredProfile(replatformingRollupsCapability),
		)
		return
	}

	var req iacManagementRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := normalizeIaCManagementRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter = bindIaCManagementFilterAccess(r.Context(), filter)
	if h == nil || h.Management == nil {
		WriteError(w, http.StatusServiceUnavailable, "IaC management store is required for replatforming rollups")
		return
	}

	totalFindings, err := h.Management.CountUnmanagedCloudResources(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	findings, err := h.Management.ListUnmanagedCloudResources(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	findings = normalizeIaCManagementFindingsSafety(findings)
	rollups := buildReplatformingRollups(findings, filter)

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"scope_id":      filter.ScopeID,
		"account_id":    filter.AccountID,
		"region":        filter.Region,
		"finding_kinds": filter.FindingKinds,
		"story":         replatformingRollupsStory(filter, rollups, len(findings), totalFindings),
		"dimensions": map[string]any{
			"account":     rollups.Account,
			"environment": rollups.Environment,
			"service":     rollups.Service,
		},
		"source_state_totals":   rollups.TotalStates,
		"readiness_totals":      rollups.Readiness,
		"rollup_findings_count": len(findings),
		"total_findings_count":  totalFindings,
		"limit":                 filter.Limit,
		"offset":                filter.Offset,
		"truncated":             iacManagementTruncated(filter.Offset, len(findings), totalFindings),
		"next_offset":           iacManagementNextOffset(filter.Offset, len(findings), totalFindings),
		"truth_basis":           "materialized_reducer_rows",
		"analysis_status":       "replatforming_rollups",
		"recommended_next_checks": []string{
			"drill into the ambiguous and unattributed buckets before promoting any owner",
			"resolve stale, unavailable, and unknown source states before reporting readiness as clean",
			"review the refused readiness count; refused items must not drive Terraform import automation",
		},
		"limitations": []string{
			"bounded to the active AWS runtime drift reducer facts within the requested limit",
			"source states are preserved per item; unsupported, stale, and unavailable are never folded into clean",
			"ambiguous or missing service and environment attribution is counted under explicit buckets, never guessed",
			"counts reflect the bounded page; when truncated is true, re-run with offset or a tighter scope for a full rollup",
		},
	}, BuildTruthEnvelope(
		h.profile(),
		replatformingRollupsCapability,
		TruthBasisSemanticFacts,
		"aggregated from reducer-materialized AWS runtime drift and IaC findings; per-item source state and readiness preserved",
	))
}

func replatformingRollupsStory(
	filter IaCManagementFilter,
	rollups replatformingRollupResult,
	returned int,
	total int,
) string {
	scope := iacFirstNonEmpty(filter.ScopeID, filter.AccountID)
	if scope == "" {
		scope = "requested AWS scope"
	}
	return fmt.Sprintf(
		"%d active AWS replatforming findings matched %s; %d in this rollup across %d accounts, %d environments, and %d services "+
			"(%d import-ready, %d need review, %d refused).",
		total,
		scope,
		returned,
		len(rollups.Account),
		len(rollups.Environment),
		len(rollups.Service),
		rollups.Readiness.ImportReady,
		rollups.Readiness.NeedsReview,
		rollups.Readiness.Refused,
	)
}
