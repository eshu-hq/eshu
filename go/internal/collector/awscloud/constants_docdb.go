// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDocDB identifies the regional Amazon DocumentDB (with MongoDB
	// compatibility) metadata scan slice.
	ServiceDocDB = "docdb"
)

const (
	// ResourceTypeDocDBCluster identifies a DocumentDB DB cluster metadata
	// resource.
	ResourceTypeDocDBCluster = "aws_docdb_db_cluster"
	// ResourceTypeDocDBClusterInstance identifies a DocumentDB cluster instance
	// metadata resource.
	ResourceTypeDocDBClusterInstance = "aws_docdb_db_instance"
	// ResourceTypeDocDBClusterParameterGroup identifies a DocumentDB cluster
	// parameter group metadata resource. Only the name, family, and parameter
	// count are persisted; parameter values are never read or stored.
	ResourceTypeDocDBClusterParameterGroup = "aws_docdb_db_cluster_parameter_group"
	// ResourceTypeDocDBClusterSnapshot identifies a DocumentDB cluster snapshot
	// metadata resource. Snapshot contents are never read.
	ResourceTypeDocDBClusterSnapshot = "aws_docdb_db_cluster_snapshot"
	// ResourceTypeDocDBSubnetGroup identifies a DocumentDB DB subnet group
	// metadata resource.
	ResourceTypeDocDBSubnetGroup = "aws_docdb_db_subnet_group"
	// ResourceTypeDocDBGlobalCluster identifies a DocumentDB global cluster
	// metadata resource.
	ResourceTypeDocDBGlobalCluster = "aws_docdb_global_cluster"
	// ResourceTypeDocDBEventSubscription identifies a DocumentDB event
	// subscription metadata resource.
	ResourceTypeDocDBEventSubscription = "aws_docdb_event_subscription"
)

const (
	// RelationshipDocDBClusterInVPC records a DocumentDB cluster's reported VPC
	// placement, derived from its DB subnet group's VPC.
	RelationshipDocDBClusterInVPC = "docdb_db_cluster_in_vpc"
	// RelationshipDocDBClusterInSubnetGroup records a DocumentDB cluster's
	// reported DB subnet group placement.
	RelationshipDocDBClusterInSubnetGroup = "docdb_db_cluster_in_subnet_group"
	// RelationshipDocDBClusterUsesKMSKey records a DocumentDB cluster's reported
	// KMS key dependency.
	RelationshipDocDBClusterUsesKMSKey = "docdb_db_cluster_uses_kms_key"
	// RelationshipDocDBInstanceMemberOfCluster records a DocumentDB cluster
	// instance's reported DB cluster membership.
	RelationshipDocDBInstanceMemberOfCluster = "docdb_db_instance_member_of_cluster"
	// RelationshipDocDBGlobalClusterHasCluster records a DocumentDB global
	// cluster's reported regional DB cluster membership.
	RelationshipDocDBGlobalClusterHasCluster = "docdb_global_cluster_has_cluster"
)
