// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accessanalyzer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits IAM Access Analyzer metadata facts for one claimed account and
// region. It never persists finding bodies, archive-rule filters, policy
// generation results, or mutation-derived state.
type Scanner struct {
	Client Client
}

// Scan observes Access Analyzer analyzers, safe archive-rule bindings,
// aggregate finding counts, and unused-access summaries through the configured
// client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("accessanalyzer scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAccessAnalyzer:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAccessAnalyzer
	default:
		return nil, fmt.Errorf("accessanalyzer scanner received service_kind %q", boundary.ServiceKind)
	}

	analyzers, err := s.Client.ListAnalyzers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Access Analyzer analyzers: %w", err)
	}
	var envelopes []facts.Envelope
	for _, analyzer := range analyzers {
		if !isSupportedAnalyzerType(analyzer.Type) {
			continue
		}
		resource, err := awscloud.NewResourceEnvelope(analyzerObservation(boundary, analyzer))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)

		if relationship, ok := analyzerOrganizationAccountRelationship(boundary, analyzer); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}

		for _, warning := range analyzer.Warnings {
			warning.Boundary = boundary
			envelope, err := awscloud.NewWarningEnvelope(warning)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}

		if strings.TrimSpace(analyzer.ARN) == "" {
			continue
		}

		for _, rule := range analyzer.ArchiveRules {
			if archiveRuleID(analyzer, rule) == "" {
				continue
			}
			ruleResource, err := awscloud.NewResourceEnvelope(archiveRuleObservation(boundary, analyzer, rule))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, ruleResource)
			relationship, ok := analyzerArchiveRuleRelationship(boundary, analyzer, rule)
			if !ok {
				continue
			}
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}

		for _, count := range analyzer.FindingCounts {
			if findingCountID(analyzer, count) == "" {
				continue
			}
			countResource, err := awscloud.NewResourceEnvelope(findingCountObservation(boundary, analyzer, count))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, countResource)
		}

		for _, summary := range analyzer.UnusedAccessSummaries {
			if unusedAccessSummaryID(analyzer, summary) == "" {
				continue
			}
			summaryResource, err := awscloud.NewResourceEnvelope(unusedAccessSummaryObservation(boundary, analyzer, summary))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, summaryResource)
		}
	}
	return envelopes, nil
}

func analyzerObservation(boundary awscloud.Boundary, analyzer Analyzer) awscloud.ResourceObservation {
	analyzerARN := strings.TrimSpace(analyzer.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          analyzerARN,
		ResourceID:   firstNonEmpty(analyzerARN, analyzer.Name),
		ResourceType: awscloud.ResourceTypeAccessAnalyzerAnalyzer,
		Name:         strings.TrimSpace(analyzer.Name),
		State:        strings.TrimSpace(analyzer.Status),
		Tags:         cloneStringMap(analyzer.Tags),
		Attributes: map[string]any{
			"analysis_type":             analysisType(analyzer.Type),
			"analyzer_type":             strings.TrimSpace(analyzer.Type),
			"created_at":                timeOrNil(analyzer.CreatedAt),
			"last_resource_analyzed":    strings.TrimSpace(analyzer.LastResourceAnalyzed),
			"last_resource_analyzed_at": timeOrNil(analyzer.LastResourceAnalyzedAt),
			"scope":                     analyzerScope(analyzer.Type),
		},
		CorrelationAnchors: []string{analyzerARN, analyzer.Name},
		SourceRecordID:     firstNonEmpty(analyzerARN, analyzer.Name),
	}
}

func archiveRuleObservation(
	boundary awscloud.Boundary,
	analyzer Analyzer,
	rule ArchiveRule,
) awscloud.ResourceObservation {
	resourceID := archiveRuleID(analyzer, rule)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAccessAnalyzerArchiveRule,
		Name:         strings.TrimSpace(rule.Name),
		Attributes: map[string]any{
			"analyzer_arn": strings.TrimSpace(firstNonEmpty(rule.AnalyzerARN, analyzer.ARN)),
			"created_at":   timeOrNil(rule.CreatedAt),
			"updated_at":   timeOrNil(rule.UpdatedAt),
		},
		CorrelationAnchors: []string{resourceID, rule.Name, analyzer.ARN},
		SourceRecordID:     resourceID,
	}
}

func findingCountObservation(
	boundary awscloud.Boundary,
	analyzer Analyzer,
	count FindingCount,
) awscloud.ResourceObservation {
	resourceID := findingCountID(analyzer, count)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAccessAnalyzerFindingCount,
		Name:         strings.TrimSpace(count.Status + " " + count.ResourceType),
		State:        strings.TrimSpace(count.Status),
		Attributes: map[string]any{
			"analysis_type":         analysisType(analyzer.Type),
			"analyzer_arn":          strings.TrimSpace(analyzer.ARN),
			"count":                 count.Count,
			"finding_resource_type": strings.TrimSpace(count.ResourceType),
			"finding_status":        strings.TrimSpace(count.Status),
			"scope":                 analyzerScope(analyzer.Type),
		},
		CorrelationAnchors: []string{resourceID, analyzer.ARN, count.ResourceType},
		SourceRecordID:     resourceID,
	}
}

func unusedAccessSummaryObservation(
	boundary awscloud.Boundary,
	analyzer Analyzer,
	summary UnusedAccessSummary,
) awscloud.ResourceObservation {
	resourceID := unusedAccessSummaryID(analyzer, summary)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAccessAnalyzerUnusedAccessSummary,
		Name:         strings.TrimSpace(summary.ResourceID),
		State:        strings.TrimSpace(summary.Status),
		Attributes: map[string]any{
			"analyzer_arn":            strings.TrimSpace(analyzer.ARN),
			"analyzed_at":             timeOrNil(summary.AnalyzedAt),
			"finding_resource_type":   strings.TrimSpace(summary.ResourceType),
			"finding_status":          strings.TrimSpace(summary.Status),
			"finding_type":            strings.TrimSpace(summary.FindingType),
			"last_accessed_at":        timeOrNil(summary.LastAccessedAt),
			"resource_owner_account":  strings.TrimSpace(summary.ResourceOwnerAccount),
			"target_resource_id":      strings.TrimSpace(summary.ResourceID),
			"unused_access_aggregate": true,
			"updated_at":              timeOrNil(summary.UpdatedAt),
		},
		CorrelationAnchors: []string{resourceID, analyzer.ARN, summary.ResourceID},
		SourceRecordID:     resourceID,
	}
}

func analyzerOrganizationAccountRelationship(
	boundary awscloud.Boundary,
	analyzer Analyzer,
) (awscloud.RelationshipObservation, bool) {
	if analyzerScope(analyzer.Type) != "ORGANIZATION" {
		return awscloud.RelationshipObservation{}, false
	}
	partition, accountID, ok := accountFromAnalyzerARN(analyzer.ARN)
	if !ok {
		return awscloud.RelationshipObservation{}, false
	}
	accountARN := "arn:" + partition + ":iam::" + accountID + ":root"
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount,
		SourceResourceID: strings.TrimSpace(analyzer.ARN),
		SourceARN:        strings.TrimSpace(analyzer.ARN),
		TargetResourceID: accountARN,
		TargetARN:        accountARN,
		TargetType:       awscloud.ResourceTypeAWSAccount,
		Attributes: map[string]any{
			"account_id": accountID,
			"scope":      "ORGANIZATION",
		},
		SourceRecordID: strings.TrimSpace(analyzer.ARN) + "#organization-account#" + accountID,
	}, true
}

func analyzerArchiveRuleRelationship(
	boundary awscloud.Boundary,
	analyzer Analyzer,
	rule ArchiveRule,
) (awscloud.RelationshipObservation, bool) {
	analyzerARN := strings.TrimSpace(firstNonEmpty(rule.AnalyzerARN, analyzer.ARN))
	ruleID := archiveRuleID(analyzer, rule)
	if analyzerARN == "" || ruleID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAccessAnalyzerAnalyzerHasArchiveRule,
		SourceResourceID: analyzerARN,
		SourceARN:        analyzerARN,
		TargetResourceID: ruleID,
		TargetType:       awscloud.ResourceTypeAccessAnalyzerArchiveRule,
		Attributes: map[string]any{
			"rule_name": strings.TrimSpace(rule.Name),
		},
		SourceRecordID: analyzerARN + "#archive-rule#" + strings.TrimSpace(rule.Name),
	}, true
}

func archiveRuleID(analyzer Analyzer, rule ArchiveRule) string {
	base := strings.TrimRight(strings.TrimSpace(firstNonEmpty(rule.AnalyzerARN, analyzer.ARN)), "/")
	name := strings.TrimSpace(rule.Name)
	if base == "" || name == "" {
		return ""
	}
	return base + "/archive-rule/" + name
}

func findingCountID(analyzer Analyzer, count FindingCount) string {
	analyzerARN := strings.TrimRight(strings.TrimSpace(analyzer.ARN), "/")
	status := strings.TrimSpace(count.Status)
	resourceType := strings.TrimSpace(count.ResourceType)
	if analyzerARN == "" || status == "" || resourceType == "" {
		return ""
	}
	return analyzerARN + "/finding-count/" + status + "/" + resourceType
}

func unusedAccessSummaryID(analyzer Analyzer, summary UnusedAccessSummary) string {
	analyzerARN := strings.TrimRight(strings.TrimSpace(analyzer.ARN), "/")
	suffix := strings.TrimSpace(summary.FindingID)
	if suffix == "" {
		findingType := strings.TrimSpace(summary.FindingType)
		resourceID := strings.TrimSpace(summary.ResourceID)
		if findingType == "" || resourceID == "" {
			return ""
		}
		suffix = findingType + "/" + resourceID
	}
	if analyzerARN == "" || suffix == "" {
		return ""
	}
	return analyzerARN + "/unused-access/" + suffix
}

func isSupportedAnalyzerType(value string) bool {
	switch strings.TrimSpace(value) {
	case "ACCOUNT", "ORGANIZATION", "ACCOUNT_UNUSED_ACCESS", "ORGANIZATION_UNUSED_ACCESS":
		return true
	default:
		return false
	}
}

func analyzerScope(value string) string {
	if strings.HasPrefix(strings.TrimSpace(value), "ORGANIZATION") {
		return "ORGANIZATION"
	}
	return "ACCOUNT"
}

func analysisType(value string) string {
	if strings.Contains(strings.TrimSpace(value), "UNUSED_ACCESS") {
		return "unused_access"
	}
	return "external_access"
}

func accountFromAnalyzerARN(value string) (partition string, accountID string, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 6 || parts[0] != "arn" || parts[4] == "" {
		return "", "", false
	}
	return parts[1], parts[4], true
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
