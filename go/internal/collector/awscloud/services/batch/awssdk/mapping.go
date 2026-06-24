// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbatchtypes "github.com/aws/aws-sdk-go-v2/service/batch/types"

	batchservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/batch"
)

func mapComputeEnvironment(detail awsbatchtypes.ComputeEnvironmentDetail) batchservice.ComputeEnvironment {
	computeEnvironment := batchservice.ComputeEnvironment{
		ARN:               aws.ToString(detail.ComputeEnvironmentArn),
		Name:              aws.ToString(detail.ComputeEnvironmentName),
		Type:              string(detail.Type),
		State:             string(detail.State),
		Status:            string(detail.Status),
		OrchestrationType: string(detail.ContainerOrchestrationType),
		ServiceRoleARN:    aws.ToString(detail.ServiceRole),
		EcsClusterARN:     aws.ToString(detail.EcsClusterArn),
		Tags:              mapTags(detail.Tags),
	}
	if detail.EksConfiguration != nil {
		computeEnvironment.EksClusterARN = aws.ToString(detail.EksConfiguration.EksClusterArn)
	}
	if detail.ComputeResources != nil {
		computeEnvironment.InstanceRoleARN = aws.ToString(detail.ComputeResources.InstanceRole)
		computeEnvironment.ComputeResource = mapComputeResource(*detail.ComputeResources)
	}
	return computeEnvironment
}

func mapComputeResource(resource awsbatchtypes.ComputeResource) batchservice.ComputeResource {
	computeResource := batchservice.ComputeResource{
		ResourceType:     string(resource.Type),
		SubnetIDs:        cloneStrings(resource.Subnets),
		SecurityGroupIDs: cloneStrings(resource.SecurityGroupIds),
	}
	if resource.LaunchTemplate != nil {
		computeResource.LaunchTemplateID = aws.ToString(resource.LaunchTemplate.LaunchTemplateId)
		computeResource.LaunchTemplateName = aws.ToString(resource.LaunchTemplate.LaunchTemplateName)
	}
	return computeResource
}

func mapJobQueue(detail awsbatchtypes.JobQueueDetail) batchservice.JobQueue {
	return batchservice.JobQueue{
		ARN:                     aws.ToString(detail.JobQueueArn),
		Name:                    aws.ToString(detail.JobQueueName),
		State:                   string(detail.State),
		Status:                  string(detail.Status),
		Type:                    string(detail.JobQueueType),
		Priority:                aws.ToInt32(detail.Priority),
		SchedulingPolicyARN:     aws.ToString(detail.SchedulingPolicyArn),
		ComputeEnvironmentOrder: mapComputeEnvironmentOrder(detail.ComputeEnvironmentOrder),
		Tags:                    mapTags(detail.Tags),
	}
}

func mapComputeEnvironmentOrder(order []awsbatchtypes.ComputeEnvironmentOrder) []batchservice.ComputeEnvironmentOrderEntry {
	if len(order) == 0 {
		return nil
	}
	output := make([]batchservice.ComputeEnvironmentOrderEntry, 0, len(order))
	for _, entry := range order {
		output = append(output, batchservice.ComputeEnvironmentOrderEntry{
			Order:              aws.ToInt32(entry.Order),
			ComputeEnvironment: aws.ToString(entry.ComputeEnvironment),
		})
	}
	return output
}

// mapJobDefinition maps a Batch job definition into the scanner-owned type.
// Container.Command, Parameters, NodeProperties, EcsProperties, and
// EksProperties are intentionally not read: the container command list and job
// parameters never cross the adapter boundary, so they can never be persisted.
func mapJobDefinition(detail awsbatchtypes.JobDefinition) batchservice.JobDefinition {
	return batchservice.JobDefinition{
		ARN:               aws.ToString(detail.JobDefinitionArn),
		Name:              aws.ToString(detail.JobDefinitionName),
		Revision:          aws.ToInt32(detail.Revision),
		Type:              aws.ToString(detail.Type),
		Status:            aws.ToString(detail.Status),
		OrchestrationType: string(detail.ContainerOrchestrationType),
		Container:         mapContainer(detail.ContainerProperties),
		Tags:              mapTags(detail.Tags),
	}
}

// mapContainer maps the non-secret container metadata. ContainerProperties
// Command, Ulimits, MountPoints, Volumes, LinuxParameters, and LogConfiguration
// are intentionally excluded. Environment values are carried only so the
// scanner can replace them with redaction markers; they are never persisted in
// clear text.
func mapContainer(properties *awsbatchtypes.ContainerProperties) *batchservice.Container {
	if properties == nil {
		return nil
	}
	return &batchservice.Container{
		Image:            aws.ToString(properties.Image),
		JobRoleARN:       aws.ToString(properties.JobRoleArn),
		ExecutionRoleARN: aws.ToString(properties.ExecutionRoleArn),
		Environment:      mapEnvironment(properties.Environment),
		Secrets:          mapSecrets(properties.Secrets),
	}
}

func mapEnvironment(environment []awsbatchtypes.KeyValuePair) []batchservice.EnvironmentVariable {
	if len(environment) == 0 {
		return nil
	}
	output := make([]batchservice.EnvironmentVariable, 0, len(environment))
	for _, variable := range environment {
		output = append(output, batchservice.EnvironmentVariable{
			Name:  aws.ToString(variable.Name),
			Value: aws.ToString(variable.Value),
		})
	}
	return output
}

func mapSecrets(secrets []awsbatchtypes.Secret) []batchservice.SecretReference {
	if len(secrets) == 0 {
		return nil
	}
	output := make([]batchservice.SecretReference, 0, len(secrets))
	for _, secret := range secrets {
		output = append(output, batchservice.SecretReference{
			Name:      aws.ToString(secret.Name),
			ValueFrom: aws.ToString(secret.ValueFrom),
		})
	}
	return output
}

// mapSchedulingPolicy maps only the scheduling policy identity. The
// FairsharePolicy and QuotaSharePolicy state is intentionally discarded so the
// fair-share weight state never crosses the adapter boundary.
func mapSchedulingPolicy(detail awsbatchtypes.SchedulingPolicyDetail) batchservice.SchedulingPolicy {
	return batchservice.SchedulingPolicy{
		ARN:  aws.ToString(detail.Arn),
		Name: aws.ToString(detail.Name),
		Tags: mapTags(detail.Tags),
	}
}

// mapJob maps a Batch job summary into the scanner-owned type. The container
// exit code/reason, array properties, node properties, and capacity usage are
// intentionally excluded; only identity, status, and definition reference are
// carried.
func mapJob(summary awsbatchtypes.JobSummary, queueARN string) batchservice.Job {
	return batchservice.Job{
		ID:            aws.ToString(summary.JobId),
		ARN:           aws.ToString(summary.JobArn),
		Name:          aws.ToString(summary.JobName),
		Status:        string(summary.Status),
		JobQueueARN:   strings.TrimSpace(queueARN),
		JobDefinition: aws.ToString(summary.JobDefinition),
		CreatedAt:     epochMillisToTime(summary.CreatedAt),
	}
}

func epochMillisToTime(millis *int64) time.Time {
	if millis == nil || *millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(*millis).UTC()
}

func mapTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for key, value := range tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		output[key] = value
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
