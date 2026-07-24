// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	awsRuntimeDriftOutcomeExact     = "exact"
	awsRuntimeDriftOutcomeDerived   = "derived"
	awsRuntimeDriftOutcomeAmbiguous = "ambiguous"
	awsRuntimeDriftOutcomeStale     = "stale"
	awsRuntimeDriftOutcomeUnknown   = "unknown"

	awsRuntimeDriftPromotionNotPromoted = "not_promoted"
	awsRuntimeDriftPromotionRejected    = "rejected"
)

// AWSRuntimeDriftFindingRow exposes one active AWS runtime drift finding with
// query-facing outcome and promotion status fields.
type AWSRuntimeDriftFindingRow struct {
	IaCManagementFindingRow
	Outcome          string `json:"outcome"`
	PromotionOutcome string `json:"promotion_outcome"`
	PromotionReason  string `json:"promotion_reason"`
}

type awsRuntimeDriftOutcomeGroup struct {
	Outcome string   `json:"outcome"`
	Count   int      `json:"count"`
	ARNs    []string `json:"arns,omitempty"`
}

func (h *IaCHandler) handleAWSRuntimeDriftFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryAWSRuntimeDriftFindings,
		"POST /api/v0/aws/runtime-drift/findings",
		awsRuntimeDriftFindingsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), awsRuntimeDriftFindingsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"AWS runtime drift findings require reducer-materialized AWS drift facts",
			ErrorCodeUnsupportedCapability,
			awsRuntimeDriftFindingsCapability,
			h.profile(),
			requiredProfile(awsRuntimeDriftFindingsCapability),
		)
		return
	}

	var req iacManagementRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := normalizeAWSRuntimeDriftFindingsRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h == nil || h.Management == nil {
		WriteError(w, http.StatusServiceUnavailable, "AWS runtime drift finding store is required")
		return
	}

	// Access scoping (#5167 Group B): IaCManagementStore/IaCManagementFilter
	// are shared with the iac/* and replatforming/* route families (owned by
	// other #5167 workstreams: iac_management.go, iac_management_surface.go,
	// iac_import_plan.go, replatforming_*_handler.go), so the fix here is a
	// caller-side grant precheck on this handler's own resolved filter rather
	// than a store/filter-level change that would also alter those routes'
	// behavior. A scoped caller must supply an exact scope_id (an account_id-
	// only filter fans out via a LIKE prefix scan across every region/service
	// scope under that account, which this precheck cannot safely narrow
	// without a store change); a scoped caller with no grants, no scope_id, or
	// a scope_id outside its granted repositories/ingestion scopes gets the
	// same zero-finding page a real empty result would produce.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() || (access.scoped() && (filter.ScopeID == "" || !access.allowsRepositoryID(filter.ScopeID))) {
		writeAWSRuntimeDriftFindings(w, r, h, filter, nil, 0)
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
	driftFindings := awsRuntimeDriftFindingRows(findings)
	writeAWSRuntimeDriftFindings(w, r, h, filter, driftFindings, totalFindings)
}

// writeAWSRuntimeDriftFindings renders the bounded findings-page response.
// Passing nil driftFindings and totalFindings 0 renders the same
// zero-finding shape a real empty result would, used by both the
// genuine-empty-result path and the #5167 access-scoping precheck in
// handleAWSRuntimeDriftFindings.
func writeAWSRuntimeDriftFindings(
	w http.ResponseWriter,
	r *http.Request,
	h *IaCHandler,
	filter IaCManagementFilter,
	driftFindings []AWSRuntimeDriftFindingRow,
	totalFindings int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"scope_id":             filter.ScopeID,
		"account_id":           filter.AccountID,
		"region":               filter.Region,
		"arn":                  filter.ARN,
		"story":                awsRuntimeDriftFindingsStory(filter, driftFindings, totalFindings),
		"finding_kinds":        filter.FindingKinds,
		"outcome_groups":       awsRuntimeDriftOutcomeGroups(driftFindings),
		"drift_findings":       driftFindings,
		"findings_count":       len(driftFindings),
		"total_findings_count": totalFindings,
		"limit":                filter.Limit,
		"offset":               filter.Offset,
		"truncated":            iacManagementTruncated(filter.Offset, len(driftFindings), totalFindings),
		"next_offset":          iacManagementNextOffset(filter.Offset, len(driftFindings), totalFindings),
		"truth_basis":          "materialized_reducer_rows",
		"analysis_status":      "materialized_aws_runtime_drift",
		"graph_projection_note": "read-model-backed drift surface; graph projection remains gated " +
			"until Cypher shape and performance proof are frozen",
		"limitations": []string{
			"bounded to active AWS runtime drift reducer facts for the requested scope or account",
			"outcome is derived from management status and evidence strength without promoting service ownership",
			"rejected promotion means the read-only finding must not drive Terraform import or cleanup automation",
		},
	}, BuildTruthEnvelope(
		h.profile(),
		awsRuntimeDriftFindingsCapability,
		TruthBasisSemanticFacts,
		"resolved from active reducer-materialized AWS runtime drift findings",
	))
}

// normalizeAWSRuntimeDriftFindingsRequest normalizes the shared IaC
// management request for the AWS runtime drift findings route (#5453).
// handleUnmanagedCloudResources also calls normalizeIaCManagementRequest
// directly and intentionally keeps its narrower existence-only default (see
// TestHandleUnmanagedCloudResourcesDefaultsToActionableAWSFindingKinds): that
// route is scoped to unmanaged-resource triage. This route is the "runtime
// drift findings" surface, so when the caller names no explicit
// finding_kinds this widens the shared existence-kind default to also
// include image_version_drift -- a managed-but-value-drifted resource is not
// "unmanaged" and must not be structurally excluded from its own default
// drift-findings page.
func normalizeAWSRuntimeDriftFindingsRequest(req iacManagementRequest) (IaCManagementFilter, error) {
	filter, err := normalizeIaCManagementRequest(req)
	if err != nil {
		return IaCManagementFilter{}, err
	}
	// Widen the default finding-kind set to include image_version_drift only when
	// the caller named no explicit kind. normalizeIaCManagementFindingKinds
	// strips blank/whitespace-only entries before applying the existence-only
	// default, so guard on whether any NON-BLANK kind was supplied rather than on
	// the raw slice length -- otherwise a request like finding_kinds=["  "] would
	// fall through to the narrow default and silently exclude image_version_drift
	// from its own drift-findings page.
	callerNamedKind := false
	for _, kind := range req.FindingKinds {
		if strings.TrimSpace(kind) != "" {
			callerNamedKind = true
			break
		}
	}
	if !callerNamedKind {
		filter.FindingKinds = append(filter.FindingKinds, findingKindImageVersionDrift)
		sort.Strings(filter.FindingKinds)
	}
	return filter, nil
}

func awsRuntimeDriftFindingRows(findings []IaCManagementFindingRow) []AWSRuntimeDriftFindingRow {
	out := make([]AWSRuntimeDriftFindingRow, 0, len(findings))
	for _, finding := range findings {
		out = append(out, AWSRuntimeDriftFindingRow{
			IaCManagementFindingRow: finding,
			Outcome:                 awsRuntimeDriftOutcome(finding),
			PromotionOutcome:        awsRuntimeDriftPromotionOutcome(finding),
			PromotionReason:         awsRuntimeDriftPromotionReason(finding),
		})
	}
	return out
}

func awsRuntimeDriftOutcome(finding IaCManagementFindingRow) string {
	switch strings.TrimSpace(finding.ManagementStatus) {
	case managementStatusManagedByTerraform:
		return awsRuntimeDriftOutcomeExact
	case managementStatusTerraformStateOnly,
		managementStatusTerraformConfigOnly,
		managementStatusCloudOnly,
		managementStatusManagedByOtherIaC:
		return awsRuntimeDriftOutcomeDerived
	case managementStatusAmbiguous:
		return awsRuntimeDriftOutcomeAmbiguous
	case managementStatusStaleIaCCandidate:
		return awsRuntimeDriftOutcomeStale
	case managementStatusUnknown:
		return awsRuntimeDriftOutcomeUnknown
	default:
		return awsRuntimeDriftOutcomeUnknown
	}
}

func awsRuntimeDriftPromotionOutcome(finding IaCManagementFindingRow) string {
	if finding.SafetyGate.ReviewRequired {
		return awsRuntimeDriftPromotionRejected
	}
	return awsRuntimeDriftPromotionNotPromoted
}

func awsRuntimeDriftPromotionReason(finding IaCManagementFindingRow) string {
	if finding.SafetyGate.ReviewRequired {
		return "safety_gate_requires_review"
	}
	return "read_model_only_no_ownership_promotion"
}

func awsRuntimeDriftOutcomeGroups(findings []AWSRuntimeDriftFindingRow) []awsRuntimeDriftOutcomeGroup {
	byOutcome := map[string]*awsRuntimeDriftOutcomeGroup{}
	var outcomes []string
	for _, finding := range findings {
		group := byOutcome[finding.Outcome]
		if group == nil {
			group = &awsRuntimeDriftOutcomeGroup{Outcome: finding.Outcome}
			byOutcome[finding.Outcome] = group
			outcomes = append(outcomes, finding.Outcome)
		}
		group.Count++
		group.ARNs = append(group.ARNs, finding.ARN)
	}
	sort.Strings(outcomes)
	out := make([]awsRuntimeDriftOutcomeGroup, 0, len(outcomes))
	for _, outcome := range outcomes {
		group := byOutcome[outcome]
		sort.Strings(group.ARNs)
		out = append(out, *group)
	}
	return out
}

func awsRuntimeDriftFindingsStory(
	filter IaCManagementFilter,
	findings []AWSRuntimeDriftFindingRow,
	total int,
) string {
	scope := iacFirstNonEmpty(filter.ScopeID, filter.AccountID)
	if scope == "" {
		scope = "requested AWS scope"
	}
	return fmt.Sprintf(
		"%d active AWS runtime drift findings matched %s; %d returned in this page.",
		total,
		scope,
		len(findings),
	)
}
