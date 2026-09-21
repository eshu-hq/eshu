// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dms

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Database Migration Service observations for
// one AWS claim. Implementations read control-plane describe APIs and resource
// tags only and never read migrated rows, endpoint credentials, task settings,
// or table-mapping bodies.
type Client interface {
	// Snapshot returns every DMS replication instance, replication subnet group,
	// endpoint, and replication task visible to the configured AWS credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures DMS control-plane metadata plus non-fatal scan warnings.
type Snapshot struct {
	// ReplicationInstances is the metadata-only set of DMS replication
	// instances.
	ReplicationInstances []ReplicationInstance
	// SubnetGroups is the metadata-only set of DMS replication subnet groups.
	SubnetGroups []ReplicationSubnetGroup
	// Endpoints is the metadata-only set of DMS endpoints.
	Endpoints []Endpoint
	// Tasks is the metadata-only set of DMS replication tasks.
	Tasks []ReplicationTask
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// ReplicationInstance is the scanner-owned DMS replication instance model. It
// carries control-plane metadata only.
type ReplicationInstance struct {
	// ARN is the Amazon Resource Name that uniquely identifies the instance.
	ARN string
	// Identifier is the customer replication instance identifier.
	Identifier string
	// Class is the compute class (for example dms.r5.large).
	Class string
	// EngineVersion is the DMS engine version running on the instance.
	EngineVersion string
	// Status is the current lifecycle status (for example available).
	Status string
	// AllocatedStorageGiB is the allocated storage in gibibytes.
	AllocatedStorageGiB int32
	// MultiAZ reports whether the instance is deployed across multiple
	// Availability Zones.
	MultiAZ bool
	// PubliclyAccessible reports whether the instance has a public IP address.
	PubliclyAccessible bool
	// AvailabilityZone is the primary Availability Zone for the instance.
	AvailabilityZone string
	// NetworkType is the reported network type (IPV4 or DUAL).
	NetworkType string
	// KMSKeyID is the KMS key identifier used to encrypt instance storage. AWS
	// reports a key id or key ARN here.
	KMSKeyID string
	// SubnetGroupIdentifier is the replication subnet group the instance runs in.
	SubnetGroupIdentifier string
	// VPCID is the VPC reported on the instance's subnet group, when present.
	VPCID string
	// SubnetIDs are the member subnet ids reported on the instance's subnet
	// group, when present.
	SubnetIDs []string
	// SecurityGroupIDs are the VPC security group ids attached to the instance.
	SecurityGroupIDs []string
	// CreateTime is when the instance was created.
	CreateTime time.Time
	// Tags carries the instance resource tags.
	Tags map[string]string
}

// ReplicationSubnetGroup is the scanner-owned DMS replication subnet group
// model. It carries control-plane metadata only.
type ReplicationSubnetGroup struct {
	// Identifier is the replication subnet group identifier.
	Identifier string
	// Description is the subnet group description.
	Description string
	// Status is the subnet group status.
	Status string
	// VPCID is the VPC the subnet group belongs to.
	VPCID string
	// SubnetIDs are the member subnet ids.
	SubnetIDs []string
	// Tags carries the subnet group resource tags.
	Tags map[string]string
}

// Endpoint is the scanner-owned DMS endpoint model. It carries control-plane
// identity and resolvable data-store references only. It intentionally excludes
// server names used as credentials, usernames, passwords, connection
// attributes, external table definitions, and SSL key material.
type Endpoint struct {
	// ARN is the Amazon Resource Name that uniquely identifies the endpoint.
	ARN string
	// Identifier is the customer endpoint identifier.
	Identifier string
	// EndpointType is the endpoint role (source or target).
	EndpointType string
	// EngineName is the database engine name (for example postgres, s3,
	// kinesis, redshift).
	EngineName string
	// EngineDisplayName is the expanded engine display name.
	EngineDisplayName string
	// SSLMode is the SSL mode used to connect to the endpoint.
	SSLMode string
	// Status is the endpoint status.
	Status string
	// DatabaseName is the database name at the endpoint, when reported. It is
	// configuration metadata, not a credential.
	DatabaseName string
	// Port is the port used to reach the endpoint, when reported.
	Port int32
	// KMSKeyID is the KMS key identifier used to encrypt the endpoint's stored
	// connection parameters.
	KMSKeyID string
	// S3BucketName is the S3 data-store bucket name for an S3 endpoint.
	S3BucketName string
	// KinesisStreamARN is the target Kinesis Data Streams stream ARN for a
	// Kinesis endpoint.
	KinesisStreamARN string
	// SecretsManagerSecretID is the Secrets Manager secret reference (full ARN,
	// partial ARN, or friendly name) used for the endpoint's connection
	// credentials. The secret value is never read.
	SecretsManagerSecretID string
}

// ReplicationTask is the scanner-owned DMS replication task model. It carries
// control-plane identity and endpoint/instance references only. It intentionally
// excludes task settings, table-mapping bodies, CDC start/stop positions, and
// recovery checkpoints.
type ReplicationTask struct {
	// ARN is the Amazon Resource Name that uniquely identifies the task.
	ARN string
	// Identifier is the customer replication task identifier.
	Identifier string
	// MigrationType is the migration type (full-load, cdc, or
	// full-load-and-cdc).
	MigrationType string
	// Status is the task status.
	Status string
	// SourceEndpointARN is the ARN of the task's source endpoint.
	SourceEndpointARN string
	// TargetEndpointARN is the ARN of the task's target endpoint.
	TargetEndpointARN string
	// ReplicationInstanceARN is the ARN of the replication instance the task
	// runs on.
	ReplicationInstanceARN string
	// CreationDate is when the task was created.
	CreationDate time.Time
	// Tags carries the task resource tags.
	Tags map[string]string
}
