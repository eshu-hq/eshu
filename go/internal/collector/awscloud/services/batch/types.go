// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package batch

import (
	"context"
	"time"
)

// Client is the AWS Batch read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned types. The surface is
// metadata-only: it exposes no SubmitJob, CancelJob, TerminateJob,
// RegisterJobDefinition, or any Create/Update/Delete operation.
type Client interface {
	ListComputeEnvironments(context.Context) ([]ComputeEnvironment, error)
	ListJobQueues(context.Context) ([]JobQueue, error)
	ListJobDefinitions(context.Context) ([]JobDefinition, error)
	ListSchedulingPolicies(context.Context) ([]SchedulingPolicy, error)
	// ListRecentJobs returns recent jobs for one queue bounded by the adapter so
	// the scan stays within the per-scope performance contract. Jobs carry
	// identity, status, and a job-definition reference only.
	ListRecentJobs(context.Context, JobQueue) ([]Job, error)
}

// ComputeEnvironment is the scanner-owned representation of an AWS Batch
// compute environment. Networking and launch-template evidence is carried for
// EC2 topology joins; no secret-bearing field is present.
type ComputeEnvironment struct {
	ARN               string
	Name              string
	Type              string
	State             string
	Status            string
	OrchestrationType string
	ServiceRoleARN    string
	InstanceRoleARN   string
	EcsClusterARN     string
	EksClusterARN     string
	ComputeResource   ComputeResource
	Tags              map[string]string
}

// ComputeResource carries the non-secret compute-resource networking and
// launch-template evidence of a Batch compute environment.
type ComputeResource struct {
	ResourceType       string
	SubnetIDs          []string
	SecurityGroupIDs   []string
	LaunchTemplateID   string
	LaunchTemplateName string
}

// JobQueue is the scanner-owned representation of an AWS Batch job queue.
type JobQueue struct {
	ARN                     string
	Name                    string
	State                   string
	Status                  string
	Type                    string
	Priority                int32
	SchedulingPolicyARN     string
	ComputeEnvironmentOrder []ComputeEnvironmentOrderEntry
	Tags                    map[string]string
}

// ComputeEnvironmentOrderEntry records one ordered compute-environment binding
// of a job queue.
type ComputeEnvironmentOrderEntry struct {
	Order              int32
	ComputeEnvironment string
}

// JobDefinition is the scanner-owned representation of an AWS Batch job
// definition. Container command lists and job parameters are intentionally
// absent from this type so they can never be emitted.
type JobDefinition struct {
	ARN               string
	Name              string
	Revision          int32
	Type              string
	Status            string
	OrchestrationType string
	Container         *Container
	Tags              map[string]string
}

// Container is the scanner-owned representation of a Batch job-definition
// container. Command is intentionally absent. Environment values are redacted
// at the scanner boundary and only HMAC markers are persisted. Secret
// references carry the Secrets Manager / SSM ARN, never the resolved value.
type Container struct {
	Image            string
	JobRoleARN       string
	ExecutionRoleARN string
	Environment      []EnvironmentVariable
	Secrets          []SecretReference
}

// EnvironmentVariable carries one Batch container environment key. The raw
// value is redacted before persistence; it is never stored in clear text.
type EnvironmentVariable struct {
	Name  string
	Value string
}

// SecretReference carries one Batch container secret reference. ValueFrom is a
// Secrets Manager or SSM Parameter Store ARN, not the secret value.
type SecretReference struct {
	Name      string
	ValueFrom string
}

// SchedulingPolicy is the scanner-owned representation of a Batch fair-share
// scheduling policy. Only the name and ARN are carried; the fair-share weight
// state (FairsharePolicy) is intentionally absent so it can never be emitted.
type SchedulingPolicy struct {
	ARN  string
	Name string
	Tags map[string]string
}

// Job is the scanner-owned representation of one recent Batch job. It carries
// identity, status, and a job-definition reference only. Job parameters and
// container overrides are intentionally absent.
type Job struct {
	ID            string
	ARN           string
	Name          string
	Status        string
	JobQueueARN   string
	JobDefinition string
	CreatedAt     time.Time
}
