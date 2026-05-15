package query

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type iacManagementFindingGroup struct {
	ManagementStatus string   `json:"management_status"`
	FindingKind      string   `json:"finding_kind"`
	Count            int      `json:"count"`
	ARNs             []string `json:"arns,omitempty"`
}

type iacManagementEvidenceGroup struct {
	Layer    string                     `json:"layer"`
	Count    int                        `json:"count"`
	Evidence []IaCManagementEvidenceRow `json:"evidence"`
}

func (h *IaCHandler) handleIaCManagementStatus(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryIaCManagementStatus,
		"POST /api/v0/iac/management-status",
		iacManagementStatusCapability,
	)
	defer span.End()

	filter, err := h.readExactIaCManagementFilter(w, r, iacManagementStatusCapability)
	if err != nil {
		return
	}
	finding, total, err := h.loadExactIaCManagementFinding(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := managementStatusUnknown
	analysisStatus := "no_active_management_finding"
	if finding != nil {
		status = finding.ManagementStatus
		analysisStatus = "active_management_finding"
	}
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"story":                iacManagementStatusStory(filter, finding),
		"arn":                  filter.ARN,
		"scope_id":             filter.ScopeID,
		"account_id":           filter.AccountID,
		"region":               filter.Region,
		"management_status":    status,
		"analysis_status":      analysisStatus,
		"finding":              finding,
		"total_findings_count": total,
		"limitations":          iacManagementStatusLimitations(finding),
	}, BuildTruthEnvelope(
		h.profile(),
		iacManagementStatusCapability,
		TruthBasisSemanticFacts,
		"resolved from one exact active AWS runtime drift finding",
	))
}

func (h *IaCHandler) handleIaCManagementExplanation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryIaCManagementExplanation,
		"POST /api/v0/iac/management-status/explain",
		iacManagementExplainCapability,
	)
	defer span.End()

	filter, err := h.readExactIaCManagementFilter(w, r, iacManagementExplainCapability)
	if err != nil {
		return
	}
	finding, total, err := h.loadExactIaCManagementFinding(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"story":                iacManagementStatusStory(filter, finding),
		"arn":                  filter.ARN,
		"scope_id":             filter.ScopeID,
		"account_id":           filter.AccountID,
		"region":               filter.Region,
		"finding":              finding,
		"evidence_groups":      iacManagementEvidenceGroups(finding),
		"total_findings_count": total,
		"limitations":          iacManagementStatusLimitations(finding),
	}, BuildTruthEnvelope(
		h.profile(),
		iacManagementExplainCapability,
		TruthBasisSemanticFacts,
		"explained from one exact active AWS runtime drift finding",
	))
}

func (h *IaCHandler) readExactIaCManagementFilter(
	w http.ResponseWriter,
	r *http.Request,
	capability string,
) (IaCManagementFilter, error) {
	if capabilityUnsupported(h.profile(), capability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"IaC management status requires reducer-materialized AWS runtime drift findings",
			ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			requiredProfile(capability),
		)
		return IaCManagementFilter{}, fmt.Errorf("unsupported capability")
	}
	var req iacManagementRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return IaCManagementFilter{}, err
	}
	filter, err := normalizeIaCManagementRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return IaCManagementFilter{}, err
	}
	if strings.TrimSpace(filter.ARN) == "" {
		err := fmt.Errorf("arn or resource_id is required")
		WriteError(w, http.StatusBadRequest, err.Error())
		return IaCManagementFilter{}, err
	}
	filter.Limit = 1
	filter.Offset = 0
	if h == nil || h.Management == nil {
		err := fmt.Errorf("IaC management store is required")
		WriteError(w, http.StatusServiceUnavailable, err.Error())
		return IaCManagementFilter{}, err
	}
	return filter, nil
}

func (h *IaCHandler) loadExactIaCManagementFinding(
	ctx context.Context,
	filter IaCManagementFilter,
) (*IaCManagementFindingRow, int, error) {
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
	return &rows[0], total, nil
}

func iacManagementListStory(filter IaCManagementFilter, findings []IaCManagementFindingRow, total int) string {
	scope := iacFirstNonEmpty(filter.ScopeID, filter.AccountID)
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

func iacManagementStatusStory(filter IaCManagementFilter, finding *IaCManagementFindingRow) string {
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

func iacManagementFindingGroups(findings []IaCManagementFindingRow) []iacManagementFindingGroup {
	byKey := map[string]*iacManagementFindingGroup{}
	var keys []string
	for _, finding := range findings {
		key := finding.ManagementStatus + "\x00" + finding.FindingKind
		group := byKey[key]
		if group == nil {
			group = &iacManagementFindingGroup{
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
	out := make([]iacManagementFindingGroup, 0, len(keys))
	for _, key := range keys {
		group := byKey[key]
		sort.Strings(group.ARNs)
		out = append(out, *group)
	}
	return out
}

func iacManagementEvidenceGroups(finding *IaCManagementFindingRow) []iacManagementEvidenceGroup {
	if finding == nil || len(finding.Evidence) == 0 {
		return nil
	}
	byLayer := map[string][]IaCManagementEvidenceRow{}
	for _, atom := range finding.Evidence {
		layer := iacManagementEvidenceLayer(atom)
		byLayer[layer] = append(byLayer[layer], atom)
	}
	layers := make([]string, 0, len(byLayer))
	for layer := range byLayer {
		layers = append(layers, layer)
	}
	sort.Strings(layers)
	out := make([]iacManagementEvidenceGroup, 0, len(layers))
	for _, layer := range layers {
		evidence := byLayer[layer]
		out = append(out, iacManagementEvidenceGroup{
			Layer:    layer,
			Count:    len(evidence),
			Evidence: evidence,
		})
	}
	return out
}

func iacManagementEvidenceLayer(atom IaCManagementEvidenceRow) string {
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

func iacManagementStatusLimitations(finding *IaCManagementFindingRow) []string {
	limitations := []string{
		"read-only surface; does not run Terraform, import resources, or mutate cloud state",
		"bounded to active AWS runtime drift reducer facts for the supplied scope/account and ARN",
	}
	if finding == nil {
		limitations = append(limitations, "no active finding may mean managed, not collected, stale, or outside current AWS coverage")
	}
	return limitations
}
