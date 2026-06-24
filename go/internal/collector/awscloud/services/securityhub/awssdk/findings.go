package awssdk

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecurityhub "github.com/aws/aws-sdk-go-v2/service/securityhub"
	awssecurityhubtypes "github.com/aws/aws-sdk-go-v2/service/securityhub/types"

	securityhubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub"
)

func (c *Client) listFindingCounts(ctx context.Context) ([]securityhubservice.FindingCount, error) {
	counts := make(map[findingCountKey]int64)
	var nextToken *string
	for {
		var page *awssecurityhub.GetFindingsOutput
		err := c.recordAPICall(ctx, "GetFindings", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetFindings(callCtx, &awssecurityhub.GetFindingsInput{
				MaxResults: aws.Int32(defaultPageSize),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("get Security Hub findings for aggregate counts: %w", err)
		}
		if page == nil {
			return findingCountsFromMap(counts), nil
		}
		for _, finding := range page.Findings {
			for _, key := range aggregateKeys(finding) {
				counts[key]++
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return findingCountsFromMap(counts), nil
		}
	}
}

type findingCountKey struct {
	standardID       string
	controlID        string
	complianceStatus string
	severityLabel    string
	workflowStatus   string
}

func aggregateKeys(finding awssecurityhubtypes.AwsSecurityFinding) []findingCountKey {
	standardIDs := findingStandardIDs(finding)
	controlID := unspecifiedIfEmpty("")
	complianceStatus := unspecifiedIfEmpty("")
	if finding.Compliance != nil {
		controlID = unspecifiedIfEmpty(aws.ToString(finding.Compliance.SecurityControlId))
		complianceStatus = unspecifiedIfEmpty(string(finding.Compliance.Status))
	}
	keyTemplate := findingCountKey{
		controlID:        controlID,
		complianceStatus: complianceStatus,
		severityLabel:    severityLabel(finding),
		workflowStatus:   workflowStatus(finding),
	}
	keys := make([]findingCountKey, 0, len(standardIDs))
	for _, standardID := range standardIDs {
		key := keyTemplate
		key.standardID = standardID
		keys = append(keys, key)
	}
	return keys
}

func findingStandardIDs(finding awssecurityhubtypes.AwsSecurityFinding) []string {
	if finding.Compliance == nil || len(finding.Compliance.AssociatedStandards) == 0 {
		return []string{unspecifiedBucket}
	}
	seen := make(map[string]struct{})
	standardIDs := make([]string, 0, len(finding.Compliance.AssociatedStandards))
	for _, standard := range finding.Compliance.AssociatedStandards {
		standardID := unspecifiedIfEmpty(aws.ToString(standard.StandardsId))
		if _, ok := seen[standardID]; ok {
			continue
		}
		seen[standardID] = struct{}{}
		standardIDs = append(standardIDs, standardID)
	}
	sort.Strings(standardIDs)
	return standardIDs
}

func severityLabel(finding awssecurityhubtypes.AwsSecurityFinding) string {
	if finding.Severity == nil {
		return unspecifiedBucket
	}
	return unspecifiedIfEmpty(string(finding.Severity.Label))
}

func workflowStatus(finding awssecurityhubtypes.AwsSecurityFinding) string {
	if finding.Workflow == nil {
		return unspecifiedBucket
	}
	return unspecifiedIfEmpty(string(finding.Workflow.Status))
}

func findingCountsFromMap(input map[findingCountKey]int64) []securityhubservice.FindingCount {
	keys := make([]findingCountKey, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		return compareFindingCountKey(keys[left], keys[right]) < 0
	})
	output := make([]securityhubservice.FindingCount, 0, len(keys))
	for _, key := range keys {
		output = append(output, securityhubservice.FindingCount{
			StandardID:       key.standardID,
			ControlID:        key.controlID,
			ComplianceStatus: key.complianceStatus,
			SeverityLabel:    key.severityLabel,
			WorkflowStatus:   key.workflowStatus,
			Count:            input[key],
		})
	}
	return output
}

func compareFindingCountKey(left findingCountKey, right findingCountKey) int {
	leftValue := strings.Join([]string{
		left.standardID,
		left.controlID,
		left.complianceStatus,
		left.severityLabel,
		left.workflowStatus,
	}, "\x00")
	rightValue := strings.Join([]string{
		right.standardID,
		right.controlID,
		right.complianceStatus,
		right.severityLabel,
		right.workflowStatus,
	}, "\x00")
	return strings.Compare(leftValue, rightValue)
}

func applyComplianceCounts(
	standards []securityhubservice.Standard,
	findingCounts []securityhubservice.FindingCount,
) {
	counts := make(map[string]map[string]int64)
	for _, count := range findingCounts {
		key := standardControlKey(count.StandardID, count.ControlID)
		if _, ok := counts[key]; !ok {
			counts[key] = make(map[string]int64)
		}
		counts[key][count.ComplianceStatus] += count.Count
	}
	for standardIndex := range standards {
		standardID := standardIDFromARN(standards[standardIndex].ARN)
		if standardID == "" {
			standardID = standardIDFromARN(standards[standardIndex].SubscriptionARN)
		}
		for controlIndex := range standards[standardIndex].Controls {
			controlID := standards[standardIndex].Controls[controlIndex].ID
			standards[standardIndex].Controls[controlIndex].ComplianceCounts = cloneInt64Map(counts[standardControlKey(standardID, controlID)])
		}
	}
}

func standardControlKey(standardID string, controlID string) string {
	return strings.TrimSpace(standardID) + "\x00" + strings.TrimSpace(controlID)
}

func unspecifiedIfEmpty(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return unspecifiedBucket
}
