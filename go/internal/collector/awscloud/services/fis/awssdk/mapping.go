// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfistypes "github.com/aws/aws-sdk-go-v2/service/fis/types"

	fisservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/fis"
)

// mapActions flattens the FIS action map into scanner-owned actions, keeping
// only the action key, FIS action id, and per-template description. Action
// parameter values are intentionally excluded so no secret or payload-shaped
// value is persisted. The result is sorted by key for deterministic facts.
func mapActions(actions map[string]awsfistypes.ExperimentTemplateAction) []fisservice.Action {
	if len(actions) == 0 {
		return nil
	}
	mapped := make([]fisservice.Action, 0, len(actions))
	for key, action := range actions {
		mapped = append(mapped, fisservice.Action{
			Key:         strings.TrimSpace(key),
			ActionID:    strings.TrimSpace(aws.ToString(action.ActionId)),
			Description: strings.TrimSpace(aws.ToString(action.Description)),
		})
	}
	sort.Slice(mapped, func(i, j int) bool { return mapped[i].Key < mapped[j].Key })
	return mapped
}

// mapTargets flattens the FIS target map into scanner-owned targets, keeping the
// target key, resource type selector, selection mode, and any explicitly listed
// resource ARNs. Target filter values and resource-tag selectors are
// intentionally excluded. The result is sorted by key for deterministic facts.
func mapTargets(targets map[string]awsfistypes.ExperimentTemplateTarget) []fisservice.Target {
	if len(targets) == 0 {
		return nil
	}
	mapped := make([]fisservice.Target, 0, len(targets))
	for key, target := range targets {
		mapped = append(mapped, fisservice.Target{
			Key:           strings.TrimSpace(key),
			ResourceType:  strings.TrimSpace(aws.ToString(target.ResourceType)),
			SelectionMode: strings.TrimSpace(aws.ToString(target.SelectionMode)),
			ResourceARNs:  cleanStrings(target.ResourceArns),
		})
	}
	sort.Slice(mapped, func(i, j int) bool { return mapped[i].Key < mapped[j].Key })
	return mapped
}

// logDestinations extracts the CloudWatch Logs log group ARN and S3 bucket/
// prefix from an FIS log configuration. The S3 log object contents are never
// read; only the destination location configuration is metadata.
func logDestinations(config *awsfistypes.ExperimentTemplateLogConfiguration) (logGroupARN, s3Bucket, s3Prefix string) {
	if config == nil {
		return "", "", ""
	}
	if cw := config.CloudWatchLogsConfiguration; cw != nil {
		logGroupARN = strings.TrimSpace(aws.ToString(cw.LogGroupArn))
	}
	if s3 := config.S3Configuration; s3 != nil {
		s3Bucket = strings.TrimSpace(aws.ToString(s3.BucketName))
		s3Prefix = strings.TrimSpace(aws.ToString(s3.Prefix))
	}
	return logGroupARN, s3Bucket, s3Prefix
}

// stopConditionAlarmARNs returns the CloudWatch alarm ARNs from the template's
// stop conditions. FIS reports an alarm ARN in the Value field only for the
// aws:cloudwatch:alarm source; the implicit "none" stop condition carries no
// value and is dropped.
func stopConditionAlarmARNs(conditions []awsfistypes.ExperimentTemplateStopCondition) []string {
	var arns []string
	for _, condition := range conditions {
		source := strings.ToLower(strings.TrimSpace(aws.ToString(condition.Source)))
		if source == "" || source == "none" {
			continue
		}
		value := strings.TrimSpace(aws.ToString(condition.Value))
		if value == "" {
			continue
		}
		arns = append(arns, value)
	}
	return arns
}

// cloneTags returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty.
func cloneTags(input map[string]string) map[string]string {
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

// cleanStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func cleanStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
