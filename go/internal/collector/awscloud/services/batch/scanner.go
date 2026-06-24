// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package batch

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const containerImageTargetType = "container_image"

// Scanner emits AWS Batch compute-environment, job-queue, job-definition,
// scheduling-policy, recent-job, and relationship facts for one claimed
// account and region.
//
// The scanner is metadata-only. It never reads or persists job-definition
// container command lists, environment values in clear text, secret values,
// scheduling-policy fair-share state, job parameters, or container overrides.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes AWS Batch resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("batch scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("batch scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceBatch:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceBatch
	default:
		return nil, fmt.Errorf("batch scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	computeEnvironments, err := s.Client.ListComputeEnvironments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Batch compute environments: %w", err)
	}
	for _, computeEnvironment := range computeEnvironments {
		computeEnvelopes, err := computeEnvironmentEnvelopes(boundary, computeEnvironment)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, computeEnvelopes...)
	}

	jobQueues, err := s.Client.ListJobQueues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Batch job queues: %w", err)
	}
	for _, jobQueue := range jobQueues {
		queueEnvelopes, err := jobQueueEnvelopes(boundary, jobQueue)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, queueEnvelopes...)

		jobs, err := s.Client.ListRecentJobs(ctx, jobQueue)
		if err != nil {
			return nil, fmt.Errorf("list recent Batch jobs for queue %q: %w", jobQueue.Name, err)
		}
		for _, job := range jobs {
			jobEnvelopes, err := jobEnvelopes(boundary, job)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, jobEnvelopes...)
		}
	}

	jobDefinitions, err := s.Client.ListJobDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Batch job definitions: %w", err)
	}
	for _, jobDefinition := range jobDefinitions {
		jobDefinitionEnvelopes, err := s.jobDefinitionEnvelopes(boundary, jobDefinition)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, jobDefinitionEnvelopes...)
	}

	schedulingPolicies, err := s.Client.ListSchedulingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Batch scheduling policies: %w", err)
	}
	for _, schedulingPolicy := range schedulingPolicies {
		resource, err := awscloud.NewResourceEnvelope(schedulingPolicyObservation(boundary, schedulingPolicy))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	return envelopes, nil
}

func computeEnvironmentEnvelopes(
	boundary awscloud.Boundary,
	computeEnvironment ComputeEnvironment,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(computeEnvironmentObservation(boundary, computeEnvironment))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range computeEnvironmentRelationships(boundary, computeEnvironment) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func jobQueueEnvelopes(boundary awscloud.Boundary, jobQueue JobQueue) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(jobQueueObservation(boundary, jobQueue))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range jobQueueRelationships(boundary, jobQueue) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func (s Scanner) jobDefinitionEnvelopes(
	boundary awscloud.Boundary,
	jobDefinition JobDefinition,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(s.jobDefinitionObservation(boundary, jobDefinition))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range jobDefinitionRelationships(boundary, jobDefinition) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func jobEnvelopes(boundary awscloud.Boundary, job Job) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(jobObservation(boundary, job))
	if err != nil {
		return nil, err
	}
	return []facts.Envelope{resource}, nil
}

func computeEnvironmentObservation(
	boundary awscloud.Boundary,
	computeEnvironment ComputeEnvironment,
) awscloud.ResourceObservation {
	computeEnvironmentARN := strings.TrimSpace(computeEnvironment.ARN)
	resourceID := firstNonEmpty(computeEnvironmentARN, computeEnvironment.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          computeEnvironmentARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeBatchComputeEnvironment,
		Name:         strings.TrimSpace(computeEnvironment.Name),
		State:        strings.TrimSpace(computeEnvironment.State),
		Tags:         computeEnvironment.Tags,
		Attributes: map[string]any{
			"compute_resource_type": strings.TrimSpace(computeEnvironment.ComputeResource.ResourceType),
			"ecs_cluster_arn":       strings.TrimSpace(computeEnvironment.EcsClusterARN),
			"eks_cluster_arn":       strings.TrimSpace(computeEnvironment.EksClusterARN),
			"instance_role_arn":     strings.TrimSpace(computeEnvironment.InstanceRoleARN),
			"launch_template_id":    strings.TrimSpace(computeEnvironment.ComputeResource.LaunchTemplateID),
			"launch_template_name":  strings.TrimSpace(computeEnvironment.ComputeResource.LaunchTemplateName),
			"orchestration_type":    strings.TrimSpace(computeEnvironment.OrchestrationType),
			"security_group_ids":    cloneStrings(computeEnvironment.ComputeResource.SecurityGroupIDs),
			"service_role_arn":      strings.TrimSpace(computeEnvironment.ServiceRoleARN),
			"status":                strings.TrimSpace(computeEnvironment.Status),
			"subnet_ids":            cloneStrings(computeEnvironment.ComputeResource.SubnetIDs),
			"type":                  strings.TrimSpace(computeEnvironment.Type),
		},
		CorrelationAnchors: []string{computeEnvironmentARN, strings.TrimSpace(computeEnvironment.Name)},
		SourceRecordID:     resourceID,
	}
}

func jobQueueObservation(boundary awscloud.Boundary, jobQueue JobQueue) awscloud.ResourceObservation {
	jobQueueARN := strings.TrimSpace(jobQueue.ARN)
	resourceID := firstNonEmpty(jobQueueARN, jobQueue.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          jobQueueARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeBatchJobQueue,
		Name:         strings.TrimSpace(jobQueue.Name),
		State:        strings.TrimSpace(jobQueue.State),
		Tags:         jobQueue.Tags,
		Attributes: map[string]any{
			"compute_environment_order": computeEnvironmentOrderMaps(jobQueue.ComputeEnvironmentOrder),
			"priority":                  jobQueue.Priority,
			"scheduling_policy_arn":     strings.TrimSpace(jobQueue.SchedulingPolicyARN),
			"status":                    strings.TrimSpace(jobQueue.Status),
			"type":                      strings.TrimSpace(jobQueue.Type),
		},
		CorrelationAnchors: []string{jobQueueARN, strings.TrimSpace(jobQueue.Name)},
		SourceRecordID:     resourceID,
	}
}

func (s Scanner) jobDefinitionObservation(
	boundary awscloud.Boundary,
	jobDefinition JobDefinition,
) awscloud.ResourceObservation {
	jobDefinitionARN := strings.TrimSpace(jobDefinition.ARN)
	familyRevision := strings.TrimSpace(jobDefinition.Name) + ":" + strconv.Itoa(int(jobDefinition.Revision))
	resourceID := firstNonEmpty(jobDefinitionARN, familyRevision)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          jobDefinitionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeBatchJobDefinition,
		Name:         strings.TrimSpace(jobDefinition.Name),
		State:        strings.TrimSpace(jobDefinition.Status),
		Tags:         jobDefinition.Tags,
		Attributes: map[string]any{
			"container":          s.containerMap(jobDefinition.Container),
			"orchestration_type": strings.TrimSpace(jobDefinition.OrchestrationType),
			"revision":           jobDefinition.Revision,
			"type":               strings.TrimSpace(jobDefinition.Type),
		},
		CorrelationAnchors: []string{jobDefinitionARN, strings.TrimSpace(jobDefinition.Name), familyRevision},
		SourceRecordID:     resourceID,
	}
}

func schedulingPolicyObservation(
	boundary awscloud.Boundary,
	schedulingPolicy SchedulingPolicy,
) awscloud.ResourceObservation {
	schedulingPolicyARN := strings.TrimSpace(schedulingPolicy.ARN)
	resourceID := firstNonEmpty(schedulingPolicyARN, schedulingPolicy.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          schedulingPolicyARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeBatchSchedulingPolicy,
		Name:         strings.TrimSpace(schedulingPolicy.Name),
		Tags:         schedulingPolicy.Tags,
		// No attributes: the fair-share policy weight state is never persisted.
		CorrelationAnchors: []string{schedulingPolicyARN, strings.TrimSpace(schedulingPolicy.Name)},
		SourceRecordID:     resourceID,
	}
}

func jobObservation(boundary awscloud.Boundary, job Job) awscloud.ResourceObservation {
	jobARN := strings.TrimSpace(job.ARN)
	resourceID := firstNonEmpty(jobARN, job.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          jobARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeBatchJob,
		Name:         strings.TrimSpace(job.Name),
		State:        strings.TrimSpace(job.Status),
		Attributes: map[string]any{
			"created_at":     timeOrNil(job.CreatedAt),
			"job_definition": strings.TrimSpace(job.JobDefinition),
			"job_id":         strings.TrimSpace(job.ID),
			"job_queue_arn":  strings.TrimSpace(job.JobQueueARN),
		},
		CorrelationAnchors: []string{jobARN, strings.TrimSpace(job.ID), strings.TrimSpace(job.JobDefinition)},
		SourceRecordID:     resourceID,
	}
}

func (s Scanner) containerMap(container *Container) map[string]any {
	if container == nil {
		return nil
	}
	return map[string]any{
		"environment":        s.environmentVariableMaps(container.Environment),
		"execution_role_arn": strings.TrimSpace(container.ExecutionRoleARN),
		"image":              strings.TrimSpace(container.Image),
		"job_role_arn":       strings.TrimSpace(container.JobRoleARN),
		"secrets":            secretReferenceMaps(container.Secrets),
	}
}

func (s Scanner) environmentVariableMaps(environment []EnvironmentVariable) []map[string]any {
	if len(environment) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(environment))
	for _, variable := range environment {
		source := "batch.job_definition.container.environment." + strings.TrimSpace(variable.Name)
		output = append(output, map[string]any{
			"name":  strings.TrimSpace(variable.Name),
			"value": awscloud.RedactString(variable.Value, source, s.RedactionKey),
		})
	}
	return output
}

func secretReferenceMaps(secrets []SecretReference) []map[string]string {
	if len(secrets) == 0 {
		return nil
	}
	output := make([]map[string]string, 0, len(secrets))
	for _, secret := range secrets {
		output = append(output, map[string]string{
			"name":       strings.TrimSpace(secret.Name),
			"value_from": strings.TrimSpace(secret.ValueFrom),
		})
	}
	return output
}

func computeEnvironmentOrderMaps(order []ComputeEnvironmentOrderEntry) []map[string]any {
	if len(order) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(order))
	for _, entry := range order {
		output = append(output, map[string]any{
			"compute_environment": strings.TrimSpace(entry.ComputeEnvironment),
			"order":               entry.Order,
		})
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
