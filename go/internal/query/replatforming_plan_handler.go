// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// replatformingPlanRoute is the stable HTTP route that composes a service-scoped
// replatforming plan over reducer-owned IaC management and runtime-drift
// evidence. It is read-only: it observes, compares, and plans, but never runs
// Terraform, imports resources, or mutates cloud or repository state.
const replatformingPlanRoute = "/api/v0/replatforming/plans"

// replatformingPlanRequest is the bounded request body for the replatforming
// plan compose route. ScopeKind anchors the plan on one primary dimension; the
// remaining fields narrow and bound the underlying IaC management read. The plan
// reuses the AWS IaC management filter, so AccountID or ScopeID is required.
type replatformingPlanRequest struct {
	ScopeKind    string   `json:"scope_kind"`
	ScopeID      string   `json:"scope_id"`
	AccountID    string   `json:"account_id"`
	Region       string   `json:"region"`
	ServiceName  string   `json:"service_name"`
	WorkloadID   string   `json:"workload_id"`
	RepoID       string   `json:"repo_id"`
	Environment  string   `json:"environment"`
	ResourceID   string   `json:"resource_id"`
	ARN          string   `json:"arn"`
	FindingKinds []string `json:"finding_kinds"`
	Limit        int      `json:"limit"`
	Offset       int      `json:"offset"`
}

// handleReplatformingPlan composes one bounded, truth-labeled ReplatformingPlan
// for the requested scope. It reuses the IaC management findings store and the
// Terraform import-plan composition rather than re-deriving cloud truth, then
// maps each finding into a provider-neutral migration packet item with its
// source state, safety gate, owner candidates, and import candidate. Unsupported
// profiles return unsupported_capability so a lightweight runtime never serves a
// downgraded readiness rollup.
func (h *IaCHandler) handleReplatformingPlan(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryReplatformingPlan,
		"POST "+replatformingPlanRoute,
		replatformingPlanReadinessCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), replatformingPlanReadinessCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"replatforming plan composition requires reducer-materialized AWS runtime drift and IaC evidence",
			ErrorCodeUnsupportedCapability,
			replatformingPlanReadinessCapability,
			h.profile(),
			requiredProfile(replatformingPlanReadinessCapability),
		)
		return
	}

	var req replatformingPlanRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	scope, err := normalizeReplatformingScope(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := normalizeIaCManagementRequest(replatformingFilterRequest(req))
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter = bindIaCManagementFilterAccess(r.Context(), filter)
	if h == nil || h.Management == nil {
		WriteError(w, http.StatusServiceUnavailable, "IaC management store is required")
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

	plan := composeReplatformingPlan(scope, findings, filter)
	if validationErr := plan.Validate(); validationErr != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("composed replatforming plan failed contract validation: %v", validationErr))
		return
	}
	truncated := iacManagementTruncated(filter.Offset, len(findings), totalFindings)
	nextOffset := iacManagementNextOffset(filter.Offset, len(findings), totalFindings)
	plan.RecommendedNextCalls = replatformingPlanNextCalls(filter, nextOffset)
	plan.Limitations = replatformingPlanLimitations()

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"story":                  replatformingPlanStory(scope, plan, totalFindings),
		"scope_kind":             string(scope.Kind),
		"account_id":             filter.AccountID,
		"scope_id":               filter.ScopeID,
		"region":                 filter.Region,
		"arn":                    filter.ARN,
		"finding_kinds":          filter.FindingKinds,
		"plan":                   plan,
		"plan_truth_level":       string(plan.RollupTruthLevel()),
		"items_count":            len(plan.Items),
		"ready_import_count":     replatformingReadyImportCount(plan),
		"refused_import_count":   replatformingRefusedImportCount(plan),
		"wave_summaries":         replatformingPlanWaveSummaries(plan),
		"blast_radius_summaries": replatformingPlanBlastRadiusSummaries(plan),
		"total_findings_count":   totalFindings,
		"limit":                  filter.Limit,
		"offset":                 filter.Offset,
		"truncated":              truncated,
		"next_offset":            nextOffset,
		"recommended_next_calls": plan.RecommendedNextCalls,
		"truth_basis":            "materialized_reducer_rows",
		"analysis_status":        "replatforming_plan_composition",
		"limitations":            plan.Limitations,
	}, BuildTruthEnvelope(
		h.profile(),
		replatformingPlanReadinessCapability,
		TruthBasisSemanticFacts,
		"composed provider-neutral replatforming plan from reducer-materialized IaC management findings",
	))
}

// normalizeReplatformingScope validates the requested scope kind and builds the
// contract scope. Narrowing fields are copied through; the underlying IaC
// management filter still bounds the read on the AWS scope or account.
func normalizeReplatformingScope(req replatformingPlanRequest) (ReplatformingPlanScope, error) {
	kind := ReplatformingScopeKind(strings.ToLower(strings.TrimSpace(req.ScopeKind)))
	switch kind {
	case ReplatformingScopeAccount,
		ReplatformingScopeRegion,
		ReplatformingScopeService,
		ReplatformingScopeWorkload,
		ReplatformingScopeRepository,
		ReplatformingScopeEnvironment,
		ReplatformingScopeResource:
	case "":
		return ReplatformingPlanScope{}, fmt.Errorf("scope_kind is required")
	default:
		return ReplatformingPlanScope{}, fmt.Errorf("unsupported scope_kind %q", req.ScopeKind)
	}
	return ReplatformingPlanScope{
		Kind:        kind,
		Account:     strings.TrimSpace(req.AccountID),
		Region:      strings.TrimSpace(req.Region),
		Service:     strings.TrimSpace(req.ServiceName),
		Workload:    strings.TrimSpace(req.WorkloadID),
		Repository:  strings.TrimSpace(req.RepoID),
		Environment: strings.TrimSpace(req.Environment),
		Resource:    strings.TrimSpace(iacFirstNonEmpty(req.ResourceID, req.ARN)),
	}, nil
}

// replatformingFilterRequest maps the plan request onto the shared IaC
// management request shape so the plan reuses the bounded, validated AWS scope
// filter instead of re-deriving its own scoping rules.
func replatformingFilterRequest(req replatformingPlanRequest) iacManagementRequest {
	return iacManagementRequest{
		ScopeID:      req.ScopeID,
		AccountID:    req.AccountID,
		Region:       req.Region,
		ARN:          req.ARN,
		ResourceID:   req.ResourceID,
		FindingKinds: req.FindingKinds,
		Limit:        req.Limit,
		Offset:       req.Offset,
	}
}

// replatformingPlanLimitations names the fixed read-only bounds every plan
// response carries so a consumer cannot mistake a coverage gap for agreement or
// the plan for an execution surface.
func replatformingPlanLimitations() []string {
	return []string{
		"read-only surface; does not run Terraform, import resources, or mutate cloud or repository state",
		"bounded to active AWS runtime drift reducer facts for the requested scope or account",
		"owner candidates are read-only attributions; competing candidates name their ambiguity reasons and are never promoted to a single owner",
		"only safety-approved cloud_only findings for supported AWS resource families receive ready import candidates; the rest are refused with reasons",
	}
}

// replatformingPlanNextCalls returns the bounded follow-up calls a consumer can
// make to deepen the plan: pagination when truncated, plus the drill-down read
// surfaces that explain a single item's management status and drift evidence.
func replatformingPlanNextCalls(filter IaCManagementFilter, nextOffset *int) []map[string]any {
	calls := make([]map[string]any, 0, 3)
	if nextOffset != nil {
		calls = append(calls, map[string]any{
			"route":  "POST " + replatformingPlanRoute,
			"reason": "fetch the next page of migration packet items",
			"params": map[string]any{
				"account_id": filter.AccountID,
				"scope_id":   filter.ScopeID,
				"region":     filter.Region,
				"offset":     *nextOffset,
				"limit":      filter.Limit,
			},
		})
	}
	calls = append(calls, map[string]any{
		"route":  "POST /api/v0/iac/management-status/explain",
		"reason": "explain one item's IaC management status, evidence, and safety gate",
	})
	calls = append(calls, map[string]any{
		"route":  "POST /api/v0/aws/runtime-drift/findings",
		"reason": "inspect the underlying active AWS runtime drift findings for this scope",
	})
	return calls
}

func replatformingPlanStory(scope ReplatformingPlanScope, plan ReplatformingPlan, total int) string {
	return fmt.Sprintf(
		"Replatforming plan for %s scope inspected %d active IaC management findings and composed %d migration packet items (%d ready import candidates, %d refused).%s",
		scope.Kind,
		total,
		len(plan.Items),
		replatformingReadyImportCount(plan),
		replatformingRefusedImportCount(plan),
		replatformingWavesStorySuffix(plan),
	)
}

func replatformingReadyImportCount(plan ReplatformingPlan) int {
	count := 0
	for _, item := range plan.Items {
		if item.ImportCandidate != nil && item.ImportCandidate.Status == ReplatformingImportStatusReady {
			count++
		}
	}
	return count
}

func replatformingRefusedImportCount(plan ReplatformingPlan) int {
	count := 0
	for _, item := range plan.Items {
		if item.ImportCandidate != nil && item.ImportCandidate.Status == ReplatformingImportStatusRefused {
			count++
		}
	}
	return count
}
