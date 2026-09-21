// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mgn

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Application Migration Service observations
// for one AWS claim. Implementations read migration control-plane metadata
// through the MGN management APIs and never read or persist replication-agent
// credentials, replication configuration secrets, or replicated disk contents.
type Client interface {
	// Snapshot returns every MGN application, source server, launch
	// configuration, and job visible to the configured AWS credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures AWS Application Migration Service metadata plus non-fatal
// scan warnings.
type Snapshot struct {
	// Applications is the metadata-only set of MGN applications.
	Applications []Application
	// SourceServers is the metadata-only set of MGN source servers, each
	// carrying its launch configuration when one is reported.
	SourceServers []SourceServer
	// Jobs is the metadata-only set of recent MGN jobs (test and cutover runs).
	Jobs []Job
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Application is the scanner-owned MGN application model. It carries
// control-plane metadata only.
type Application struct {
	// ARN is the Amazon Resource Name that uniquely identifies the application.
	ARN string
	// ApplicationID is the MGN application id.
	ApplicationID string
	// Name is the application name.
	Name string
	// Description is the application description.
	Description string
	// WaveID is the migration wave the application belongs to, when set.
	WaveID string
	// IsArchived reports whether the application is archived.
	IsArchived bool
	// HealthStatus is the aggregated application health status MGN reports.
	HealthStatus string
	// ProgressStatus is the aggregated application progress status MGN reports.
	ProgressStatus string
	// TotalSourceServers is the count of source servers MGN aggregates under the
	// application.
	TotalSourceServers int64
	// CreationTime is when the application was created.
	CreationTime time.Time
	// LastModifiedTime is when the application was last modified.
	LastModifiedTime time.Time
	// Tags carries the application resource tags.
	Tags map[string]string
}

// SourceServer is the scanner-owned MGN source server model. It carries
// control-plane migration metadata only and intentionally excludes
// replication-agent credentials, replicated disk contents, and any replication
// data-plane payload.
type SourceServer struct {
	// ARN is the Amazon Resource Name that uniquely identifies the source server.
	ARN string
	// SourceServerID is the MGN source server id.
	SourceServerID string
	// ApplicationID is the MGN application the source server belongs to, when set.
	ApplicationID string
	// LifeCycleState is the migration lifecycle state (for example READY_FOR_TEST).
	LifeCycleState string
	// DataReplicationState is the data-replication state MGN reports (for example
	// CONTINUOUS). No replication payload, snapshot, or disk content is read.
	DataReplicationState string
	// ReplicationType is the source server replication type MGN reports.
	ReplicationType string
	// IsArchived reports whether the source server is archived.
	IsArchived bool
	// RecommendedInstanceType is the target EC2 instance type MGN recommends.
	RecommendedInstanceType string
	// OS is the reported source operating-system full string.
	OS string
	// Hostname is the non-secret hostname identification hint, when reported.
	Hostname string
	// FQDN is the non-secret fully qualified domain name identification hint.
	FQDN string
	// AWSInstanceID is the non-secret AWS instance identification hint MGN
	// reports for an already-EC2 source, when set.
	AWSInstanceID string
	// LaunchedEC2InstanceID is the bare EC2 instance id (i-...) of the
	// cutover/test instance MGN launched for this source server, when set.
	LaunchedEC2InstanceID string
	// VcenterClientID is the vCenter client id for agentless vCenter sources.
	VcenterClientID string
	// LaunchConfiguration is the launch configuration reported for this source
	// server, when one is available.
	LaunchConfiguration *LaunchConfiguration
	// Tags carries the source server resource tags.
	Tags map[string]string
}

// LaunchConfiguration is the scanner-owned MGN launch configuration model for
// one source server. It carries control-plane launch metadata only.
type LaunchConfiguration struct {
	// SourceServerID is the source server the launch configuration belongs to.
	SourceServerID string
	// Name is the launch configuration name.
	Name string
	// LaunchDisposition is how the launched instance is dispositioned (for
	// example STARTED or STOPPED).
	LaunchDisposition string
	// BootMode is the launch configuration boot mode (for example LEGACY_BIOS).
	BootMode string
	// TargetInstanceTypeRightSizingMethod is the right-sizing method MGN applies
	// when choosing the target instance type.
	TargetInstanceTypeRightSizingMethod string
	// EC2LaunchTemplateID is the EC2 launch template id (lt-...) the launch
	// configuration references, when reported.
	EC2LaunchTemplateID string
	// CopyPrivateIP reports whether the source private IP is copied on launch.
	CopyPrivateIP bool
	// CopyTags reports whether source tags are copied on launch.
	CopyTags bool
}

// Job is the scanner-owned MGN job model (a test or cutover launch run). It
// carries control-plane metadata only.
type Job struct {
	// ARN is the Amazon Resource Name that uniquely identifies the job.
	ARN string
	// JobID is the MGN job id.
	JobID string
	// Type is the job type MGN reports (for example LAUNCH or TERMINATE).
	Type string
	// Status is the job status MGN reports (for example COMPLETED).
	Status string
	// InitiatedBy records who initiated the job (for example START_TEST).
	InitiatedBy string
	// CreationTime is when the job was created.
	CreationTime time.Time
	// EndTime is when the job ended, when reported.
	EndTime time.Time
	// ParticipatingSourceServerIDs are the source server ids the job acted on.
	ParticipatingSourceServerIDs []string
	// Tags carries the job resource tags.
	Tags map[string]string
}
