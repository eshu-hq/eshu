// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesisanalyticsv2

import (
	"context"
	"time"
)

// Client lists metadata-only Amazon Managed Service for Apache Flink (Kinesis
// Data Analytics v2) observations for one claimed account and region.
// Implementations adapt the AWS SDK; the scanner depends on this small surface
// so tests can supply fakes and the SDK adapter owns pagination and telemetry.
type Client interface {
	// ListApplications returns one Application per Managed Flink application in
	// the boundary, already described into scanner-owned metadata.
	ListApplications(ctx context.Context) ([]Application, error)
}

// Application is the scanner-owned view of one Amazon Managed Service for Apache
// Flink (Kinesis Data Analytics v2) application. It carries control-plane
// identity, runtime, parallelism, posture, version, and join-relevant reference
// metadata only. Application code bodies, SQL text, environment property values,
// run-configuration content, and any record payload stay outside the contract.
type Application struct {
	// Name is the application name.
	Name string
	// ARN is the application ARN as AWS reports it.
	ARN string
	// Status is the application status (for example RUNNING, READY, STARTING).
	Status string
	// RuntimeEnvironment is the runtime AWS reports (for example FLINK-1_18,
	// SQL-1_0, ZEPPELIN-FLINK-2_0).
	RuntimeEnvironment string
	// Mode is the application mode (STREAMING for a Managed Flink application,
	// INTERACTIVE for a Studio notebook). It is empty when AWS reports none.
	Mode string
	// Description is the optional application description AWS reports.
	Description string
	// VersionID is the current application version id.
	VersionID int64
	// VersionCount is the number of application versions AWS reports, recorded
	// as a count only; no version code or configuration body is persisted.
	VersionCount int
	// ServiceExecutionRoleARN is the IAM role ARN the application uses to access
	// external resources, when AWS reports one.
	ServiceExecutionRoleARN string
	// CreateTimestamp is when the application was created.
	CreateTimestamp time.Time
	// LastUpdateTimestamp is when the application was last updated.
	LastUpdateTimestamp time.Time

	// AutoScalingEnabled reports whether the Managed Flink parallelism
	// auto-scales in response to throughput.
	AutoScalingEnabled bool
	// ParallelismConfigurationType is the parallelism configuration type AWS
	// reports (CUSTOM or DEFAULT), or "" when not a Flink application.
	ParallelismConfigurationType string
	// Parallelism is the initial parallelism (parallel task count) AWS reports.
	Parallelism int32
	// ParallelismPerKPU is the parallelism per Kinesis Processing Unit AWS
	// reports.
	ParallelismPerKPU int32
	// CurrentParallelism is the current parallel task count AWS reports.
	CurrentParallelism int32

	// SnapshotsEnabled reports whether application snapshots are enabled.
	SnapshotsEnabled bool
	// CodeContentType is the application code content format (PLAINTEXT or
	// ZIPFILE) AWS reports. Only the format is recorded; the code body is never
	// read.
	CodeContentType string
	// CodeS3BucketARN is the S3 bucket ARN holding the application code when the
	// code is stored in S3 (ZIPFILE). AWS reports a full ARN.
	CodeS3BucketARN string
	// CodeS3FileKey is the S3 object key of the application code archive, kept as
	// a join reference only; the object body is never read.
	CodeS3FileKey string

	// InputKinesisStreamARNs holds the SQL input Kinesis data stream ARNs the
	// application reads from.
	InputKinesisStreamARNs []string
	// InputFirehoseStreamARNs holds the SQL input Firehose delivery stream ARNs
	// the application reads from.
	InputFirehoseStreamARNs []string
	// OutputKinesisStreamARNs holds the SQL output Kinesis data stream ARNs the
	// application writes to.
	OutputKinesisStreamARNs []string
	// OutputFirehoseStreamARNs holds the SQL output Firehose delivery stream ARNs
	// the application writes to.
	OutputFirehoseStreamARNs []string

	// VPCConfigurations holds the application's VPC configuration placements.
	VPCConfigurations []VPCConfiguration
	// LogGroupARNs holds the CloudWatch log group ARNs (the reported log stream
	// ARNs with the trailing `:*` wildcard trimmed to the log group form) the
	// application logs to.
	LogGroupARNs []string

	// Snapshots holds metadata-only application snapshot summaries (name, status,
	// version id). No snapshot data or persisted application state is read.
	Snapshots []Snapshot
	// Tags holds AWS resource tags as reported, key to value.
	Tags map[string]string
}

// VPCConfiguration is the scanner-owned view of one application VPC
// configuration. It carries only the join-relevant network placement: the VPC
// id, the subnet ids, and the security group ids.
type VPCConfiguration struct {
	// VPCConfigurationID is the application VPC configuration id.
	VPCConfigurationID string
	// VPCID is the associated VPC id (vpc-…).
	VPCID string
	// SubnetIDs are the bare subnet ids (subnet-…) the configuration uses.
	SubnetIDs []string
	// SecurityGroupIDs are the bare security group ids (sg-…) the configuration
	// uses.
	SecurityGroupIDs []string
}

// Snapshot is the scanner-owned view of one application snapshot. It carries
// the snapshot name, status, and the application version id it was taken at.
// Snapshot data and persisted application state are never read.
type Snapshot struct {
	// Name is the snapshot identifier.
	Name string
	// Status is the snapshot status (for example READY, CREATING).
	Status string
	// ApplicationVersionID is the application version id when the snapshot was
	// created.
	ApplicationVersionID int64
}
