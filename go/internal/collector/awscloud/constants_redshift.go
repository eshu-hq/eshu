// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceRedshift identifies the regional Amazon Redshift metadata-only scan
	// slice. It covers both provisioned Redshift and Redshift Serverless control
	// planes; provisioned and Serverless resource types are distinguished by the
	// emitted resource_type, not the service kind.
	ServiceRedshift = "redshift"
)

const (
	// ResourceTypeRedshiftCluster identifies a provisioned Amazon Redshift
	// cluster metadata resource.
	ResourceTypeRedshiftCluster = "aws_redshift_cluster"
	// ResourceTypeRedshiftClusterParameterGroup identifies a Redshift cluster
	// parameter group metadata resource.
	ResourceTypeRedshiftClusterParameterGroup = "aws_redshift_cluster_parameter_group"
	// ResourceTypeRedshiftClusterSubnetGroup identifies a Redshift cluster subnet
	// group metadata resource.
	ResourceTypeRedshiftClusterSubnetGroup = "aws_redshift_cluster_subnet_group"
	// ResourceTypeRedshiftClusterSnapshot identifies a Redshift cluster snapshot
	// metadata resource. The scanner emits identity/timing metadata only and never
	// persists snapshot contents.
	ResourceTypeRedshiftClusterSnapshot = "aws_redshift_cluster_snapshot"
	// ResourceTypeRedshiftScheduledAction identifies a Redshift scheduled action
	// metadata resource.
	ResourceTypeRedshiftScheduledAction = "aws_redshift_scheduled_action"
	// ResourceTypeRedshiftServerlessNamespace identifies a Redshift Serverless
	// namespace metadata resource.
	ResourceTypeRedshiftServerlessNamespace = "aws_redshift_serverless_namespace"
	// ResourceTypeRedshiftServerlessWorkgroup identifies a Redshift Serverless
	// workgroup metadata resource.
	ResourceTypeRedshiftServerlessWorkgroup = "aws_redshift_serverless_workgroup"
)

const (
	// RelationshipRedshiftClusterInVPC records a Redshift cluster's reported VPC
	// placement.
	RelationshipRedshiftClusterInVPC = "redshift_cluster_in_vpc"
	// RelationshipRedshiftClusterInSubnetGroup records a Redshift cluster's
	// reported cluster subnet group placement.
	RelationshipRedshiftClusterInSubnetGroup = "redshift_cluster_in_subnet_group"
	// RelationshipRedshiftClusterUsesSecurityGroup records a Redshift cluster's
	// reported VPC security group attachment.
	RelationshipRedshiftClusterUsesSecurityGroup = "redshift_cluster_uses_security_group"
	// RelationshipRedshiftClusterUsesKMSKey records a Redshift cluster's reported
	// KMS key dependency.
	RelationshipRedshiftClusterUsesKMSKey = "redshift_cluster_uses_kms_key"
	// RelationshipRedshiftClusterUsesIAMRole records a Redshift cluster's
	// reported IAM role attachment.
	RelationshipRedshiftClusterUsesIAMRole = "redshift_cluster_uses_iam_role"
	// RelationshipRedshiftClusterUsesParameterGroup records a Redshift cluster's
	// reported cluster parameter group dependency.
	RelationshipRedshiftClusterUsesParameterGroup = "redshift_cluster_uses_parameter_group"
	// RelationshipRedshiftClusterSubnetGroupInVPC records a Redshift cluster
	// subnet group's reported VPC placement.
	RelationshipRedshiftClusterSubnetGroupInVPC = "redshift_cluster_subnet_group_in_vpc"
	// RelationshipRedshiftClusterSnapshotOfCluster records a Redshift cluster
	// snapshot's reported source cluster identity.
	RelationshipRedshiftClusterSnapshotOfCluster = "redshift_cluster_snapshot_of_cluster"
	// RelationshipRedshiftClusterSnapshotUsesKMSKey records a Redshift cluster
	// snapshot's reported KMS key dependency.
	RelationshipRedshiftClusterSnapshotUsesKMSKey = "redshift_cluster_snapshot_uses_kms_key"
	// RelationshipRedshiftScheduledActionTargetsCluster records a Redshift
	// scheduled action's reported target cluster identity when the target action
	// reports a cluster identifier.
	RelationshipRedshiftScheduledActionTargetsCluster = "redshift_scheduled_action_targets_cluster"
	// RelationshipRedshiftScheduledActionUsesIAMRole records a Redshift
	// scheduled action's reported IAM role dependency.
	RelationshipRedshiftScheduledActionUsesIAMRole = "redshift_scheduled_action_uses_iam_role"
	// RelationshipRedshiftServerlessWorkgroupInNamespace records a Redshift
	// Serverless workgroup's reported namespace membership.
	RelationshipRedshiftServerlessWorkgroupInNamespace = "redshift_serverless_workgroup_in_namespace"
	// RelationshipRedshiftServerlessWorkgroupUsesSubnet records a Redshift
	// Serverless workgroup's reported subnet placement.
	RelationshipRedshiftServerlessWorkgroupUsesSubnet = "redshift_serverless_workgroup_uses_subnet"
	// RelationshipRedshiftServerlessWorkgroupUsesSecurityGroup records a Redshift
	// Serverless workgroup's reported VPC security group attachment.
	RelationshipRedshiftServerlessWorkgroupUsesSecurityGroup = "redshift_serverless_workgroup_uses_security_group"
	// RelationshipRedshiftServerlessNamespaceUsesKMSKey records a Redshift
	// Serverless namespace's reported KMS key dependency.
	RelationshipRedshiftServerlessNamespaceUsesKMSKey = "redshift_serverless_namespace_uses_kms_key"
	// RelationshipRedshiftServerlessNamespaceUsesIAMRole records a Redshift
	// Serverless namespace's reported IAM role attachment.
	RelationshipRedshiftServerlessNamespaceUsesIAMRole = "redshift_serverless_namespace_uses_iam_role"
)
