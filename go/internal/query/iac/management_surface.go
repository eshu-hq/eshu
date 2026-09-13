// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type managementFindingGroup struct {
	ManagementStatus string   `json:"management_status"`
	FindingKind      string   `json:"finding_kind"`
	Count            int      `json:"count"`
	ARNs             []string `json:"arns,omitempty"`
}

type managementEvidenceGroup struct {
	Layer    string                  `json:"layer"`
	Count    int                     `json:"count"`
	Evidence []ManagementEvidenceRow `json:"evidence"`
}

func (h *Handler) handleIaCManagementStatus(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryIaCManagementStatus,
		"POST /api/v0/iac/management-status",
		ManagementStatusCapability,
	)
	defer span.End()

	filter, err := h.readExactIaCManagementFilter(w, r, ManagementStatusCapability)
	if err != nil {
		return
	}
	finding, total, err := h.loadExactIaCManagementFinding(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := ManagementStatusUnknown
	analysisStatus := "no_active_management_finding"
	safetyGate := managementEmptySafetyGate()
	if finding != nil {
		status = finding.ManagementStatus
		analysisStatus = "active_management_finding"
		safetyGate = finding.SafetyGate
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"story":                managementStatusStory(filter, finding),
		"arn":                  filter.ARN,
		"scope_id":             filter.ScopeID,
		"account_id":           filter.AccountID,
		"region":               filter.Region,
		"management_status":    status,
		"analysis_status":      analysisStatus,
		"finding":              finding,
		"safety_gate":          safetyGate,
		"total_findings_count": total,
		"limitations":          managementStatusLimitations(finding),
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		ManagementStatusCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from one exact active AWS runtime drift finding",
	))
}

func (h *Handler) handleIaCManagementExplanation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryIaCManagementExplanation,
		"POST /api/v0/iac/management-status/explain",
		ManagementExplainCapability,
	)
	defer span.End()

	filter, err := h.readExactIaCManagementFilter(w, r, ManagementExplainCapability)
	if err != nil {
		return
	}
	finding, total, err := h.loadExactIaCManagementFinding(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"story":                managementStatusStory(filter, finding),
		"arn":                  filter.ARN,
		"scope_id":             filter.ScopeID,
		"account_id":           filter.AccountID,
		"region":               filter.Region,
		"finding":              finding,
		"evidence_groups":      managementEvidenceGroups(finding),
		"safety_gate":          managementSafetyGateForExactFinding(finding),
		"total_findings_count": total,
		"limitations":          managementStatusLimitations(finding),
	}, querycontract.BuildTruthEnvelope(
		h.profile(),
		ManagementExplainCapability,
		querycontract.TruthBasisSemanticFacts,
		"explained from one exact active AWS runtime drift finding",
	))
}

// readExactIaCManagementFilter reads and validates the bounded exact-selector
// IaC management filter shared by handleIaCManagementStatus and
// handleIaCManagementExplanation, then binds it to the caller's exact AWS
// collector-scope grant (#5167 W4, bindIaCManagementFilterAccess) before any
// store read.
func (h *Handler) readExactIaCManagementFilter(
	w http.ResponseWriter,
	r *http.Request,
	capability string,
) (ManagementFilter, error) {
	if querycontract.CapabilityUnsupported(h.profile(), capability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"IaC management status requires reducer-materialized AWS runtime drift findings",
			querycontract.ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			querycontract.RequiredProfile(capability),
		)
		return ManagementFilter{}, fmt.Errorf("unsupported capability")
	}
	var req managementRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return ManagementFilter{}, err
	}
	filter, err := normalizeIaCManagementRequest(req)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return ManagementFilter{}, err
	}
	if strings.TrimSpace(filter.ARN) == "" {
		err := fmt.Errorf("arn or resource_id is required")
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return ManagementFilter{}, err
	}
	filter.Limit = 1
	filter.Offset = 0
	filter = bindIaCManagementFilterAccess(r.Context(), filter)
	if h == nil || h.Management == nil {
		err := fmt.Errorf("IaC management store is required")
		querycontract.WriteError(w, http.StatusServiceUnavailable, err.Error())
		return ManagementFilter{}, err
	}
	return filter, nil
}

func (h *Handler) loadExactIaCManagementFinding(
	ctx context.Context,
	filter ManagementFilter,
) (*ManagementFindingRow, int, error) {
	total, err := h.Management.CountUnmanagedCloudResources(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	rows, err := h.Management.ListUnmanagedCloudResources(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, total, nil
	}
	finding := rows[0]
	NormalizeManagementFindingSafety(&finding)
	return &finding, total, nil
}

func managementSafetyGateForExactFinding(finding *ManagementFindingRow) ManagementSafetyGate {
	if finding == nil {
		return managementEmptySafetyGate()
	}
	return finding.SafetyGate
}

func managementListStory(filter ManagementFilter, findings []ManagementFindingRow, total int) string {
	scope := firstNonEmpty(filter.ScopeID, filter.AccountID)
	if scope == "" {
		scope = "requested AWS scope"
	}
	return fmt.Sprintf(
		"%d active IaC management findings matched %s; %d returned in this page.",
		total,
		scope,
		len(findings),
	)
}

func managementStatusStory(filter ManagementFilter, finding *ManagementFindingRow) string {
	if finding == nil {
		return fmt.Sprintf(
			"No active AWS runtime drift finding matched %s; absence of a finding is not proof of Terraform ownership.",
			filter.ARN,
		)
	}
	return fmt.Sprintf(
		"%s is classified as %s from %s evidence.",
		finding.ARN,
		finding.ManagementStatus,
		finding.FindingKind,
	)
}

func managementFindingGroups(findings []ManagementFindingRow) []managementFindingGroup {
	byKey := map[string]*managementFindingGroup{}
	var keys []string
	for _, finding := range findings {
		key := finding.ManagementStatus + "\x00" + finding.FindingKind
		group := byKey[key]
		if group == nil {
			group = &managementFindingGroup{
				ManagementStatus: finding.ManagementStatus,
				FindingKind:      finding.FindingKind,
			}
			byKey[key] = group
			keys = append(keys, key)
		}
		group.Count++
		group.ARNs = append(group.ARNs, finding.ARN)
	}
	sort.Strings(keys)
	out := make([]managementFindingGroup, 0, len(keys))
	for _, key := range keys {
		group := byKey[key]
		sort.Strings(group.ARNs)
		out = append(out, *group)
	}
	return out
}

func managementEvidenceGroups(finding *ManagementFindingRow) []managementEvidenceGroup {
	if finding == nil || len(finding.Evidence) == 0 {
		return nil
	}
	byLayer := map[string][]ManagementEvidenceRow{}
	for _, atom := range finding.Evidence {
		layer := managementEvidenceLayer(atom)
		byLayer[layer] = append(byLayer[layer], atom)
	}
	layers := make([]string, 0, len(byLayer))
	for layer := range byLayer {
		layers = append(layers, layer)
	}
	sort.Strings(layers)
	out := make([]managementEvidenceGroup, 0, len(layers))
	for _, layer := range layers {
		evidence := byLayer[layer]
		out = append(out, managementEvidenceGroup{
			Layer:    layer,
			Count:    len(evidence),
			Evidence: evidence,
		})
	}
	return out
}

func managementEvidenceLayer(atom ManagementEvidenceRow) string {
	kind := strings.ToLower(strings.TrimSpace(atom.EvidenceType))
	switch {
	case strings.HasPrefix(kind, "aws_raw_tag"):
		return "raw_tags"
	case strings.HasPrefix(kind, "aws_cloud"):
		return "cloud"
	case strings.HasPrefix(kind, "terraform_state"):
		return "terraform_state"
	case strings.HasPrefix(kind, "terraform_config"):
		return "terraform_config"
	case strings.Contains(kind, "ambiguous") || strings.Contains(kind, "coverage_gap"):
		return "management_status"
	default:
		return "other"
	}
}

func managementStatusLimitations(finding *ManagementFindingRow) []string {
	limitations := []string{
		"read-only surface; does not run Terraform, import resources, or mutate cloud state",
		"bounded to active AWS runtime drift reducer facts for the supplied scope/account and ARN",
	}
	if finding == nil {
		limitations = append(limitations, "no active finding may mean managed, not collected, stale, or outside current AWS coverage")
	}
	return limitations
}
