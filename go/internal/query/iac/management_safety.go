// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	managementSafetyOutcomeReadOnlyAllowed        = "read_only_allowed"
	managementSafetyOutcomeSecurityReviewRequired = "security_review_required"
	managementSafetyRedactionSensitiveEvidence    = "sensitive_evidence_value"
	managementSafetyActionTerraformImportPlan     = "terraform_import_plan"
	managementRedactedMarker                      = "[REDACTED]"
	managementSafetyAuditExpectation              = "log caller, scope, route, finding id, and safety outcome without resource secrets"
)

// ManagementSafetyGate describes the safety decision that applies before a
// caller turns one read-only finding into a Terraform import or migration task.
type ManagementSafetyGate struct {
	Outcome          string   `json:"outcome"`
	ReadOnly         bool     `json:"read_only"`
	ReviewRequired   bool     `json:"review_required"`
	RefusedActions   []string `json:"refused_actions,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Redactions       []string `json:"redactions,omitempty"`
	AuditExpectation string   `json:"audit_expectation"`
}

type managementSafetySummaryRow struct {
	TotalFindings         int      `json:"total_findings"`
	ReviewRequiredCount   int      `json:"review_required_count"`
	RedactedFindingsCount int      `json:"redacted_findings_count"`
	RefusedActions        []string `json:"refused_actions,omitempty"`
}

func sanitizeIaCManagementEvidence(
	atom postgres.AWSCloudRuntimeDriftEvidenceRow,
) (ManagementEvidenceRow, bool) {
	value := strings.TrimSpace(atom.Value)
	redacted := false
	if managementSensitiveEvidenceValue(atom.EvidenceType, atom.Key) && value != "" {
		value = managementRedactedMarker
		redacted = true
	}
	return ManagementEvidenceRow{
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

func managementSensitiveEvidenceValue(evidenceType string, key string) bool {
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

func normalizeIaCManagementFindingsSafety(findings []ManagementFindingRow) []ManagementFindingRow {
	out := make([]ManagementFindingRow, len(findings))
	for i := range findings {
		out[i] = findings[i]
		NormalizeManagementFindingSafety(&out[i])
	}
	return out
}

func NormalizeManagementFindingSafety(finding *ManagementFindingRow) {
	if finding == nil {
		return
	}
	finding.WarningFlags = mergeStringSets(
		finding.WarningFlags,
		warningFlagsForManagementFinding(
			finding.ManagementStatus,
			finding.ResourceType,
			finding.ResourceID,
			len(finding.Tags) > 0,
		),
	)
	finding.SafetyGate = NewManagementSafetyGate(
		finding.ManagementStatus,
		finding.WarningFlags,
		finding.SafetyGate.Redactions,
	)
}

func NewManagementSafetyGate(
	status string,
	warnings []string,
	redactions []string,
) ManagementSafetyGate {
	warnings = mergeStringSets(warnings, nil)
	redactions = mergeStringSets(redactions, nil)
	reviewRequired := managementSafetyRequiresReview(status, warnings)
	gate := ManagementSafetyGate{
		Outcome:          managementSafetyOutcomeReadOnlyAllowed,
		ReadOnly:         true,
		ReviewRequired:   reviewRequired,
		Warnings:         warnings,
		Redactions:       redactions,
		AuditExpectation: managementSafetyAuditExpectation,
	}
	if reviewRequired {
		gate.Outcome = managementSafetyOutcomeSecurityReviewRequired
		gate.RefusedActions = []string{managementSafetyActionTerraformImportPlan}
	}
	return gate
}

func managementSafetyRequiresReview(status string, warnings []string) bool {
	switch status {
	case ManagementStatusAmbiguous, ManagementStatusUnknown, ManagementStatusStaleIaCCandidate:
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

func managementEmptySafetyGate() ManagementSafetyGate {
	return NewManagementSafetyGate(ManagementStatusUnknown, []string{"insufficient_coverage"}, nil)
}

func managementSafetySummary(findings []ManagementFindingRow) managementSafetySummaryRow {
	summary := managementSafetySummaryRow{TotalFindings: len(findings)}
	refused := map[string]struct{}{}
	for _, finding := range findings {
		NormalizeManagementFindingSafety(&finding)
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
