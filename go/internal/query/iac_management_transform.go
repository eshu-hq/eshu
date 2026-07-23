// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// awsRuntimeDriftRowToIaCManagement maps one reducer-materialized AWS runtime
// drift fact row onto the query package's stable IaC management response
// contract, deriving management status, missing evidence, warning flags, and
// the safety gate from the row's evidence atoms. Split out of
// iac_management.go (with parseAWSManagementARN and awsManagementARN) to keep
// that file under the repository's file-size cap.
func awsRuntimeDriftRowToIaCManagement(
	row postgres.AWSCloudRuntimeDriftFindingRow,
) IaCManagementFindingRow {
	parsed := parseAWSManagementARN(row.ARN)
	evidence := make([]IaCManagementEvidenceRow, 0, len(row.Evidence))
	statusInput := iacManagementStatusInput{
		FindingKind: strings.TrimSpace(row.FindingKind),
	}
	tags := map[string]string{}
	var redactions []string
	enrichment := iacManagementEvidenceEnrichment{}
	for _, atom := range row.Evidence {
		statusInput.recordEvidence(atom.EvidenceType)
		enrichment.recordEvidence(atom)
		evidenceRow, redacted := sanitizeIaCManagementEvidence(atom)
		if redacted {
			redactions = append(redactions, iacManagementSafetyRedactionSensitiveEvidence)
		}
		if evidenceRow.ProvenanceOnly && strings.HasPrefix(atom.Key, "tag:") {
			tags[strings.TrimPrefix(atom.Key, "tag:")] = evidenceRow.Value
		}
		evidence = append(evidence, evidenceRow)
	}
	if len(tags) == 0 {
		tags = nil
	}
	status := normalizeIaCManagementStatus(row.ManagementStatus, deriveIaCManagementStatus(statusInput))
	missingEvidence := firstNonEmptySlice(row.MissingEvidence, missingEvidenceForManagementStatus(status))
	warningFlags := iacMergeStringSets(
		row.WarningFlags,
		warningFlagsForManagementFinding(status, parsed.resourceType, parsed.resourceID, tags),
	)
	finding := IaCManagementFindingRow{
		ID:                           row.FactID,
		Provider:                     "aws",
		AccountID:                    parsed.accountID,
		Region:                       parsed.region,
		ResourceType:                 parsed.resourceType,
		ResourceID:                   parsed.resourceID,
		ARN:                          row.ARN,
		Tags:                         tags,
		FindingKind:                  row.FindingKind,
		ManagementStatus:             status,
		Confidence:                   row.Confidence,
		ScopeID:                      row.ScopeID,
		GenerationID:                 row.GenerationID,
		SourceSystem:                 row.SourceSystem,
		CandidateID:                  row.CandidateID,
		MatchedTerraformStateAddress: iacFirstNonEmpty(row.MatchedTerraformStateAddress, enrichment.stateAddress),
		MatchedTerraformConfigFile:   iacFirstNonEmpty(row.MatchedTerraformConfigFile, enrichment.configFile),
		MatchedTerraformModulePath:   iacFirstNonEmpty(row.MatchedTerraformModulePath, enrichment.modulePath),
		MatchedOtherIaCSource:        iacFirstNonEmpty(row.MatchedOtherIaCSource, enrichment.otherIaCSource),
		ServiceCandidates:            firstNonEmptySlice(row.ServiceCandidates, enrichment.serviceCandidates),
		EnvironmentCandidates:        firstNonEmptySlice(row.EnvironmentCandidates, enrichment.environmentCandidates),
		DependencyPaths:              firstNonEmptySlice(row.DependencyPaths, enrichment.dependencyPaths),
		RecommendedAction:            iacFirstNonEmpty(row.RecommendedAction, recommendedActionForManagementStatus(status)),
		MissingEvidence:              missingEvidence,
		WarningFlags:                 warningFlags,
		SafetyGate:                   iacManagementSafetyGate(status, warningFlags, redactions),
		Evidence:                     evidence,
		DriftedAttributes:            driftedAttributesFromAWSEvidence(row.Evidence),
	}
	normalizeIaCManagementFindingSafety(&finding)
	return finding
}

// awsManagementARN is the parsed shape of a standard AWS ARN
// (arn:partition:service:region:account-id:resource), narrowed to the four
// fields IaC management findings project onto their response contract.
type awsManagementARN struct {
	accountID    string
	region       string
	resourceType string
	resourceID   string
}

// parseAWSManagementARN splits a standard 6-part AWS ARN into its
// service/region/account/resource components. A malformed or non-ARN input
// returns the zero value rather than an error, matching this package's
// tolerant-parse convention for provenance fields that are evidence, not
// caller-validated input.
func parseAWSManagementARN(arn string) awsManagementARN {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" {
		return awsManagementARN{}
	}
	return awsManagementARN{
		accountID:    parts[4],
		region:       parts[3],
		resourceType: parts[2],
		resourceID:   parts[5],
	}
}
