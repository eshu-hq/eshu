// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceBatch identifies the regional AWS Batch service scan slice.
	ServiceBatch = "batch"
)

const (
	// ResourceTypeBatchComputeEnvironment identifies an AWS Batch compute
	// environment.
	ResourceTypeBatchComputeEnvironment = "aws_batch_compute_environment"
	// ResourceTypeBatchJobQueue identifies an AWS Batch job queue.
	ResourceTypeBatchJobQueue = "aws_batch_job_queue"
	// ResourceTypeBatchJobDefinition identifies an AWS Batch job definition.
	ResourceTypeBatchJobDefinition = "aws_batch_job_definition"
	// ResourceTypeBatchSchedulingPolicy identifies an AWS Batch fair-share
	// scheduling policy. Only the policy name and ARN are emitted; the
	// fair-share weight state is never persisted.
	ResourceTypeBatchSchedulingPolicy = "aws_batch_scheduling_policy"
	// ResourceTypeBatchJob identifies a single AWS Batch job observed in a
	// recent-jobs listing. Only identity, status, and job-definition reference
	// are emitted; job parameters and container overrides are never persisted.
	ResourceTypeBatchJob = "aws_batch_job"
	// ResourceTypeEC2LaunchTemplate identifies an EC2 launch template referenced
	// by a Batch compute environment. Batch owns this target-type label because
	// no launch-template inventory scanner exists yet; the resource_id is the
	// launch template ID (lt-...) when reported, otherwise the launch template
	// name.
	ResourceTypeEC2LaunchTemplate = "aws_ec2_launch_template"
)

const (
	// RelationshipBatchJobQueueUsesComputeEnvironment records that a job queue
	// dispatches to a compute environment in its compute-environment order.
	RelationshipBatchJobQueueUsesComputeEnvironment = "batch_job_queue_uses_compute_environment"
	// RelationshipBatchComputeEnvironmentUsesSubnet records a subnet placement
	// for a compute environment. The compute-environment-to-VPC join is reached
	// transitively through this edge plus the EC2-owned subnet-to-VPC edge,
	// because the Batch API reports compute-resource subnets but no VPC ID.
	RelationshipBatchComputeEnvironmentUsesSubnet = "batch_compute_environment_uses_subnet"
	// RelationshipBatchComputeEnvironmentUsesSecurityGroup records a security
	// group attached to a compute environment's compute resources, providing the
	// EC2 network-fabric join alongside the subnet edge.
	RelationshipBatchComputeEnvironmentUsesSecurityGroup = "batch_compute_environment_uses_security_group"
	// RelationshipBatchComputeEnvironmentUsesLaunchTemplate records an EC2 or
	// Fargate launch template referenced by a compute environment.
	RelationshipBatchComputeEnvironmentUsesLaunchTemplate = "batch_compute_environment_uses_launch_template"
	// RelationshipBatchComputeEnvironmentUsesIAMRole records the Batch service
	// role or the EC2 instance-profile role used by a compute environment.
	RelationshipBatchComputeEnvironmentUsesIAMRole = "batch_compute_environment_uses_iam_role"
	// RelationshipBatchJobDefinitionUsesIAMRole records the job or execution IAM
	// role declared by a job definition's container properties.
	RelationshipBatchJobDefinitionUsesIAMRole = "batch_job_definition_uses_iam_role"
	// RelationshipBatchJobDefinitionUsesImage records a container image URI
	// referenced by a job definition's container properties.
	RelationshipBatchJobDefinitionUsesImage = "batch_job_definition_uses_image"
	// RelationshipBatchJobDefinitionReferencesSecret records a Secrets Manager
	// or SSM Parameter Store ARN referenced by a job definition container. The
	// reference (value_from ARN) is recorded as an edge; the resolved secret
	// value is never read or persisted.
	RelationshipBatchJobDefinitionReferencesSecret = "batch_job_definition_references_secret"
)
