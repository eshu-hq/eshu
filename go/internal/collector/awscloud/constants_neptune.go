// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceNeptune identifies the regional Amazon Neptune metadata scan slice.
	// The slice covers both Neptune (provisioned graph database, RDS-shaped) and
	// Neptune Analytics (graph) resources under one service_kind.
	ServiceNeptune = "neptune"
)

const (
	// ResourceTypeNeptuneCluster identifies a Neptune (provisioned) DB cluster
	// metadata resource.
	ResourceTypeNeptuneCluster = "aws_neptune_db_cluster"
	// ResourceTypeNeptuneClusterInstance identifies a Neptune cluster instance
	// metadata resource.
	ResourceTypeNeptuneClusterInstance = "aws_neptune_db_instance"
	// ResourceTypeNeptuneClusterParameterGroup identifies a Neptune cluster
	// parameter group metadata resource. Only the name and family are persisted;
	// parameter values are never read or stored.
	ResourceTypeNeptuneClusterParameterGroup = "aws_neptune_db_cluster_parameter_group"
	// ResourceTypeNeptuneClusterSnapshot identifies a Neptune cluster snapshot
	// metadata resource. Snapshot contents are never read.
	ResourceTypeNeptuneClusterSnapshot = "aws_neptune_db_cluster_snapshot"
	// ResourceTypeNeptuneSubnetGroup identifies a Neptune DB subnet group
	// metadata resource.
	ResourceTypeNeptuneSubnetGroup = "aws_neptune_db_subnet_group"
	// ResourceTypeNeptuneGlobalCluster identifies a Neptune global cluster
	// metadata resource.
	ResourceTypeNeptuneGlobalCluster = "aws_neptune_global_cluster"
	// ResourceTypeNeptuneGraph identifies a Neptune Analytics graph metadata
	// resource. Graph vertex and edge contents are never read or persisted.
	ResourceTypeNeptuneGraph = "aws_neptune_graph"
	// ResourceTypeNeptuneGraphSnapshot identifies a Neptune Analytics graph
	// snapshot metadata resource. Snapshot contents are never read.
	ResourceTypeNeptuneGraphSnapshot = "aws_neptune_graph_snapshot"
)

const (
	// RelationshipNeptuneClusterInVPC records a Neptune cluster's reported VPC
	// placement, derived from its DB subnet group's VPC.
	RelationshipNeptuneClusterInVPC = "neptune_db_cluster_in_vpc"
	// RelationshipNeptuneClusterInSubnetGroup records a Neptune cluster's
	// reported DB subnet group placement.
	RelationshipNeptuneClusterInSubnetGroup = "neptune_db_cluster_in_subnet_group"
	// RelationshipNeptuneClusterUsesKMSKey records a Neptune cluster's reported
	// KMS key dependency.
	RelationshipNeptuneClusterUsesKMSKey = "neptune_db_cluster_uses_kms_key"
	// RelationshipNeptuneClusterUsesIAMRole records a Neptune cluster's reported
	// associated IAM role.
	RelationshipNeptuneClusterUsesIAMRole = "neptune_db_cluster_uses_iam_role"
	// RelationshipNeptuneInstanceMemberOfCluster records a Neptune cluster
	// instance's reported DB cluster membership.
	RelationshipNeptuneInstanceMemberOfCluster = "neptune_db_instance_member_of_cluster"
	// RelationshipNeptuneGlobalClusterHasCluster records a Neptune global
	// cluster's reported regional DB cluster membership.
	RelationshipNeptuneGlobalClusterHasCluster = "neptune_global_cluster_has_cluster"
	// RelationshipNeptuneGraphUsesKMSKey records a Neptune Analytics graph's
	// reported KMS key dependency.
	RelationshipNeptuneGraphUsesKMSKey = "neptune_graph_uses_kms_key"
)
