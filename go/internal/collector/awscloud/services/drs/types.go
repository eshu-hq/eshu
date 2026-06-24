// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drs

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Elastic Disaster Recovery observations for
// one AWS claim. Implementations read control-plane describe APIs only and never
// install or read replication agent secrets, replicated disk data, snapshot
// contents, or job logs, and never start, stop, recover, or mutate DRS state.
type Client interface {
	// Snapshot returns every DRS source server, recovery instance, and
	// replication configuration template visible to the configured AWS
	// credentials in the boundary region.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures DRS source server, recovery instance, and replication
// configuration template metadata plus non-fatal scan warnings.
type Snapshot struct {
	// SourceServers is the metadata-only set of DRS source servers.
	SourceServers []SourceServer
	// RecoveryInstances is the metadata-only set of DRS recovery instances.
	RecoveryInstances []RecoveryInstance
	// ReplicationConfigurationTemplates is the metadata-only set of DRS
	// replication configuration templates.
	ReplicationConfigurationTemplates []ReplicationConfigurationTemplate
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// SourceServer is the scanner-owned DRS source server model. It carries
// control-plane metadata only and intentionally excludes the replication agent
// identity/secret, replicated disk data, and snapshot contents.
type SourceServer struct {
	// SourceServerID is the DRS source server id (s-...).
	SourceServerID string
	// ARN is the Amazon Resource Name that uniquely identifies the source server.
	ARN string
	// Hostname is the reported hostname identification hint, when present.
	Hostname string
	// FQDN is the reported fully qualified domain name identification hint, when
	// present.
	FQDN string
	// OperatingSystem is the reported operating-system description string, when
	// present.
	OperatingSystem string
	// RecoveryInstanceID is the id of the recovery instance associated with this
	// source server, when one exists.
	RecoveryInstanceID string
	// DataReplicationState is the reported data-replication lifecycle state (for
	// example CONTINUOUS or INITIATING).
	DataReplicationState string
	// ReplicationDirection is the reported replication direction (FAILOVER or
	// FAILBACK), when present.
	ReplicationDirection string
	// LastLaunchResult is the reported status of the last recovery launch, when
	// present.
	LastLaunchResult string
	// RecommendedInstanceType is the recommended EC2 instance type AWS reports for
	// recovering the source server, when present.
	RecommendedInstanceType string
	// OriginAccountID is the AWS account id of an EC2-originated source server,
	// when reported.
	OriginAccountID string
	// OriginRegion is the AWS region of an EC2-originated source server, when
	// reported.
	OriginRegion string
	// OriginAvailabilityZone is the AWS availability zone of an EC2-originated
	// source server, when reported.
	OriginAvailabilityZone string
	// Tags carries the source server resource tags.
	Tags map[string]string
}

// RecoveryInstance is the scanner-owned DRS recovery instance model. It carries
// control-plane metadata only and intentionally excludes replicated disk data
// and snapshot contents.
type RecoveryInstance struct {
	// RecoveryInstanceID is the DRS recovery instance id.
	RecoveryInstanceID string
	// ARN is the Amazon Resource Name that uniquely identifies the recovery
	// instance.
	ARN string
	// EC2InstanceID is the bare id (i-...) of the launched EC2 instance backing
	// this recovery instance, when present.
	EC2InstanceID string
	// EC2InstanceState is the reported state of the backing EC2 instance, when
	// present.
	EC2InstanceState string
	// SourceServerID is the id of the source server this recovery instance was
	// recovered from, when reported.
	SourceServerID string
	// IsDrill reports whether this recovery instance was created for a drill
	// rather than an actual recovery event.
	IsDrill bool
	// OriginEnvironment is the reported environment (ON_PREMISES or AWS) of the
	// instance the recovery instance originated from, when present.
	OriginEnvironment string
	// Tags carries the recovery instance resource tags.
	Tags map[string]string
}

// ReplicationConfigurationTemplate is the scanner-owned DRS replication
// configuration template model. It carries control-plane metadata only.
type ReplicationConfigurationTemplate struct {
	// TemplateID is the replication configuration template id.
	TemplateID string
	// ARN is the Amazon Resource Name that uniquely identifies the template.
	ARN string
	// EBSEncryption is the reported EBS encryption mode used during replication
	// (for example DEFAULT or CUSTOM), when present.
	EBSEncryption string
	// StagingAreaSubnetID is the staging-area subnet id used during replication,
	// when present.
	StagingAreaSubnetID string
	// ReplicationServerInstanceType is the replication server EC2 instance type,
	// when present.
	ReplicationServerInstanceType string
	// UseDedicatedReplicationServer reports whether a dedicated replication server
	// is used per source server.
	UseDedicatedReplicationServer bool
	// AssociateDefaultSecurityGroup reports whether the default DRS security group
	// is associated with the template.
	AssociateDefaultSecurityGroup bool
	// Tags carries the template resource tags.
	Tags map[string]string
}
