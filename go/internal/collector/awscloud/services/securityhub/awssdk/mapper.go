// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecurityhub "github.com/aws/aws-sdk-go-v2/service/securityhub"
	awssecurityhubtypes "github.com/aws/aws-sdk-go-v2/service/securityhub/types"

	securityhubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/securityhub"
)

func mapHub(output *awssecurityhub.DescribeHubOutput) securityhubservice.Hub {
	return securityhubservice.Hub{
		ARN:                     aws.ToString(output.HubArn),
		AutoEnableControls:      aws.ToBool(output.AutoEnableControls),
		ControlFindingGenerator: string(output.ControlFindingGenerator),
		SubscribedAt:            parseSecurityHubTime(aws.ToString(output.SubscribedAt)),
	}
}

func mapMember(member awssecurityhubtypes.Member) securityhubservice.Member {
	return securityhubservice.Member{
		AccountID:       aws.ToString(member.AccountId),
		AdministratorID: aws.ToString(member.AdministratorId),
		Status:          aws.ToString(member.MemberStatus),
		InvitedAt:       aws.ToTime(member.InvitedAt),
		UpdatedAt:       aws.ToTime(member.UpdatedAt),
	}
}

func mapStandard(subscription awssecurityhubtypes.StandardsSubscription) securityhubservice.Standard {
	inputKeys := make([]string, 0, len(subscription.StandardsInput))
	for key := range subscription.StandardsInput {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			inputKeys = append(inputKeys, trimmed)
		}
	}
	sort.Strings(inputKeys)
	return securityhubservice.Standard{
		ARN:                aws.ToString(subscription.StandardsArn),
		SubscriptionARN:    aws.ToString(subscription.StandardsSubscriptionArn),
		Status:             string(subscription.StandardsStatus),
		ControlsUpdatable:  string(subscription.StandardsControlsUpdatable),
		StatusReasonCode:   statusReasonCode(subscription.StandardsStatusReason),
		StandardsInputKeys: inputKeys,
	}
}

func mapControl(control awssecurityhubtypes.StandardsControl) securityhubservice.Control {
	return securityhubservice.Control{
		ARN:            aws.ToString(control.StandardsControlArn),
		ID:             aws.ToString(control.ControlId),
		Title:          aws.ToString(control.Title),
		ControlStatus:  string(control.ControlStatus),
		SeverityRating: string(control.SeverityRating),
		Related:        cloneStrings(control.RelatedRequirements),
	}
}

func mapActionTarget(target awssecurityhubtypes.ActionTarget) securityhubservice.ActionTarget {
	return securityhubservice.ActionTarget{
		ARN:         aws.ToString(target.ActionTargetArn),
		Name:        aws.ToString(target.Name),
		Description: aws.ToString(target.Description),
	}
}

func mapInsight(insight awssecurityhubtypes.Insight) securityhubservice.Insight {
	return securityhubservice.Insight{
		ARN:              aws.ToString(insight.InsightArn),
		Name:             aws.ToString(insight.Name),
		GroupByAttribute: aws.ToString(insight.GroupByAttribute),
	}
}

func statusReasonCode(reason *awssecurityhubtypes.StandardsStatusReason) string {
	if reason == nil {
		return ""
	}
	return string(reason.StatusReasonCode)
}

func parseSecurityHubTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func groupsByControl(attribute string) bool {
	normalized := strings.ToLower(strings.NewReplacer(".", "", "_", "").Replace(strings.TrimSpace(attribute)))
	return normalized == "compliancesecuritycontrolid" || normalized == "securitycontrolid"
}

func standardIDFromARN(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	for _, marker := range []string{":standards/", "/standards/", ":subscription/", "/subscription/"} {
		if before, after, ok := strings.Cut(value, marker); ok && before != "" {
			return strings.TrimSpace(after)
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

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			output = append(output, trimmed)
		}
	}
	sort.Strings(output)
	return output
}

func cloneInt64Map(input map[string]int64) map[string]int64 {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]int64, len(input))
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
