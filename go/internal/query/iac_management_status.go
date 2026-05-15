package query

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	managementStatusManagedByTerraform  = "managed_by_terraform"
	managementStatusTerraformStateOnly  = "terraform_state_only"
	managementStatusTerraformConfigOnly = "terraform_config_only"
	managementStatusCloudOnly           = "cloud_only"
	managementStatusManagedByOtherIaC   = "managed_by_other_iac"
	managementStatusAmbiguous           = "ambiguous_management"
	managementStatusUnknown             = "unknown_management"
	managementStatusStaleIaCCandidate   = "stale_iac_candidate"
)

type iacManagementStatusInput struct {
	FindingKind                string
	HasCloudEvidence           bool
	HasTerraformStateEvidence  bool
	HasTerraformConfigEvidence bool
	HasOtherIaCEvidence        bool
	HasConflictingEvidence     bool
	HasCoverageGapEvidence     bool
	HasStaleIaCEvidence        bool
}

func (i *iacManagementStatusInput) recordEvidence(evidenceType string) {
	normalized := strings.ToLower(strings.TrimSpace(evidenceType))
	switch normalized {
	case "aws_cloud_resource", "aws_cloud_resource_arn", "cloud_resource":
		i.HasCloudEvidence = true
	case "terraform_state_resource":
		i.HasTerraformStateEvidence = true
	case "terraform_config_resource":
		i.HasTerraformConfigEvidence = true
	}
	if strings.Contains(normalized, "cloudformation") ||
		strings.Contains(normalized, "cdk") ||
		strings.Contains(normalized, "pulumi") ||
		strings.Contains(normalized, "crossplane") ||
		strings.Contains(normalized, "serverless") ||
		normalized == "other_iac_resource" {
		i.HasOtherIaCEvidence = true
	}
	if strings.Contains(normalized, "conflict") || strings.Contains(normalized, "ambiguous") {
		i.HasConflictingEvidence = true
	}
	if strings.Contains(normalized, "coverage_gap") ||
		strings.Contains(normalized, "permission_gap") ||
		strings.Contains(normalized, "missing_permission") ||
		strings.Contains(normalized, "unsupported_resource") {
		i.HasCoverageGapEvidence = true
	}
	if strings.Contains(normalized, "stale") || strings.Contains(normalized, "missing_cloud_resource") {
		i.HasStaleIaCEvidence = true
	}
}

func deriveIaCManagementStatus(input iacManagementStatusInput) string {
	switch strings.TrimSpace(input.FindingKind) {
	case findingKindOrphanedCloudResource:
		input.HasCloudEvidence = true
	case findingKindUnmanagedCloudResource:
		input.HasCloudEvidence = true
		input.HasTerraformStateEvidence = true
	}

	if input.HasConflictingEvidence ||
		(input.HasOtherIaCEvidence && (input.HasTerraformStateEvidence || input.HasTerraformConfigEvidence)) {
		return managementStatusAmbiguous
	}
	if input.HasCoverageGapEvidence {
		return managementStatusUnknown
	}
	if input.HasStaleIaCEvidence {
		return managementStatusStaleIaCCandidate
	}
	if input.HasCloudEvidence && input.HasTerraformStateEvidence && input.HasTerraformConfigEvidence {
		return managementStatusManagedByTerraform
	}
	if input.HasCloudEvidence && input.HasTerraformStateEvidence {
		return managementStatusTerraformStateOnly
	}
	if input.HasTerraformConfigEvidence && !input.HasCloudEvidence && !input.HasTerraformStateEvidence {
		return managementStatusTerraformConfigOnly
	}
	if input.HasCloudEvidence && input.HasOtherIaCEvidence {
		return managementStatusManagedByOtherIaC
	}
	if input.HasCloudEvidence {
		return managementStatusCloudOnly
	}
	return managementStatusUnknown
}

func missingEvidenceForManagementStatus(status string) []string {
	switch status {
	case managementStatusTerraformStateOnly:
		return []string{"terraform_config_resource"}
	case managementStatusTerraformConfigOnly:
		return []string{"terraform_state_resource", "aws_cloud_resource"}
	case managementStatusCloudOnly:
		return []string{"terraform_state_resource", "terraform_config_resource"}
	case managementStatusUnknown:
		return []string{"collector_coverage"}
	case managementStatusStaleIaCCandidate:
		return []string{"fresh_aws_cloud_resource"}
	default:
		return nil
	}
}

func recommendedActionForManagementStatus(status string) string {
	switch status {
	case managementStatusManagedByTerraform:
		return "no_action_verify_drift_only"
	case managementStatusTerraformStateOnly:
		return "restore_config_or_prepare_import_block"
	case managementStatusTerraformConfigOnly:
		return "verify_state_or_remove_stale_declaration"
	case managementStatusCloudOnly:
		return "triage_owner_and_import_or_retire"
	case managementStatusManagedByOtherIaC:
		return "review_other_iac_owner_before_terraform_adoption"
	case managementStatusAmbiguous:
		return "collect_stronger_evidence_before_import"
	case managementStatusUnknown:
		return "expand_collector_coverage_or_permissions"
	case managementStatusStaleIaCCandidate:
		return "verify_live_resource_before_cleanup_or_import"
	default:
		return "review_evidence"
	}
}

func warningFlagsForManagementFinding(
	status string,
	resourceType string,
	resourceID string,
	tags map[string]string,
) []string {
	var warnings []string
	switch status {
	case managementStatusAmbiguous:
		warnings = append(warnings, "ambiguous_ownership")
	case managementStatusUnknown:
		warnings = append(warnings, "insufficient_coverage")
	case managementStatusStaleIaCCandidate:
		warnings = append(warnings, "stale_iac_evidence")
	}
	if securitySensitiveAWSResource(resourceType, resourceID) {
		warnings = append(warnings, "security_sensitive_resource")
	}
	if len(tags) > 0 {
		warnings = append(warnings, "raw_tags_provenance_only")
	}
	return iacMergeStringSets(warnings, nil)
}

func securitySensitiveAWSResource(resourceType string, resourceID string) bool {
	resourceType = strings.ToLower(strings.TrimSpace(resourceType))
	resourceID = strings.ToLower(strings.TrimSpace(resourceID))
	switch resourceType {
	case "iam", "kms", "secretsmanager", "ssm", "rds":
		return true
	case "ec2":
		return strings.HasPrefix(resourceID, "security-group/") ||
			strings.HasPrefix(resourceID, "network-interface/") ||
			strings.HasPrefix(resourceID, "vpc/") ||
			strings.HasPrefix(resourceID, "subnet/")
	case "elasticloadbalancing", "elbv2", "cloudfront", "route53":
		return true
	default:
		return false
	}
}

type iacManagementEvidenceEnrichment struct {
	stateAddress          string
	configFile            string
	modulePath            string
	otherIaCSource        string
	serviceCandidates     []string
	environmentCandidates []string
	dependencyPaths       []string
}

func (e *iacManagementEvidenceEnrichment) recordEvidence(atom postgres.AWSCloudRuntimeDriftEvidenceRow) {
	evidenceType := strings.ToLower(strings.TrimSpace(atom.EvidenceType))
	key := strings.ToLower(strings.TrimSpace(atom.Key))
	value := strings.TrimSpace(atom.Value)
	if value == "" {
		return
	}
	switch {
	case evidenceType == "terraform_state_resource" && (key == "resource_address" || key == "address"):
		e.stateAddress = iacFirstNonEmpty(e.stateAddress, value)
	case evidenceType == "terraform_config_resource" && (key == "file_path" || key == "relative_path"):
		e.configFile = iacFirstNonEmpty(e.configFile, value)
	case evidenceType == "terraform_config_resource" && key == "module_path":
		e.modulePath = iacFirstNonEmpty(e.modulePath, value)
	case evidenceType == "service_candidate":
		e.serviceCandidates = append(e.serviceCandidates, value)
	case evidenceType == "environment_candidate":
		e.environmentCandidates = append(e.environmentCandidates, value)
	case evidenceType == "dependency_path":
		e.dependencyPaths = append(e.dependencyPaths, value)
	case strings.Contains(evidenceType, "cloudformation") ||
		strings.Contains(evidenceType, "cdk") ||
		strings.Contains(evidenceType, "pulumi") ||
		strings.Contains(evidenceType, "crossplane") ||
		strings.Contains(evidenceType, "serverless") ||
		evidenceType == "other_iac_resource":
		e.otherIaCSource = iacFirstNonEmpty(e.otherIaCSource, value)
	}
}

func iacFirstNonEmpty(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func firstNonEmptySlice(primary []string, fallback []string) []string {
	if len(primary) > 0 {
		return append([]string(nil), primary...)
	}
	if len(fallback) == 0 {
		return nil
	}
	return append([]string(nil), fallback...)
}

func iacMergeStringSets(primary []string, secondary []string) []string {
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(primary)+len(secondary))
	for _, values := range [][]string{primary, secondary} {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}
	sort.Strings(merged)
	if len(merged) == 0 {
		return nil
	}
	return merged
}
