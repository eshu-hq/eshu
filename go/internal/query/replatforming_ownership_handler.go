// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// replatformingOwnershipCapability is the provider-neutral capability for the
// bounded unmanaged-resource ownership packet surface. For each active AWS drift
// finding it composes owner, repository, module, service, and environment
// candidates with explicit ambiguity reasons. Lightweight local runtime cannot
// materialize the reducer-owned drift, IaC, service, and environment evidence
// the packet composes, so that profile returns unsupported_capability rather
// than a downgraded answer.
const replatformingOwnershipCapability = "replatforming.ownership.candidates"

func (h *IaCHandler) handleReplatformingOwnershipPackets(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryReplatformingOwnership,
		"POST /api/v0/replatforming/ownership-packets",
		replatformingOwnershipCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), replatformingOwnershipCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"unmanaged-resource ownership packets require reducer-materialized AWS runtime drift and IaC findings",
			ErrorCodeUnsupportedCapability,
			replatformingOwnershipCapability,
			h.profile(),
			requiredProfile(replatformingOwnershipCapability),
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
	if h == nil || h.Management == nil {
		WriteError(w, http.StatusServiceUnavailable, "IaC management store is required for replatforming ownership packets")
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
	summary := buildReplatformingOwnershipSummary(findings, filter)

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"scope_id":             filter.ScopeID,
		"account_id":           filter.AccountID,
		"region":               filter.Region,
		"finding_kinds":        filter.FindingKinds,
		"story":                replatformingOwnershipStory(filter, summary, totalFindings),
		"ownership_packets":    summary.Packets,
		"packets_count":        len(summary.Packets),
		"ambiguous_count":      summary.AmbiguousCount,
		"unattributed_count":   summary.UnattributedCount,
		"rejected_count":       summary.RejectedCount,
		"total_findings_count": totalFindings,
		"limit":                filter.Limit,
		"offset":               filter.Offset,
		"truncated":            iacManagementTruncated(filter.Offset, len(findings), totalFindings),
		"next_offset":          iacManagementNextOffset(filter.Offset, len(findings), totalFindings),
		"truth_basis":          "materialized_reducer_rows",
		"analysis_status":      "replatforming_ownership_packets",
		"recommended_next_checks": []string{
			"resolve the ambiguous and unattributed packets before promoting any owner, repository, module, service, or environment",
			"treat every candidate as a hint; confirm against repository and service ownership before import planning",
			"refused packets must not drive Terraform import automation",
		},
		"limitations": []string{
			"bounded to the active AWS runtime drift reducer facts within the requested limit",
			"owner, repository, module, service, and environment values are candidates, not confirmed ownership",
			"raw tags remain provenance evidence and never become owner candidates",
			"a single candidate is derived, never exact; conflicting candidates carry explicit ambiguity reasons",
			"counts reflect the bounded page; when truncated is true, re-run with offset or a tighter scope",
		},
	}, BuildTruthEnvelope(
		h.profile(),
		replatformingOwnershipCapability,
		TruthBasisSemanticFacts,
		"composed ownership candidates from reducer-materialized AWS runtime drift and IaC findings; candidates are never promoted to a single fabricated owner",
	))
}

// replatformingOwnershipStory summarizes the bounded ownership packet read for an
// operator without leaking any candidate value or resource identity.
func replatformingOwnershipStory(
	filter IaCManagementFilter,
	summary replatformingOwnershipSummary,
	total int,
) string {
	scope := iacFirstNonEmpty(filter.ScopeID, filter.AccountID)
	if scope == "" {
		scope = "requested AWS scope"
	}
	return fmt.Sprintf(
		"%d active AWS drift findings matched %s; composed %d ownership packets (%d ambiguous, %d unattributed, %d refused). "+
			"Every owner, repository, module, service, and environment value is a candidate, not confirmed ownership.",
		total,
		scope,
		len(summary.Packets),
		summary.AmbiguousCount,
		summary.UnattributedCount,
		summary.RejectedCount,
	)
}
