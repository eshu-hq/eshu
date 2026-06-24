// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDRS identifies the regional AWS Elastic Disaster Recovery (DRS)
	// metadata-only scan slice. The scanner reads control-plane describe APIs
	// only (DescribeSourceServers, DescribeRecoveryInstances,
	// DescribeReplicationConfigurationTemplates). It never installs or reads
	// replication agent secrets, never reads replicated disk data or snapshot
	// contents, and never starts, stops, recovers, or mutates DRS state.
	ServiceDRS = "drs"
)

const (
	// ResourceTypeDRSSourceServer identifies an AWS Elastic Disaster Recovery
	// source server metadata resource. The scanner emits identity (source server
	// id and ARN), reported hostname/FQDN identification hints, operating-system
	// description, data-replication state, recommended recovery instance type,
	// EC2-origin cloud properties, and the associated recovery instance id only.
	// Replication agent secrets and replicated disk data stay outside the
	// contract.
	ResourceTypeDRSSourceServer = "aws_drs_source_server"
	// ResourceTypeDRSRecoveryInstance identifies an AWS Elastic Disaster Recovery
	// recovery instance metadata resource. The scanner emits identity (recovery
	// instance id and ARN), the launched EC2 instance id, the EC2 instance state,
	// the originating source server id, the drill flag, and the origin
	// environment only.
	ResourceTypeDRSRecoveryInstance = "aws_drs_recovery_instance"
	// ResourceTypeDRSReplicationConfigurationTemplate identifies an AWS Elastic
	// Disaster Recovery replication configuration template metadata resource. The
	// scanner emits identity (template id and ARN), EBS encryption posture, the
	// staging-area subnet, the replication server instance type, and the
	// dedicated-replication-server flag only.
	ResourceTypeDRSReplicationConfigurationTemplate = "aws_drs_replication_configuration_template"
)

const (
	// RelationshipDRSSourceServerRecoversToInstance records that a DRS source
	// server's reported recovery instance is the DRS recovery instance node the
	// scanner publishes. The target is keyed by the recovery instance id so the
	// edge joins the recovery instance node within the same DRS scan.
	RelationshipDRSSourceServerRecoversToInstance = "drs_source_server_recovers_to_instance"
	// RelationshipDRSRecoveryInstanceRunsOnEC2Instance records that a DRS recovery
	// instance is backed by a launched EC2 instance. The target is keyed by the
	// bare EC2 instance id (i-...) that other scanners use to publish EC2
	// instance identity, so the edge joins the EC2 instance node.
	RelationshipDRSRecoveryInstanceRunsOnEC2Instance = "drs_recovery_instance_runs_on_ec2_instance"
)
