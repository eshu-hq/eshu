// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package batch

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// jobQueueRelationships records the compute-environment dispatch order of a
// job queue. Each edge targets the compute environment by its reported
// identity so reducers can join to the compute-environment resource fact.
func jobQueueRelationships(boundary awscloud.Boundary, jobQueue JobQueue) []awscloud.RelationshipObservation {
	jobQueueID := firstNonEmpty(jobQueue.ARN, jobQueue.Name)
	if jobQueueID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, entry := range jobQueue.ComputeEnvironmentOrder {
		computeEnvironment := strings.TrimSpace(entry.ComputeEnvironment)
		if computeEnvironment == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchJobQueueUsesComputeEnvironment,
			SourceResourceID: jobQueueID,
			SourceARN:        strings.TrimSpace(jobQueue.ARN),
			TargetResourceID: computeEnvironment,
			TargetARN:        arnOrEmpty(computeEnvironment),
			TargetType:       awscloud.ResourceTypeBatchComputeEnvironment,
			Attributes:       map[string]any{"order": entry.Order},
			SourceRecordID:   jobQueueID + "#compute-environment#" + computeEnvironment,
		})
	}
	return observations
}

// computeEnvironmentRelationships records the IAM role, subnet, VPC, launch
// template, and security group joins of a compute environment. Every edge sets
// a non-empty target_type matching the target scanner's resource_id form.
func computeEnvironmentRelationships(
	boundary awscloud.Boundary,
	computeEnvironment ComputeEnvironment,
) []awscloud.RelationshipObservation {
	computeEnvironmentID := firstNonEmpty(computeEnvironment.ARN, computeEnvironment.Name)
	if computeEnvironmentID == "" {
		return nil
	}
	computeEnvironmentARN := strings.TrimSpace(computeEnvironment.ARN)
	var observations []awscloud.RelationshipObservation

	addRole := func(roleARN, targetType, recordSuffix string) {
		roleARN = strings.TrimSpace(roleARN)
		if roleARN == "" {
			return
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchComputeEnvironmentUsesIAMRole,
			SourceResourceID: computeEnvironmentID,
			SourceARN:        computeEnvironmentARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       targetType,
			SourceRecordID:   computeEnvironmentID + "#" + recordSuffix + "#" + roleARN,
		})
	}
	addRole(computeEnvironment.ServiceRoleARN, awscloud.ResourceTypeIAMRole, "service-role")
	addRole(computeEnvironment.InstanceRoleARN, instanceRoleTargetType(computeEnvironment.InstanceRoleARN), "instance-role")

	for _, subnetID := range dedupeStrings(computeEnvironment.ComputeResource.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchComputeEnvironmentUsesSubnet,
			SourceResourceID: computeEnvironmentID,
			SourceARN:        computeEnvironmentARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   computeEnvironmentID + "#subnet#" + subnetID,
		})
	}

	for _, securityGroupID := range dedupeStrings(computeEnvironment.ComputeResource.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchComputeEnvironmentUsesSecurityGroup,
			SourceResourceID: computeEnvironmentID,
			SourceARN:        computeEnvironmentARN,
			TargetResourceID: securityGroupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   computeEnvironmentID + "#security-group#" + securityGroupID,
		})
	}

	if launchTemplate := launchTemplateTargetID(computeEnvironment.ComputeResource); launchTemplate != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchComputeEnvironmentUsesLaunchTemplate,
			SourceResourceID: computeEnvironmentID,
			SourceARN:        computeEnvironmentARN,
			TargetResourceID: launchTemplate,
			TargetType:       awscloud.ResourceTypeEC2LaunchTemplate,
			SourceRecordID:   computeEnvironmentID + "#launch-template#" + launchTemplate,
		})
	}

	return observations
}

// jobDefinitionRelationships records the IAM role, container image, and secret
// reference joins of a job definition. Secret edges carry the Secrets Manager
// or SSM ARN reference only; the resolved value is never read.
func jobDefinitionRelationships(
	boundary awscloud.Boundary,
	jobDefinition JobDefinition,
) []awscloud.RelationshipObservation {
	jobDefinitionID := firstNonEmpty(jobDefinition.ARN, strings.TrimSpace(jobDefinition.Name))
	if jobDefinitionID == "" || jobDefinition.Container == nil {
		return nil
	}
	jobDefinitionARN := strings.TrimSpace(jobDefinition.ARN)
	container := jobDefinition.Container
	var observations []awscloud.RelationshipObservation

	for _, roleARN := range dedupeStrings([]string{container.JobRoleARN, container.ExecutionRoleARN}) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchJobDefinitionUsesIAMRole,
			SourceResourceID: jobDefinitionID,
			SourceARN:        jobDefinitionARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   jobDefinitionID + "#role#" + roleARN,
		})
	}

	if image := strings.TrimSpace(container.Image); image != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchJobDefinitionUsesImage,
			SourceResourceID: jobDefinitionID,
			SourceARN:        jobDefinitionARN,
			TargetResourceID: image,
			TargetType:       containerImageTargetType,
			SourceRecordID:   jobDefinitionID + "#container-image#" + image,
		})
	}

	for _, secret := range container.Secrets {
		valueFrom := strings.TrimSpace(secret.ValueFrom)
		if valueFrom == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipBatchJobDefinitionReferencesSecret,
			SourceResourceID: jobDefinitionID,
			SourceARN:        jobDefinitionARN,
			TargetResourceID: valueFrom,
			TargetARN:        valueFrom,
			TargetType:       secretReferenceTargetType(valueFrom),
			Attributes:       map[string]any{"name": strings.TrimSpace(secret.Name)},
			SourceRecordID:   jobDefinitionID + "#secret#" + valueFrom,
		})
	}

	return observations
}

// launchTemplateTargetID prefers the launch template ID and falls back to the
// launch template name when only the name is reported.
func launchTemplateTargetID(computeResource ComputeResource) string {
	return firstNonEmpty(computeResource.LaunchTemplateID, computeResource.LaunchTemplateName)
}

// instanceRoleTargetType classifies a Batch compute-environment instance role
// reference as an IAM instance profile when the ARN form is an instance
// profile, otherwise as an IAM role.
func instanceRoleTargetType(roleARN string) string {
	if strings.Contains(strings.TrimSpace(roleARN), ":instance-profile/") {
		return awscloud.ResourceTypeIAMInstanceProfile
	}
	return awscloud.ResourceTypeIAMRole
}

// secretReferenceTargetType classifies a container secret reference ARN as an
// SSM parameter when the ARN names the ssm service, otherwise as a Secrets
// Manager secret.
func secretReferenceTargetType(valueFrom string) string {
	if strings.HasPrefix(strings.TrimSpace(valueFrom), "arn:") && strings.Contains(valueFrom, ":ssm:") {
		return awscloud.ResourceTypeSSMParameter
	}
	return awscloud.ResourceTypeSecretsManagerSecret
}

// arnOrEmpty returns the candidate when it is an ARN, so relationship target
// ARNs stay accurate for non-ARN identities (bare names).
func arnOrEmpty(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if strings.HasPrefix(candidate, "arn:") {
		return candidate
	}
	return ""
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	return output
}
