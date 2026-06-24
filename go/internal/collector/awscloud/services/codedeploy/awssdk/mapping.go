// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	cdtypes "github.com/aws/aws-sdk-go-v2/service/codedeploy/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cdservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codedeploy"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// onPremisesTagValueReason labels redacted on-premises instance tag values so
// downstream audit can trace why the value was replaced.
const onPremisesTagValueReason = "codedeploy_on_premises_tag_value"

func mapApplication(info cdtypes.ApplicationInfo) cdservice.Application {
	application := cdservice.Application{
		Name:            aws.ToString(info.ApplicationName),
		ID:              aws.ToString(info.ApplicationId),
		ComputePlatform: string(info.ComputePlatform),
		GitHubAccount:   aws.ToString(info.GitHubAccountName),
		LinkedToGitHub:  info.LinkedToGitHub,
	}
	if info.CreateTime != nil {
		application.CreateTime = info.CreateTime.UTC()
	}
	return application
}

func mapDeploymentGroup(info cdtypes.DeploymentGroupInfo, key redact.Key) cdservice.DeploymentGroup {
	group := cdservice.DeploymentGroup{
		Name:                      aws.ToString(info.DeploymentGroupName),
		ID:                        aws.ToString(info.DeploymentGroupId),
		ApplicationName:           aws.ToString(info.ApplicationName),
		ComputePlatform:           string(info.ComputePlatform),
		DeploymentConfigName:      aws.ToString(info.DeploymentConfigName),
		ServiceRoleARN:            aws.ToString(info.ServiceRoleArn),
		OutdatedInstancesStrategy: string(info.OutdatedInstancesStrategy),
		TerminationHookEnabled:    info.TerminationHookEnabled,
		DeploymentStyle:           mapDeploymentStyle(info.DeploymentStyle),
		AutoRollback:              mapAutoRollback(info.AutoRollbackConfiguration),
		AutoScalingGroups:         mapAutoScalingGroups(info.AutoScalingGroups),
		ECSServices:               mapECSServices(info.EcsServices),
		SNSTriggers:               mapSNSTriggers(info.TriggerConfigurations),
		EC2TagFilterSummary:       mapEC2TagFilters(info.Ec2TagFilters, info.Ec2TagSet),
		OnPremisesTagFilterSummary: mapOnPremisesTagFilters(
			info.OnPremisesInstanceTagFilters, info.OnPremisesTagSet, key,
		),
	}
	return group
}

func mapDeploymentStyle(style *cdtypes.DeploymentStyle) cdservice.DeploymentStyle {
	if style == nil {
		return cdservice.DeploymentStyle{}
	}
	return cdservice.DeploymentStyle{
		DeploymentType:   string(style.DeploymentType),
		DeploymentOption: string(style.DeploymentOption),
	}
}

func mapAutoRollback(config *cdtypes.AutoRollbackConfiguration) cdservice.AutoRollbackConfig {
	if config == nil {
		return cdservice.AutoRollbackConfig{}
	}
	events := make([]string, 0, len(config.Events))
	for _, event := range config.Events {
		events = append(events, string(event))
	}
	return cdservice.AutoRollbackConfig{
		Enabled: config.Enabled,
		Events:  events,
	}
}

func mapAutoScalingGroups(groups []cdtypes.AutoScalingGroup) []string {
	if len(groups) == 0 {
		return nil
	}
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		if name := strings.TrimSpace(aws.ToString(group.Name)); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func mapECSServices(services []cdtypes.ECSService) []cdservice.ECSServiceTarget {
	if len(services) == 0 {
		return nil
	}
	targets := make([]cdservice.ECSServiceTarget, 0, len(services))
	for _, service := range services {
		targets = append(targets, cdservice.ECSServiceTarget{
			ClusterName: strings.TrimSpace(aws.ToString(service.ClusterName)),
			ServiceName: strings.TrimSpace(aws.ToString(service.ServiceName)),
		})
	}
	return targets
}

func mapSNSTriggers(triggers []cdtypes.TriggerConfig) []cdservice.SNSTrigger {
	if len(triggers) == 0 {
		return nil
	}
	out := make([]cdservice.SNSTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		events := make([]string, 0, len(trigger.TriggerEvents))
		for _, event := range trigger.TriggerEvents {
			events = append(events, string(event))
		}
		out = append(out, cdservice.SNSTrigger{
			Name:     strings.TrimSpace(aws.ToString(trigger.TriggerName)),
			TopicARN: strings.TrimSpace(aws.ToString(trigger.TriggerTargetArn)),
			Events:   events,
		})
	}
	return out
}

// mapEC2TagFilters flattens single and grouped EC2 tag filters into a key/type
// summary. The scanner persists the filter shape only, not the selector value,
// matching the EC2-tag-filter-summary contract in the issue.
func mapEC2TagFilters(filters []cdtypes.EC2TagFilter, tagSet *cdtypes.EC2TagSet) []cdservice.TagFilterSummary {
	var summaries []cdservice.TagFilterSummary
	for _, filter := range filters {
		summaries = append(summaries, ec2TagFilterSummary(filter))
	}
	if tagSet != nil {
		for _, group := range tagSet.Ec2TagSetList {
			for _, filter := range group {
				summaries = append(summaries, ec2TagFilterSummary(filter))
			}
		}
	}
	return summaries
}

func ec2TagFilterSummary(filter cdtypes.EC2TagFilter) cdservice.TagFilterSummary {
	value := strings.TrimSpace(aws.ToString(filter.Value))
	return cdservice.TagFilterSummary{
		Key:      strings.TrimSpace(aws.ToString(filter.Key)),
		Type:     string(filter.Type),
		HasValue: value != "",
	}
}

// mapOnPremisesTagFilters flattens on-premises instance tag filters and routes
// every tag value through the redaction library because on-premises instance
// tags may carry customer-PII patterns the issue forbids persisting raw.
func mapOnPremisesTagFilters(
	filters []cdtypes.TagFilter,
	tagSet *cdtypes.OnPremisesTagSet,
	key redact.Key,
) []cdservice.TagFilterSummary {
	var summaries []cdservice.TagFilterSummary
	for _, filter := range filters {
		summaries = append(summaries, onPremisesTagFilterSummary(filter, key))
	}
	if tagSet != nil {
		for _, group := range tagSet.OnPremisesTagSetList {
			for _, filter := range group {
				summaries = append(summaries, onPremisesTagFilterSummary(filter, key))
			}
		}
	}
	return summaries
}

func onPremisesTagFilterSummary(filter cdtypes.TagFilter, key redact.Key) cdservice.TagFilterSummary {
	value := strings.TrimSpace(aws.ToString(filter.Value))
	summary := cdservice.TagFilterSummary{
		Key:      strings.TrimSpace(aws.ToString(filter.Key)),
		Type:     string(filter.Type),
		HasValue: value != "",
	}
	if summary.HasValue {
		summary.ValueMarker = awscloud.RedactString(value, onPremisesTagValueReason, key)
	}
	return summary
}

func mapDeploymentConfig(info cdtypes.DeploymentConfigInfo) cdservice.DeploymentConfig {
	config := cdservice.DeploymentConfig{
		Name:            aws.ToString(info.DeploymentConfigName),
		ID:              aws.ToString(info.DeploymentConfigId),
		ComputePlatform: string(info.ComputePlatform),
	}
	if info.MinimumHealthyHosts != nil {
		config.MinimumHealthyHostType = string(info.MinimumHealthyHosts.Type)
		config.MinimumHealthyHostValue = info.MinimumHealthyHosts.Value
	}
	if info.CreateTime != nil {
		config.CreateTime = info.CreateTime.UTC()
	}
	return config
}

func mapDeployment(info cdtypes.DeploymentInfo) cdservice.Deployment {
	deployment := cdservice.Deployment{
		ID:                   aws.ToString(info.DeploymentId),
		ApplicationName:      aws.ToString(info.ApplicationName),
		DeploymentGroupName:  aws.ToString(info.DeploymentGroupName),
		DeploymentConfigName: aws.ToString(info.DeploymentConfigName),
		Status:               string(info.Status),
		Creator:              string(info.Creator),
		ComputePlatform:      string(info.ComputePlatform),
		RevisionSummary:      mapRevisionSummary(info.Revision),
	}
	if info.CreateTime != nil {
		deployment.CreateTime = info.CreateTime.UTC()
	}
	if info.CompleteTime != nil {
		deployment.CompleteTime = info.CompleteTime.UTC()
	}
	return deployment
}

// mapRevisionSummary keeps only safe revision-source references. It must never
// copy AppSpecContent.Content or String_.Content because those carry
// appspec.yml lifecycle-hook bodies the issue forbids persisting.
func mapRevisionSummary(revision *cdtypes.RevisionLocation) cdservice.RevisionSummary {
	if revision == nil {
		return cdservice.RevisionSummary{}
	}
	summary := cdservice.RevisionSummary{
		RevisionType: string(revision.RevisionType),
	}
	if revision.S3Location != nil {
		summary.S3Bucket = strings.TrimSpace(aws.ToString(revision.S3Location.Bucket))
		summary.S3Key = strings.TrimSpace(aws.ToString(revision.S3Location.Key))
		summary.S3Version = strings.TrimSpace(aws.ToString(revision.S3Location.Version))
		summary.S3BundleType = string(revision.S3Location.BundleType)
	}
	if revision.GitHubLocation != nil {
		summary.GitHubRepo = strings.TrimSpace(aws.ToString(revision.GitHubLocation.Repository))
		summary.GitHubCommitID = strings.TrimSpace(aws.ToString(revision.GitHubLocation.CommitId))
	}
	return summary
}
