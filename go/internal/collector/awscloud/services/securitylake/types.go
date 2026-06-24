// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securitylake

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Security Lake observations for one AWS
// claim. Implementations read control-plane configuration through the Security
// Lake management APIs and never read ingested security log records, object
// contents, subscriber credentials, or any data-plane payload.
type Client interface {
	// Snapshot returns the Security Lake data lakes, log sources, and
	// subscribers visible to the configured AWS credentials in the boundary
	// Region.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Security Lake control-plane metadata plus non-fatal scan
// warnings such as sustained throttling that omitted an optional component.
type Snapshot struct {
	// DataLakes is the metadata-only set of Security Lake data lake
	// configurations for the boundary Region.
	DataLakes []DataLake
	// LogSources is the metadata-only set of log sources feeding the data lake.
	LogSources []LogSource
	// Subscribers is the metadata-only set of Security Lake subscribers.
	Subscribers []Subscriber
	// Warnings carries non-fatal partial-scan observations.
	Warnings []awscloud.WarningObservation
}

// DataLake is the scanner-owned Security Lake data lake model. It carries
// control-plane configuration only and never the ingested log records or object
// contents stored in the lake.
type DataLake struct {
	// ARN is the Amazon Resource Name that uniquely identifies the data lake.
	ARN string
	// Region is the AWS Region where this data lake is enabled.
	Region string
	// S3BucketARN is the ARN of the S3 bucket backing the data lake, when AWS
	// reports it. It is a full ARN, matching the S3 scanner's bucket
	// resource_id.
	S3BucketARN string
	// KMSKeyID is the identifier of the KMS key used to encrypt data lake
	// objects, when configured. AWS may report a bare key id, an alias, or an
	// ARN.
	KMSKeyID string
	// CreateStatus is the reported status of the data lake creation request (for
	// example COMPLETED, INITIALIZED, PENDING, FAILED).
	CreateStatus string
	// UpdateStatus is the reported status of the last update/delete request,
	// when present.
	UpdateStatus string
	// ExpirationDays is the configured object expiration in days, when a
	// lifecycle expiration is set. Zero means no expiration is reported.
	ExpirationDays int32
	// TransitionCount is the number of configured lifecycle storage transitions.
	// The scanner records the count, not the individual storage-class values.
	TransitionCount int
	// ReplicationRegions lists the Regions data lake objects replicate to, when
	// replication is configured.
	ReplicationRegions []string
}

// LogSource is the scanner-owned Security Lake log source model. It identifies a
// source feeding the data lake and never the log records that source ingests.
type LogSource struct {
	// Account is the AWS account id the logs are collected for.
	Account string
	// Region is the AWS Region the logs are collected in.
	Region string
	// SourceName is the AWS-native source name (for example ROUTE53,
	// CLOUD_TRAIL_MGMT) or the third-party custom source name.
	SourceName string
	// SourceVersion is the reported source schema version, when present.
	SourceVersion string
	// Custom reports whether this is a third-party custom source rather than an
	// AWS-native source.
	Custom bool
	// ProviderRoleARN is the IAM role ARN a third-party custom source's log
	// provider uses, when reported. It is empty for AWS-native sources.
	ProviderRoleARN string
}

// Subscriber is the scanner-owned Security Lake subscriber model. It carries
// subscriber configuration metadata only and never the subscriber external id,
// endpoint, or any credential material used to access the lake.
type Subscriber struct {
	// ARN is the Amazon Resource Name that uniquely identifies the subscriber.
	ARN string
	// ID is the Security Lake subscriber id.
	ID string
	// Name is the subscriber account name.
	Name string
	// Status is the subscriber status (for example ACTIVE, READY, PENDING).
	Status string
	// AccessTypes lists how the subscriber consumes data (for example S3,
	// LAKEFORMATION).
	AccessTypes []string
	// PrincipalAccount is the AWS account principal granted access. It is an
	// identity reference, not a credential.
	PrincipalAccount string
	// RoleARN is the IAM role ARN the subscriber assumes, when reported. It
	// matches the IAM scanner's published role resource_id.
	RoleARN string
	// S3BucketARN is the ARN of the S3 bucket the subscriber reads, when
	// reported.
	S3BucketARN string
	// SourceNames lists the names of the log sources the subscriber is granted,
	// for scope visibility. Record bodies are never read.
	SourceNames []string
	// CreatedAt is when the subscriber was created.
	CreatedAt time.Time
	// UpdatedAt is when the subscriber was last updated.
	UpdatedAt time.Time
}
