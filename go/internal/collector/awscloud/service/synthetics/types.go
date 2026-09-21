// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package synthetics

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon CloudWatch Synthetics canary
// observations for one AWS claim. Implementations read control-plane metadata
// through DescribeCanaries and never read canary script source code, run
// artifacts (logs, screenshots, HAR files), or run results.
type Client interface {
	// Snapshot returns every Synthetics canary visible to the configured AWS
	// credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Synthetics canary metadata plus non-fatal scan warnings.
type Snapshot struct {
	// Canaries is the metadata-only set of Synthetics canaries.
	Canaries []Canary
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Canary is the scanner-owned CloudWatch Synthetics canary model. It carries
// control-plane metadata only and intentionally excludes the canary script
// source code (handler, source location, zip file), run artifacts, and run
// results.
type Canary struct {
	// ARN is the synthesized partition-aware canary ARN
	// (arn:<partition>:synthetics:<region>:<account>:canary:<name>). The
	// DescribeCanaries response carries no ARN field, so the adapter synthesizes
	// it from the scan boundary.
	ARN string
	// ID is the unique canary id AWS assigns.
	ID string
	// Name is the canary name.
	Name string
	// RuntimeVersion is the Synthetics runtime version (for example
	// syn-nodejs-puppeteer-7.0). It identifies the managed runtime, not the
	// customer script.
	RuntimeVersion string
	// EngineARN is the ARN of the Lambda function used as the canary engine, when
	// reported. It is recorded as a reference value only.
	EngineARN string
	// State is the current canary status state (for example RUNNING or STOPPED).
	State string
	// StateReasonCode is the reason code AWS reports when canary creation or
	// update failed.
	StateReasonCode string
	// ScheduleExpression is the rate or cron expression that defines how often
	// the canary runs.
	ScheduleExpression string
	// ScheduleDurationInSeconds is how long the canary continues making regular
	// runs after creation.
	ScheduleDurationInSeconds int64
	// SuccessRetentionPeriodInDays is the retention window for successful run
	// data.
	SuccessRetentionPeriodInDays int32
	// FailureRetentionPeriodInDays is the retention window for failed run data.
	FailureRetentionPeriodInDays int32
	// RunTimeoutInSeconds is how long a canary run may execute before it stops.
	RunTimeoutInSeconds int32
	// RunMemoryInMB is the maximum memory available to the canary run.
	RunMemoryInMB int32
	// RunActiveTracing reports whether canary runs use active X-Ray tracing.
	RunActiveTracing bool
	// ArtifactS3Location is the reported Amazon S3 location path where Synthetics
	// stores run artifacts. It is a "bucket/prefix" path, not an ARN, and the
	// scanner uses only its bucket-name segment to key the S3 edge.
	ArtifactS3Location string
	// ArtifactEncryptionMode is the encryption mode for run artifacts in S3 (for
	// example SSE_S3 or SSE_KMS), when reported.
	ArtifactEncryptionMode string
	// ArtifactKMSKeyARN is the customer-managed KMS key ARN used to encrypt run
	// artifacts, when SSE-KMS encryption is configured.
	ArtifactKMSKeyARN string
	// ExecutionRoleARN is the ARN of the IAM role the canary assumes to run.
	ExecutionRoleARN string
	// VPCID is the id of the VPC the canary runs in, when VPC-configured.
	VPCID string
	// SubnetIDs are the bare subnet ids (subnet-...) the canary runs in, when
	// VPC-configured.
	SubnetIDs []string
	// SecurityGroupIDs are the bare security group ids (sg-...) applied to the
	// canary, when VPC-configured.
	SecurityGroupIDs []string
	// Created is when the canary was created.
	Created time.Time
	// LastModified is when the canary was most recently modified.
	LastModified time.Time
	// Tags carries the canary resource tags.
	Tags map[string]string
}
