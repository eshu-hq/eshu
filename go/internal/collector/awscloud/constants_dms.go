// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDMS identifies the regional AWS Database Migration Service
	// metadata-only scan slice. The scanner reads control-plane describe APIs
	// (DescribeReplicationInstances, DescribeReplicationSubnetGroups,
	// DescribeEndpoints, DescribeReplicationTasks) plus ListTagsForResource and
	// never reads migrated table rows, connection credentials, endpoint
	// passwords, task settings, or table-mapping bodies, and never mutates DMS
	// state.
	ServiceDMS = "dms"
)

const (
	// ResourceTypeDMSReplicationInstance identifies an AWS DMS replication
	// instance metadata resource. The scanner emits identity, instance class,
	// engine version, allocated storage, Multi-AZ and public-accessibility
	// flags, status, and KMS/subnet-group/security-group references only.
	ResourceTypeDMSReplicationInstance = "aws_dms_replication_instance"
	// ResourceTypeDMSReplicationSubnetGroup identifies an AWS DMS replication
	// subnet group metadata resource. The scanner emits identity, description,
	// status, VPC id, and the member subnet ids only.
	ResourceTypeDMSReplicationSubnetGroup = "aws_dms_replication_subnet_group"
	// ResourceTypeDMSEndpoint identifies an AWS DMS endpoint metadata resource.
	// The scanner emits identity, endpoint type (source/target), engine name,
	// SSL mode, status, and resolvable data-store/KMS/secret references only. It
	// never persists server names as credentials, usernames, passwords,
	// connection attributes, or external table definitions.
	ResourceTypeDMSEndpoint = "aws_dms_endpoint"
	// ResourceTypeDMSReplicationTask identifies an AWS DMS replication task
	// metadata resource. The scanner emits identity, migration type, status, and
	// the source/target endpoint and replication-instance references only. It
	// never persists task settings, table-mapping bodies, CDC start/stop
	// positions, or recovery checkpoints.
	ResourceTypeDMSReplicationTask = "aws_dms_replication_task"
)

const (
	// RelationshipDMSReplicationInstanceInSubnetGroup records a replication
	// instance's placement in its replication subnet group. The target is keyed
	// by the subnet group identifier the subnet-group node publishes.
	RelationshipDMSReplicationInstanceInSubnetGroup = "dms_replication_instance_in_subnet_group"
	// RelationshipDMSReplicationInstanceInSubnet records a replication instance's
	// reported membership in an EC2 subnet (derived from its subnet group). The
	// target is keyed by the bare subnet id the EC2 scanner publishes.
	RelationshipDMSReplicationInstanceInSubnet = "dms_replication_instance_in_subnet"
	// RelationshipDMSReplicationInstanceUsesSecurityGroup records a replication
	// instance's VPC security group membership. The target is keyed by the bare
	// security-group id the EC2 scanner publishes.
	RelationshipDMSReplicationInstanceUsesSecurityGroup = "dms_replication_instance_uses_security_group"
	// RelationshipDMSReplicationInstanceUsesKMSKey records a replication
	// instance's reported KMS encryption key dependency.
	RelationshipDMSReplicationInstanceUsesKMSKey = "dms_replication_instance_uses_kms_key"
	// RelationshipDMSReplicationSubnetGroupInVPC records a replication subnet
	// group's VPC placement. The target is keyed by the bare VPC id the EC2
	// scanner publishes.
	RelationshipDMSReplicationSubnetGroupInVPC = "dms_replication_subnet_group_in_vpc"
	// RelationshipDMSReplicationSubnetGroupHasSubnet records a replication subnet
	// group's member subnet. The target is keyed by the bare subnet id the EC2
	// scanner publishes.
	RelationshipDMSReplicationSubnetGroupHasSubnet = "dms_replication_subnet_group_has_subnet"
	// RelationshipDMSEndpointUsesKMSKey records a DMS endpoint's reported KMS
	// encryption key dependency for its stored connection parameters.
	RelationshipDMSEndpointUsesKMSKey = "dms_endpoint_uses_kms_key"
	// RelationshipDMSEndpointTargetsS3Bucket records a DMS S3 endpoint's data
	// store bucket. DMS reports a bucket NAME, so the scanner synthesizes the
	// partition-aware bucket ARN to match the S3 scanner's published bucket
	// resource_id.
	RelationshipDMSEndpointTargetsS3Bucket = "dms_endpoint_targets_s3_bucket"
	// RelationshipDMSEndpointTargetsKinesisStream records a DMS Kinesis endpoint's
	// target Kinesis Data Streams stream, keyed by the stream ARN DMS reports.
	RelationshipDMSEndpointTargetsKinesisStream = "dms_endpoint_targets_kinesis_stream"
	// RelationshipDMSEndpointUsesSecret records a DMS endpoint's Secrets Manager
	// secret reference for its connection credentials. The secret value is never
	// read; only the secret id/ARN reference is recorded.
	RelationshipDMSEndpointUsesSecret = "dms_endpoint_uses_secret"
	// RelationshipDMSReplicationTaskUsesSourceEndpoint records a replication
	// task's source endpoint, keyed by the endpoint ARN the endpoint node
	// publishes.
	RelationshipDMSReplicationTaskUsesSourceEndpoint = "dms_replication_task_uses_source_endpoint"
	// RelationshipDMSReplicationTaskUsesTargetEndpoint records a replication
	// task's target endpoint, keyed by the endpoint ARN the endpoint node
	// publishes.
	RelationshipDMSReplicationTaskUsesTargetEndpoint = "dms_replication_task_uses_target_endpoint"
	// RelationshipDMSReplicationTaskRunsOnInstance records a replication task's
	// replication instance, keyed by the instance ARN the instance node
	// publishes.
	RelationshipDMSReplicationTaskRunsOnInstance = "dms_replication_task_runs_on_instance"
)
