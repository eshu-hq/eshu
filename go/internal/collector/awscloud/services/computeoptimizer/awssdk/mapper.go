// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscotypes "github.com/aws/aws-sdk-go-v2/service/computeoptimizer/types"

	coservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/computeoptimizer"
)

// mapSummary maps an SDK recommendation summary into the scanner-owned summary.
// Only finding-class counts and the aggregated savings-opportunity percentage
// are kept; the SDK's cost data points are intentionally dropped.
func mapSummary(summary awscotypes.RecommendationSummary) coservice.RecommendationSummary {
	mapped := coservice.RecommendationSummary{
		ResourceType: strings.TrimSpace(string(summary.RecommendationResourceType)),
		AccountID:    strings.TrimSpace(aws.ToString(summary.AccountId)),
	}
	if len(summary.Summaries) > 0 {
		counts := make(map[string]float64, len(summary.Summaries))
		for _, entry := range summary.Summaries {
			name := strings.TrimSpace(string(entry.Name))
			if name == "" {
				continue
			}
			counts[name] = entry.Value
		}
		if len(counts) > 0 {
			mapped.FindingCounts = counts
		}
	}
	if summary.SavingsOpportunity != nil {
		mapped.SavingsOpportunityPercentage = summary.SavingsOpportunity.SavingsOpportunityPercentage
	}
	return mapped
}

// mapInstanceRecommendation maps an SDK EC2 instance recommendation into the
// scanner-owned model, keeping the identity, finding, current type, and the
// top-ranked recommended type only.
func mapInstanceRecommendation(rec awscotypes.InstanceRecommendation) coservice.InstanceRecommendation {
	mapped := coservice.InstanceRecommendation{
		InstanceARN:          strings.TrimSpace(aws.ToString(rec.InstanceArn)),
		InstanceName:         strings.TrimSpace(aws.ToString(rec.InstanceName)),
		AccountID:            strings.TrimSpace(aws.ToString(rec.AccountId)),
		CurrentInstanceType:  strings.TrimSpace(aws.ToString(rec.CurrentInstanceType)),
		Finding:              strings.TrimSpace(string(rec.Finding)),
		LookBackPeriodInDays: rec.LookBackPeriodInDays,
		LastRefreshTimestamp: aws.ToTime(rec.LastRefreshTimestamp),
		Tags:                 mapTags(rec.Tags),
	}
	if option := topInstanceOption(rec.RecommendationOptions); option != nil {
		mapped.RecommendedInstanceType = strings.TrimSpace(aws.ToString(option.InstanceType))
		mapped.SavingsOpportunityPercentage = savingsPercentage(option.SavingsOpportunity)
	}
	return mapped
}

// mapAutoScalingGroupRecommendation maps an SDK Auto Scaling group
// recommendation into the scanner-owned model.
func mapAutoScalingGroupRecommendation(rec awscotypes.AutoScalingGroupRecommendation) coservice.AutoScalingGroupRecommendation {
	mapped := coservice.AutoScalingGroupRecommendation{
		AutoScalingGroupARN:  strings.TrimSpace(aws.ToString(rec.AutoScalingGroupArn)),
		AutoScalingGroupName: strings.TrimSpace(aws.ToString(rec.AutoScalingGroupName)),
		AccountID:            strings.TrimSpace(aws.ToString(rec.AccountId)),
		Finding:              strings.TrimSpace(string(rec.Finding)),
		LookBackPeriodInDays: rec.LookBackPeriodInDays,
		LastRefreshTimestamp: aws.ToTime(rec.LastRefreshTimestamp),
	}
	if rec.CurrentConfiguration != nil {
		mapped.CurrentInstanceType = strings.TrimSpace(aws.ToString(rec.CurrentConfiguration.InstanceType))
	}
	if option := topAutoScalingGroupOption(rec.RecommendationOptions); option != nil {
		if option.Configuration != nil {
			mapped.RecommendedInstanceType = strings.TrimSpace(aws.ToString(option.Configuration.InstanceType))
		}
		mapped.SavingsOpportunityPercentage = savingsPercentage(option.SavingsOpportunity)
	}
	return mapped
}

// mapVolumeRecommendation maps an SDK EBS volume recommendation into the
// scanner-owned model.
func mapVolumeRecommendation(rec awscotypes.VolumeRecommendation) coservice.VolumeRecommendation {
	mapped := coservice.VolumeRecommendation{
		VolumeARN:            strings.TrimSpace(aws.ToString(rec.VolumeArn)),
		AccountID:            strings.TrimSpace(aws.ToString(rec.AccountId)),
		Finding:              strings.TrimSpace(string(rec.Finding)),
		LookBackPeriodInDays: rec.LookBackPeriodInDays,
		LastRefreshTimestamp: aws.ToTime(rec.LastRefreshTimestamp),
		Tags:                 mapTags(rec.Tags),
	}
	if rec.CurrentConfiguration != nil {
		mapped.CurrentVolumeType = strings.TrimSpace(aws.ToString(rec.CurrentConfiguration.VolumeType))
	}
	if option := topVolumeOption(rec.VolumeRecommendationOptions); option != nil {
		if option.Configuration != nil {
			mapped.RecommendedVolumeType = strings.TrimSpace(aws.ToString(option.Configuration.VolumeType))
		}
		mapped.SavingsOpportunityPercentage = savingsPercentage(option.SavingsOpportunity)
	}
	return mapped
}

// mapLambdaFunctionRecommendation maps an SDK Lambda function recommendation into
// the scanner-owned model.
func mapLambdaFunctionRecommendation(rec awscotypes.LambdaFunctionRecommendation) coservice.LambdaFunctionRecommendation {
	mapped := coservice.LambdaFunctionRecommendation{
		FunctionARN:          strings.TrimSpace(aws.ToString(rec.FunctionArn)),
		FunctionVersion:      strings.TrimSpace(aws.ToString(rec.FunctionVersion)),
		AccountID:            strings.TrimSpace(aws.ToString(rec.AccountId)),
		CurrentMemorySize:    rec.CurrentMemorySize,
		Finding:              strings.TrimSpace(string(rec.Finding)),
		LookBackPeriodInDays: rec.LookbackPeriodInDays,
		LastRefreshTimestamp: aws.ToTime(rec.LastRefreshTimestamp),
		Tags:                 mapTags(rec.Tags),
	}
	if option := topLambdaMemoryOption(rec.MemorySizeRecommendationOptions); option != nil {
		mapped.RecommendedMemorySize = option.MemorySize
		mapped.SavingsOpportunityPercentage = savingsPercentage(option.SavingsOpportunity)
	}
	return mapped
}

// topInstanceOption returns the rank-1 instance recommendation option, or the
// first option when no rank-1 entry is present.
func topInstanceOption(options []awscotypes.InstanceRecommendationOption) *awscotypes.InstanceRecommendationOption {
	for i := range options {
		if options[i].Rank == 1 {
			return &options[i]
		}
	}
	if len(options) > 0 {
		return &options[0]
	}
	return nil
}

func topAutoScalingGroupOption(options []awscotypes.AutoScalingGroupRecommendationOption) *awscotypes.AutoScalingGroupRecommendationOption {
	for i := range options {
		if options[i].Rank == 1 {
			return &options[i]
		}
	}
	if len(options) > 0 {
		return &options[0]
	}
	return nil
}

func topVolumeOption(options []awscotypes.VolumeRecommendationOption) *awscotypes.VolumeRecommendationOption {
	for i := range options {
		if options[i].Rank == 1 {
			return &options[i]
		}
	}
	if len(options) > 0 {
		return &options[0]
	}
	return nil
}

func topLambdaMemoryOption(options []awscotypes.LambdaFunctionMemoryRecommendationOption) *awscotypes.LambdaFunctionMemoryRecommendationOption {
	for i := range options {
		if options[i].Rank == 1 {
			return &options[i]
		}
	}
	if len(options) > 0 {
		return &options[0]
	}
	return nil
}

// savingsPercentage returns the savings-opportunity percentage of an option,
// dropping the underlying customer cost data point.
func savingsPercentage(opportunity *awscotypes.SavingsOpportunity) float64 {
	if opportunity == nil {
		return 0
	}
	return opportunity.SavingsOpportunityPercentage
}

// mapTags converts SDK tags into a trimmed-key string map, dropping empty keys,
// or nil when nothing survives.
func mapTags(tags []awscotypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
