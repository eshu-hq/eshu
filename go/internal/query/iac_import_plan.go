package query

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type terraformImportPlanCandidate struct {
	ID                       string                  `json:"id"`
	FindingID                string                  `json:"finding_id"`
	Status                   string                  `json:"status"`
	Provider                 string                  `json:"provider"`
	AccountID                string                  `json:"account_id,omitempty"`
	Region                   string                  `json:"region,omitempty"`
	ARN                      string                  `json:"arn,omitempty"`
	CloudResourceType        string                  `json:"cloud_resource_type,omitempty"`
	TerraformResourceType    string                  `json:"terraform_resource_type,omitempty"`
	ImportID                 string                  `json:"import_id,omitempty"`
	SuggestedResourceAddress string                  `json:"suggested_resource_address,omitempty"`
	DestinationHint          string                  `json:"destination_hint"`
	ConfigurationShape       string                  `json:"configuration_shape"`
	ProviderHint             terraformProviderHint   `json:"provider_hint"`
	Warnings                 []string                `json:"warnings,omitempty"`
	RefusalReasons           []string                `json:"refusal_reasons,omitempty"`
	ImportBlock              string                  `json:"import_block,omitempty"`
	EvidenceRefs             []string                `json:"evidence_refs,omitempty"`
	SafetyGate               IaCManagementSafetyGate `json:"safety_gate"`
}

type terraformProviderHint struct {
	Provider  string `json:"provider"`
	AccountID string `json:"account_id,omitempty"`
	Region    string `json:"region,omitempty"`
	Alias     string `json:"alias,omitempty"`
}

type terraformImportPlanArtifact struct {
	Format string `json:"format"`
	HCL    string `json:"hcl"`
}

type terraformImportPlanSummary struct {
	Candidates []terraformImportPlanCandidate
	Artifact   terraformImportPlanArtifact
	Ready      int
	Refused    int
}

type terraformImportMapping struct {
	ResourceType string
	ImportID     func(IaCManagementFindingRow) string
}

func (h *IaCHandler) handleTerraformImportPlanCandidates(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryIaCTerraformImportPlan,
		"POST /api/v0/iac/terraform-import-plan/candidates",
		iacTerraformImportCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), iacTerraformImportCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"Terraform import-plan candidates require reducer-materialized AWS runtime drift findings",
			ErrorCodeUnsupportedCapability,
			iacTerraformImportCapability,
			h.profile(),
			requiredProfile(iacTerraformImportCapability),
		)
		return
	}
	var req iacManagementRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if resourceID := strings.TrimSpace(req.ResourceID); resourceID != "" && !strings.HasPrefix(resourceID, "arn:aws:") {
		WriteError(w, http.StatusBadRequest, "resource_id must be the full AWS ARN for Terraform import-plan candidates")
		return
	}
	filter, err := normalizeIaCManagementRequest(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
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
	summary := buildTerraformImportPlanCandidates(normalizeIaCManagementFindingsSafety(findings), filter)

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"story":                 terraformImportPlanStory(summary, totalFindings),
		"account_id":            filter.AccountID,
		"scope_id":              filter.ScopeID,
		"region":                filter.Region,
		"arn":                   filter.ARN,
		"finding_kinds":         filter.FindingKinds,
		"candidates":            summary.Candidates,
		"candidates_count":      len(summary.Candidates),
		"ready_count":           summary.Ready,
		"refused_count":         summary.Refused,
		"total_findings_count":  totalFindings,
		"limit":                 filter.Limit,
		"offset":                filter.Offset,
		"truncated":             iacManagementTruncated(filter.Offset, len(findings), totalFindings),
		"next_offset":           iacManagementNextOffset(filter.Offset, len(findings), totalFindings),
		"terraform_import_plan": summary.Artifact,
		"truth_basis":           "materialized_reducer_rows",
		"analysis_status":       "terraform_import_plan_candidates",
		"limitations": []string{
			"read-only surface; does not run Terraform, import resources, or mutate cloud state",
			"only safety-approved cloud_only findings for supported AWS resource families receive import blocks",
			"security-review, ambiguous, unknown, stale, state-only, and unsupported findings are returned as refused candidates",
		},
	}, BuildTruthEnvelope(
		h.profile(),
		iacTerraformImportCapability,
		TruthBasisSemanticFacts,
		"derived Terraform import-plan candidates from bounded IaC management findings",
	))
}

func buildTerraformImportPlanCandidates(findings []IaCManagementFindingRow, filter IaCManagementFilter) terraformImportPlanSummary {
	candidates := make([]terraformImportPlanCandidate, 0, len(findings))
	var blocks []string
	ready := 0
	refused := 0
	for _, finding := range findings {
		candidate := terraformImportPlanCandidateForFinding(finding, filter)
		candidates = append(candidates, candidate)
		if candidate.Status == "ready" {
			ready++
			blocks = append(blocks, candidate.ImportBlock)
		} else {
			refused++
		}
	}
	return terraformImportPlanSummary{
		Candidates: candidates,
		Artifact: terraformImportPlanArtifact{
			Format: "terraform_import_blocks",
			HCL:    strings.Join(blocks, "\n"),
		},
		Ready:   ready,
		Refused: refused,
	}
}

func terraformImportPlanCandidateForFinding(finding IaCManagementFindingRow, filter IaCManagementFilter) terraformImportPlanCandidate {
	normalizeIaCManagementFindingSafety(&finding)
	accountID := terraformImportAccountID(finding, filter)
	region := terraformImportRegion(finding, filter)
	candidate := terraformImportPlanCandidate{
		ID:                 "terraform-import:" + finding.ID,
		FindingID:          finding.ID,
		Status:             "refused",
		Provider:           "aws",
		AccountID:          accountID,
		Region:             region,
		ARN:                finding.ARN,
		CloudResourceType:  finding.ResourceType,
		DestinationHint:    terraformImportDestinationHint(finding),
		ConfigurationShape: terraformImportConfigurationShape(finding),
		ProviderHint: terraformProviderHint{
			Provider:  "aws",
			AccountID: accountID,
			Region:    region,
			Alias:     terraformProviderAlias(accountID, region),
		},
		Warnings:     iacMergeStringSets(finding.WarningFlags, finding.SafetyGate.Warnings),
		EvidenceRefs: terraformImportEvidenceRefs(finding),
		SafetyGate:   finding.SafetyGate,
	}
	if finding.SafetyGate.ReviewRequired || terraformImportStringSliceContains(finding.SafetyGate.RefusedActions, "terraform_import_plan") {
		candidate.RefusalReasons = []string{"security_review_required"}
		return candidate
	}
	if finding.ManagementStatus != managementStatusCloudOnly {
		candidate.RefusalReasons = []string{"management_status_not_importable"}
		return candidate
	}
	mapping, ok := terraformImportMappingForFinding(finding)
	if !ok {
		candidate.RefusalReasons = []string{"unsupported_resource_type"}
		candidate.Warnings = iacMergeStringSets(candidate.Warnings, []string{"unsupported_terraform_import_mapping"})
		return candidate
	}
	importID := strings.TrimSpace(mapping.ImportID(finding))
	if importID == "" {
		candidate.RefusalReasons = []string{"missing_provider_import_id"}
		return candidate
	}
	candidate.Status = "ready"
	candidate.TerraformResourceType = mapping.ResourceType
	candidate.ImportID = importID
	candidate.SuggestedResourceAddress = mapping.ResourceType + "." + terraformResourceName(importID)
	candidate.ImportBlock = terraformImportBlock(candidate.SuggestedResourceAddress, candidate.ImportID)
	return candidate
}

func terraformImportAccountID(finding IaCManagementFindingRow, filter IaCManagementFilter) string {
	if strings.TrimSpace(finding.AccountID) != "" {
		return strings.TrimSpace(finding.AccountID)
	}
	if strings.TrimSpace(filter.AccountID) != "" {
		return strings.TrimSpace(filter.AccountID)
	}
	scope := terraformImportScopeParts(filter.ScopeID)
	return scope.accountID
}

func terraformImportRegion(finding IaCManagementFindingRow, filter IaCManagementFilter) string {
	if strings.TrimSpace(finding.Region) != "" {
		return strings.TrimSpace(finding.Region)
	}
	if strings.TrimSpace(filter.Region) != "" {
		return strings.TrimSpace(filter.Region)
	}
	scope := terraformImportScopeParts(filter.ScopeID)
	return scope.region
}

type terraformImportScope struct {
	accountID string
	region    string
}

func terraformImportScopeParts(scopeID string) terraformImportScope {
	parts := strings.Split(scopeID, ":")
	if len(parts) < 4 || parts[0] != "aws" {
		return terraformImportScope{}
	}
	return terraformImportScope{
		accountID: strings.TrimSpace(parts[1]),
		region:    strings.TrimSpace(parts[2]),
	}
}

func terraformImportMappingForFinding(finding IaCManagementFindingRow) (terraformImportMapping, bool) {
	switch strings.ToLower(strings.TrimSpace(finding.ResourceType)) {
	case "s3":
		return terraformImportMapping{ResourceType: "aws_s3_bucket", ImportID: terraformImportS3BucketID}, true
	case "lambda":
		return terraformImportMapping{ResourceType: "aws_lambda_function", ImportID: terraformImportLambdaFunctionID}, true
	default:
		return terraformImportMapping{}, false
	}
}

func terraformImportS3BucketID(finding IaCManagementFindingRow) string {
	if strings.TrimSpace(finding.ResourceID) != "" {
		return strings.TrimSpace(finding.ResourceID)
	}
	const s3ARNPrefix = "arn:aws:s3:::"
	return strings.TrimPrefix(strings.TrimSpace(finding.ARN), s3ARNPrefix)
}

func terraformImportLambdaFunctionID(finding IaCManagementFindingRow) string {
	resourceID := strings.TrimSpace(finding.ResourceID)
	if strings.HasPrefix(resourceID, "function:") {
		return strings.TrimPrefix(resourceID, "function:")
	}
	return resourceID
}

func terraformResourceName(importID string) string {
	value := strings.ToLower(strings.TrimSpace(importID))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		return "resource"
	}
	if name[0] >= '0' && name[0] <= '9' {
		return "resource_" + name
	}
	return name
}

func terraformImportBlock(address string, importID string) string {
	return fmt.Sprintf("import {\n  to = %s\n  id = %q\n}\n", address, importID)
}

func terraformImportDestinationHint(finding IaCManagementFindingRow) string {
	if strings.TrimSpace(finding.MatchedTerraformModulePath) != "" {
		return "module:" + strings.TrimSpace(finding.MatchedTerraformModulePath)
	}
	if strings.TrimSpace(finding.MatchedTerraformConfigFile) != "" {
		return "file:" + strings.TrimSpace(finding.MatchedTerraformConfigFile)
	}
	return "operator_selected_target"
}

func terraformImportConfigurationShape(finding IaCManagementFindingRow) string {
	if strings.TrimSpace(finding.MatchedTerraformModulePath) != "" {
		return "module_shaped"
	}
	return "flat_starter"
}

func terraformProviderAlias(accountID string, region string) string {
	accountID = strings.TrimSpace(accountID)
	region = strings.TrimSpace(region)
	if accountID == "" && region == "" {
		return ""
	}
	return terraformResourceName(strings.Trim(accountID+"_"+region, "_"))
}

func terraformImportEvidenceRefs(finding IaCManagementFindingRow) []string {
	refs := make([]string, 0, len(finding.Evidence)+1)
	if strings.TrimSpace(finding.ID) != "" {
		refs = append(refs, finding.ID)
	}
	for _, evidence := range finding.Evidence {
		if strings.TrimSpace(evidence.ID) != "" {
			refs = append(refs, evidence.ID)
		}
	}
	sort.Strings(refs)
	return refs
}

func terraformImportPlanStory(summary terraformImportPlanSummary, total int) string {
	return fmt.Sprintf(
		"Terraform import-plan candidate generation inspected %d active IaC management findings; %d ready and %d refused.",
		total,
		summary.Ready,
		summary.Refused,
	)
}

func terraformImportStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
