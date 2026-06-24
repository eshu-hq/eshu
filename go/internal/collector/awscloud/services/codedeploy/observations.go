// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedeploy

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// applicationARN builds the canonical CodeDeploy application ARN. The
// CodeDeploy list/batch APIs do not return ARNs, so the scanner derives a
// stable identity from the boundary account and region plus the application
// name, matching the documented AWS ARN format.
func applicationARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:application:%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

// deploymentGroupARN builds the canonical CodeDeploy deployment-group ARN from
// the application and group names.
func deploymentGroupARN(boundary awscloud.Boundary, application, group string) string {
	application = strings.TrimSpace(application)
	group = strings.TrimSpace(group)
	if application == "" || group == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:deploymentgroup:%s/%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, application, group)
}

// deploymentConfigARN builds the canonical CodeDeploy deployment-config ARN.
func deploymentConfigARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:deploymentconfig:%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

// ecsServiceARN builds the canonical Amazon ECS service ARN for a CodeDeploy
// ECS deployment target. CodeDeploy reports the target as a cluster/service
// name pair, but the ECS scanner emits its service resource_id as this ARN, so
// the relationship must target the same ARN to join the ECS service node.
func ecsServiceARN(boundary awscloud.Boundary, cluster, service string) string {
	cluster = strings.TrimSpace(cluster)
	service = strings.TrimSpace(service)
	if cluster == "" || service == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:ecs:%s:%s:service/%s/%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, cluster, service)
}

// lambdaFunctionARN builds the canonical AWS Lambda function ARN for a
// CodeDeploy Lambda deployment target. CodeDeploy names the target by function
// name, but the Lambda scanner emits its function resource_id as this ARN, so
// the relationship must target the same ARN to join the function node.
func lambdaFunctionARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:lambda:%s:%s:function:%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, name)
}

// deploymentARN builds the canonical CodeDeploy deployment ARN from the
// deployment ID.
func deploymentARN(boundary awscloud.Boundary, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:codedeploy:%s:%s:deployment:%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, id)
}

func applicationObservation(boundary awscloud.Boundary, app Application) awscloud.ResourceObservation {
	arn := applicationARN(boundary, app.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   firstNonEmpty(arn, app.Name),
		ResourceType: awscloud.ResourceTypeCodeDeployApplication,
		Name:         strings.TrimSpace(app.Name),
		Tags:         cloneStringMap(app.Tags),
		Attributes: map[string]any{
			"application_id":   strings.TrimSpace(app.ID),
			"compute_platform": strings.TrimSpace(app.ComputePlatform),
			"github_account":   strings.TrimSpace(app.GitHubAccount),
			"linked_to_github": app.LinkedToGitHub,
			"create_time":      timeOrNil(app.CreateTime),
		},
		CorrelationAnchors: []string{arn, app.Name},
		SourceRecordID:     firstNonEmpty(arn, app.Name),
	}
}

func deploymentGroupObservation(boundary awscloud.Boundary, group DeploymentGroup) awscloud.ResourceObservation {
	arn := deploymentGroupARN(boundary, group.ApplicationName, group.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   firstNonEmpty(arn, group.Name),
		ResourceType: awscloud.ResourceTypeCodeDeployDeploymentGroup,
		Name:         strings.TrimSpace(group.Name),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"deployment_group_id":         strings.TrimSpace(group.ID),
			"application_name":            strings.TrimSpace(group.ApplicationName),
			"compute_platform":            strings.TrimSpace(group.ComputePlatform),
			"deployment_config_name":      strings.TrimSpace(group.DeploymentConfigName),
			"service_role_arn":            strings.TrimSpace(group.ServiceRoleARN),
			"outdated_instances_strategy": strings.TrimSpace(group.OutdatedInstancesStrategy),
			"termination_hook_enabled":    group.TerminationHookEnabled,
			"deployment_style":            deploymentStyleAttribute(group.DeploymentStyle),
			"auto_rollback":               autoRollbackAttribute(group.AutoRollback),
			"auto_scaling_groups":         cloneStrings(group.AutoScalingGroups),
			"lambda_functions":            cloneStrings(group.LambdaFunctions),
			"ecs_services":                ecsServiceAttributes(group.ECSServices),
			"ec2_tag_filters":             tagFilterAttributes(group.EC2TagFilterSummary),
			"on_premises_tag_filters":     tagFilterAttributes(group.OnPremisesTagFilterSummary),
		},
		CorrelationAnchors: []string{arn, group.Name},
		SourceRecordID:     firstNonEmpty(arn, group.Name),
	}
}

func deploymentConfigObservation(boundary awscloud.Boundary, config DeploymentConfig) awscloud.ResourceObservation {
	arn := deploymentConfigARN(boundary, config.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   firstNonEmpty(arn, config.Name),
		ResourceType: awscloud.ResourceTypeCodeDeployDeploymentConfig,
		Name:         strings.TrimSpace(config.Name),
		Attributes: map[string]any{
			"deployment_config_id":       strings.TrimSpace(config.ID),
			"compute_platform":           strings.TrimSpace(config.ComputePlatform),
			"minimum_healthy_host_type":  strings.TrimSpace(config.MinimumHealthyHostType),
			"minimum_healthy_host_value": config.MinimumHealthyHostValue,
			"create_time":                timeOrNil(config.CreateTime),
		},
		CorrelationAnchors: []string{arn, config.Name},
		SourceRecordID:     firstNonEmpty(arn, config.Name),
	}
}

func deploymentObservation(boundary awscloud.Boundary, deployment Deployment) awscloud.ResourceObservation {
	arn := deploymentARN(boundary, deployment.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   firstNonEmpty(arn, deployment.ID),
		ResourceType: awscloud.ResourceTypeCodeDeployDeployment,
		Name:         strings.TrimSpace(deployment.ID),
		State:        strings.TrimSpace(deployment.Status),
		Attributes: map[string]any{
			"deployment_id":          strings.TrimSpace(deployment.ID),
			"application_name":       strings.TrimSpace(deployment.ApplicationName),
			"deployment_group_name":  strings.TrimSpace(deployment.DeploymentGroupName),
			"deployment_config_name": strings.TrimSpace(deployment.DeploymentConfigName),
			"status":                 strings.TrimSpace(deployment.Status),
			"creator":                strings.TrimSpace(deployment.Creator),
			"compute_platform":       strings.TrimSpace(deployment.ComputePlatform),
			"create_time":            timeOrNil(deployment.CreateTime),
			"complete_time":          timeOrNil(deployment.CompleteTime),
			"revision_summary":       revisionSummaryAttribute(deployment.RevisionSummary),
		},
		CorrelationAnchors: []string{arn, deployment.ID},
		SourceRecordID:     firstNonEmpty(arn, deployment.ID),
	}
}

func deploymentStyleAttribute(style DeploymentStyle) map[string]any {
	return map[string]any{
		"deployment_type":   strings.TrimSpace(style.DeploymentType),
		"deployment_option": strings.TrimSpace(style.DeploymentOption),
	}
}

func autoRollbackAttribute(rollback AutoRollbackConfig) map[string]any {
	return map[string]any{
		"enabled": rollback.Enabled,
		"events":  cloneStrings(rollback.Events),
	}
}

func ecsServiceAttributes(services []ECSServiceTarget) []map[string]any {
	if len(services) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(services))
	for _, service := range services {
		cluster := strings.TrimSpace(service.ClusterName)
		name := strings.TrimSpace(service.ServiceName)
		if cluster == "" && name == "" {
			continue
		}
		out = append(out, map[string]any{
			"cluster_name": cluster,
			"service_name": name,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// tagFilterAttributes maps tag-filter summaries into fact payload maps. The
// value is the redaction marker map when present; raw filter values never
// reach this function.
func tagFilterAttributes(filters []TagFilterSummary) []map[string]any {
	if len(filters) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(filters))
	for _, filter := range filters {
		entry := map[string]any{
			"key":       strings.TrimSpace(filter.Key),
			"type":      strings.TrimSpace(filter.Type),
			"has_value": filter.HasValue,
		}
		if len(filter.ValueMarker) > 0 {
			entry["value"] = cloneAnyMap(filter.ValueMarker)
		}
		out = append(out, entry)
	}
	return out
}

func revisionSummaryAttribute(revision RevisionSummary) map[string]any {
	return map[string]any{
		"revision_type":     strings.TrimSpace(revision.RevisionType),
		"s3_bucket":         strings.TrimSpace(revision.S3Bucket),
		"s3_key":            strings.TrimSpace(revision.S3Key),
		"s3_version":        strings.TrimSpace(revision.S3Version),
		"s3_bundle_type":    strings.TrimSpace(revision.S3BundleType),
		"github_repository": strings.TrimSpace(revision.GitHubRepo),
		"github_commit_id":  strings.TrimSpace(revision.GitHubCommitID),
	}
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
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

func cloneStrings(input []string) []string {
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

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
