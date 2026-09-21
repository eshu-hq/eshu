// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package computeoptimizer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// summaryObservation maps a Compute Optimizer recommendation summary into a
// resource observation. The resource_id encodes the account and resource type so
// each per-type summary has a stable identity within a generation. Only
// finding-class counts and the aggregated savings-opportunity percentage are
// recorded; no customer cost data point is persisted.
func summaryObservation(boundary aws.Boundary, summary RecommendationSummary) aws.ResourceObservation {
	resourceType := strings.TrimSpace(summary.ResourceType)
	accountID := firstNonEmpty(summary.AccountID, boundary.AccountID)
	resourceID := summaryResourceID(boundary, accountID, resourceType)
	name := resourceType + " recommendation summary"
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeComputeOptimizerRecommendationSummary,
		Name:         name,
		Attributes: map[string]any{
			"account_id":                     accountID,
			"recommendation_resource_type":   resourceType,
			"finding_counts":                 cloneFloatMap(summary.FindingCounts),
			"savings_opportunity_percentage": summary.SavingsOpportunityPercentage,
		},
		CorrelationAnchors: []string{resourceID},
		SourceRecordID:     resourceID,
	}
}

// instanceObservation maps an EC2 instance recommendation into a resource
// observation keyed by the analyzed instance ARN.
func instanceObservation(boundary aws.Boundary, rec InstanceRecommendation) aws.ResourceObservation {
	resourceID := instanceRecommendationID(rec)
	instanceARN := strings.TrimSpace(rec.InstanceARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          instanceARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeComputeOptimizerRecommendation,
		Name:         firstNonEmpty(rec.InstanceName, instanceIDFromARN(instanceARN), resourceID),
		Tags:         cloneStringMap(rec.Tags),
		Attributes: map[string]any{
			"recommendation_resource_type":   "Ec2Instance",
			"account_id":                     firstNonEmpty(rec.AccountID, boundary.AccountID),
			"instance_arn":                   instanceARN,
			"instance_id":                    instanceIDFromARN(instanceARN),
			"instance_name":                  strings.TrimSpace(rec.InstanceName),
			"current_instance_type":          strings.TrimSpace(rec.CurrentInstanceType),
			"recommended_instance_type":      strings.TrimSpace(rec.RecommendedInstanceType),
			"finding":                        strings.TrimSpace(rec.Finding),
			"look_back_period_in_days":       rec.LookBackPeriodInDays,
			"savings_opportunity_percentage": rec.SavingsOpportunityPercentage,
			"last_refresh_timestamp":         timeOrNil(rec.LastRefreshTimestamp),
		},
		CorrelationAnchors: []string{instanceARN, resourceID},
		SourceRecordID:     resourceID,
	}
}

// autoScalingGroupObservation maps an Auto Scaling group recommendation into a
// resource observation keyed by the analyzed group ARN (falling back to name).
func autoScalingGroupObservation(boundary aws.Boundary, rec AutoScalingGroupRecommendation) aws.ResourceObservation {
	resourceID := autoScalingGroupRecommendationID(rec)
	groupARN := strings.TrimSpace(rec.AutoScalingGroupARN)
	groupName := strings.TrimSpace(rec.AutoScalingGroupName)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeComputeOptimizerRecommendation,
		Name:         firstNonEmpty(groupName, resourceID),
		Attributes: map[string]any{
			"recommendation_resource_type":   "AutoScalingGroup",
			"account_id":                     firstNonEmpty(rec.AccountID, boundary.AccountID),
			"auto_scaling_group_arn":         groupARN,
			"auto_scaling_group_name":        groupName,
			"current_instance_type":          strings.TrimSpace(rec.CurrentInstanceType),
			"recommended_instance_type":      strings.TrimSpace(rec.RecommendedInstanceType),
			"finding":                        strings.TrimSpace(rec.Finding),
			"look_back_period_in_days":       rec.LookBackPeriodInDays,
			"savings_opportunity_percentage": rec.SavingsOpportunityPercentage,
			"last_refresh_timestamp":         timeOrNil(rec.LastRefreshTimestamp),
		},
		CorrelationAnchors: []string{groupARN, groupName, resourceID},
		SourceRecordID:     resourceID,
	}
}

// volumeObservation maps an EBS volume recommendation into a resource
// observation keyed by the analyzed volume ARN. The bare volume id is recorded
// as metadata; recommendation-to-volume relationship projection is a separate
// follow-up.
func volumeObservation(boundary aws.Boundary, rec VolumeRecommendation) aws.ResourceObservation {
	resourceID := volumeRecommendationID(rec)
	volumeARN := strings.TrimSpace(rec.VolumeARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          volumeARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeComputeOptimizerRecommendation,
		Name:         firstNonEmpty(volumeIDFromARN(volumeARN), resourceID),
		Tags:         cloneStringMap(rec.Tags),
		Attributes: map[string]any{
			"recommendation_resource_type":   "EbsVolume",
			"account_id":                     firstNonEmpty(rec.AccountID, boundary.AccountID),
			"volume_arn":                     volumeARN,
			"volume_id":                      volumeIDFromARN(volumeARN),
			"current_volume_type":            strings.TrimSpace(rec.CurrentVolumeType),
			"recommended_volume_type":        strings.TrimSpace(rec.RecommendedVolumeType),
			"finding":                        strings.TrimSpace(rec.Finding),
			"look_back_period_in_days":       rec.LookBackPeriodInDays,
			"savings_opportunity_percentage": rec.SavingsOpportunityPercentage,
			"last_refresh_timestamp":         timeOrNil(rec.LastRefreshTimestamp),
		},
		CorrelationAnchors: []string{volumeARN, resourceID},
		SourceRecordID:     resourceID,
	}
}

// lambdaFunctionObservation maps a Lambda function recommendation into a
// resource observation keyed by the analyzed function ARN.
func lambdaFunctionObservation(boundary aws.Boundary, rec LambdaFunctionRecommendation) aws.ResourceObservation {
	resourceID := lambdaFunctionRecommendationID(rec)
	functionARN := strings.TrimSpace(rec.FunctionARN)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          functionARN,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeComputeOptimizerRecommendation,
		Name:         firstNonEmpty(functionNameFromARN(functionARN), resourceID),
		Tags:         cloneStringMap(rec.Tags),
		Attributes: map[string]any{
			"recommendation_resource_type":   "LambdaFunction",
			"account_id":                     firstNonEmpty(rec.AccountID, boundary.AccountID),
			"function_arn":                   functionARN,
			"function_version":               strings.TrimSpace(rec.FunctionVersion),
			"current_memory_size":            rec.CurrentMemorySize,
			"recommended_memory_size":        rec.RecommendedMemorySize,
			"finding":                        strings.TrimSpace(rec.Finding),
			"look_back_period_in_days":       rec.LookBackPeriodInDays,
			"savings_opportunity_percentage": rec.SavingsOpportunityPercentage,
			"last_refresh_timestamp":         timeOrNil(rec.LastRefreshTimestamp),
		},
		CorrelationAnchors: []string{functionARN, resourceID},
		SourceRecordID:     resourceID,
	}
}

// summaryResourceID builds the stable resource_id for a per-type recommendation
// summary. It is a synthetic identity (Compute Optimizer summaries have no ARN),
// scoped to the account and region the boundary already carries.
func summaryResourceID(boundary aws.Boundary, accountID, resourceType string) string {
	region := strings.TrimSpace(boundary.Region)
	parts := []string{"compute-optimizer-summary"}
	if accountID = strings.TrimSpace(accountID); accountID != "" {
		parts = append(parts, accountID)
	}
	if region != "" {
		parts = append(parts, region)
	}
	if resourceType = strings.TrimSpace(resourceType); resourceType != "" {
		parts = append(parts, resourceType)
	}
	return strings.Join(parts, ":")
}

// functionNameFromARN extracts the Lambda function name from a function ARN of
// the form arn:<partition>:lambda:<region>:<account>:function:<name>[:<qualifier>].
// It returns "" when the value is not a Lambda function ARN.
func functionNameFromARN(arn string) string {
	arn = strings.TrimSpace(arn)
	marker := ":function:"
	idx := strings.LastIndex(arn, marker)
	if idx < 0 {
		return ""
	}
	rest := arn[idx+len(marker):]
	if colon := strings.IndexByte(rest, ':'); colon >= 0 {
		rest = rest[:colon]
	}
	return strings.TrimSpace(rest)
}
