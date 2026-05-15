package query

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	iacManagementSafetyOutcomeReadOnlyAllowed        = "read_only_allowed"
	iacManagementSafetyOutcomeSecurityReviewRequired = "security_review_required"
	iacManagementSafetyRedactionSensitiveEvidence    = "sensitive_evidence_value"
	iacManagementSafetyActionTerraformImportPlan     = "terraform_import_plan"
	iacManagementRedactedMarker                      = "[REDACTED]"
	iacManagementSafetyAuditExpectation              = "log caller, scope, route, finding id, and safety outcome without resource secrets"
)

// IaCManagementSafetyGate describes the safety decision that applies before a
// caller turns one read-only finding into a Terraform import or migration task.
type IaCManagementSafetyGate struct {
	Outcome          string   `json:"outcome"`
	ReadOnly         bool     `json:"read_only"`
	ReviewRequired   bool     `json:"review_required"`
	RefusedActions   []string `json:"refused_actions,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Redactions       []string `json:"redactions,omitempty"`
	AuditExpectation string   `json:"audit_expectation"`
}

type iacManagementSafetySummaryRow struct {
	TotalFindings         int      `json:"total_findings"`
	ReviewRequiredCount   int      `json:"review_required_count"`
	RedactedFindingsCount int      `json:"redacted_findings_count"`
	RefusedActions        []string `json:"refused_actions,omitempty"`
}

func sanitizeIaCManagementEvidence(
	atom postgres.AWSCloudRuntimeDriftEvidenceRow,
) (IaCManagementEvidenceRow, bool) {
	value := strings.TrimSpace(atom.Value)
	redacted := false
	if iacManagementSensitiveEvidenceValue(atom.EvidenceType, atom.Key) && value != "" {
		value = iacManagementRedactedMarker
		redacted = true
	}
	return IaCManagementEvidenceRow{
		ID:             atom.ID,
		SourceSystem:   atom.SourceSystem,
		EvidenceType:   atom.EvidenceType,
		ScopeID:        atom.ScopeID,
		Key:            atom.Key,
		Value:          value,
		Confidence:     atom.Confidence,
		ProvenanceOnly: strings.EqualFold(atom.EvidenceType, "aws_raw_tag"),
	}, redacted
}

func iacManagementSensitiveEvidenceValue(evidenceType string, key string) bool {
	normalizedType := strings.ToLower(strings.TrimSpace(evidenceType))
	normalizedKey := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(key, "tag:")))
	switch normalizedKey {
	case "", "arn", "resource_arn", "kms_key_id", "kms_key_arn", "key_id":
		return false
	}
	for _, token := range []string{
		"password",
		"passwd",
		"pwd",
		"api_key",
		"apikey",
		"token",
		"client_secret",
		"private_key",
		"authorization",
		"cookie",
		"session",
		"credential",
		"secret",
		"secret_access_key",
		"access_key",
		"secret_value",
	} {
		if strings.Contains(normalizedKey, token) {
			return true
		}
	}
	return strings.Contains(normalizedType, "secret_value") ||
		strings.Contains(normalizedType, "parameter_value") ||
		strings.Contains(normalizedType, "environment_value") ||
		strings.Contains(normalizedType, "credential")
}

func normalizeIaCManagementFindingsSafety(findings []IaCManagementFindingRow) []IaCManagementFindingRow {
	out := make([]IaCManagementFindingRow, len(findings))
	for i := range findings {
		out[i] = findings[i]
		normalizeIaCManagementFindingSafety(&out[i])
	}
	return out
}

func normalizeIaCManagementFindingSafety(finding *IaCManagementFindingRow) {
	if finding == nil {
		return
	}
	finding.WarningFlags = iacMergeStringSets(
		finding.WarningFlags,
		warningFlagsForManagementFinding(
			finding.ManagementStatus,
			finding.ResourceType,
			finding.ResourceID,
			finding.Tags,
		),
	)
	finding.SafetyGate = iacManagementSafetyGate(
		finding.ManagementStatus,
		finding.WarningFlags,
		finding.SafetyGate.Redactions,
	)
}

func iacManagementSafetyGate(
	status string,
	warnings []string,
	redactions []string,
) IaCManagementSafetyGate {
	warnings = iacMergeStringSets(warnings, nil)
	redactions = iacMergeStringSets(redactions, nil)
	reviewRequired := iacManagementSafetyRequiresReview(status, warnings)
	gate := IaCManagementSafetyGate{
		Outcome:          iacManagementSafetyOutcomeReadOnlyAllowed,
		ReadOnly:         true,
		ReviewRequired:   reviewRequired,
		Warnings:         warnings,
		Redactions:       redactions,
		AuditExpectation: iacManagementSafetyAuditExpectation,
	}
	if reviewRequired {
		gate.Outcome = iacManagementSafetyOutcomeSecurityReviewRequired
		gate.RefusedActions = []string{iacManagementSafetyActionTerraformImportPlan}
	}
	return gate
}

func iacManagementSafetyRequiresReview(status string, warnings []string) bool {
	switch status {
	case managementStatusAmbiguous, managementStatusUnknown, managementStatusStaleIaCCandidate:
		return true
	}
	for _, warning := range warnings {
		switch warning {
		case "security_sensitive_resource", "ambiguous_ownership", "insufficient_coverage", "stale_iac_evidence":
			return true
		}
	}
	return false
}

func iacManagementEmptySafetyGate() IaCManagementSafetyGate {
	return iacManagementSafetyGate(managementStatusUnknown, []string{"insufficient_coverage"}, nil)
}

func iacManagementSafetySummary(findings []IaCManagementFindingRow) iacManagementSafetySummaryRow {
	summary := iacManagementSafetySummaryRow{TotalFindings: len(findings)}
	refused := map[string]struct{}{}
	for _, finding := range findings {
		normalizeIaCManagementFindingSafety(&finding)
		if finding.SafetyGate.ReviewRequired {
			summary.ReviewRequiredCount++
		}
		if len(finding.SafetyGate.Redactions) > 0 {
			summary.RedactedFindingsCount++
		}
		for _, action := range finding.SafetyGate.RefusedActions {
			refused[action] = struct{}{}
		}
	}
	if len(refused) > 0 {
		summary.RefusedActions = make([]string, 0, len(refused))
		for action := range refused {
			summary.RefusedActions = append(summary.RefusedActions, action)
		}
		sort.Strings(summary.RefusedActions)
	}
	return summary
}
