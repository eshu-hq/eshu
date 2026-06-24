// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceMemoryDB identifies the regional Amazon MemoryDB metadata scan
	// slice for clusters, subnet groups, parameter groups, users, ACLs, and
	// snapshot metadata.
	ServiceMemoryDB = "memorydb"
)

const (
	// ResourceTypeMemoryDBCluster identifies a MemoryDB cluster metadata
	// resource.
	ResourceTypeMemoryDBCluster = "aws_memorydb_cluster"
	// ResourceTypeMemoryDBSubnetGroup identifies a MemoryDB subnet group
	// metadata resource.
	ResourceTypeMemoryDBSubnetGroup = "aws_memorydb_subnet_group"
	// ResourceTypeMemoryDBParameterGroup identifies a MemoryDB parameter group
	// metadata resource. Only the parameter group name and family are
	// persisted; individual parameter values stay outside the scan.
	ResourceTypeMemoryDBParameterGroup = "aws_memorydb_parameter_group"
	// ResourceTypeMemoryDBUser identifies a MemoryDB user metadata resource.
	// The scanner never persists user passwords or AUTH tokens.
	ResourceTypeMemoryDBUser = "aws_memorydb_user"
	// ResourceTypeMemoryDBACL identifies a MemoryDB Access Control List
	// metadata resource.
	ResourceTypeMemoryDBACL = "aws_memorydb_acl"
	// ResourceTypeMemoryDBSnapshot identifies a MemoryDB snapshot metadata-only
	// resource (name, source cluster, source identity, and status only).
	ResourceTypeMemoryDBSnapshot = "aws_memorydb_snapshot"
)

const (
	// RelationshipMemoryDBClusterInSubnetGroup records a MemoryDB cluster's
	// reported subnet group placement.
	RelationshipMemoryDBClusterInSubnetGroup = "memorydb_cluster_in_subnet_group"
	// RelationshipMemoryDBClusterUsesKMSKey records a MemoryDB cluster's
	// reported at-rest encryption KMS key dependency.
	RelationshipMemoryDBClusterUsesKMSKey = "memorydb_cluster_uses_kms_key"
	// RelationshipMemoryDBClusterNotifiesSNSTopic records the SNS notification
	// topic a MemoryDB cluster reports for event delivery.
	RelationshipMemoryDBClusterNotifiesSNSTopic = "memorydb_cluster_notifies_sns_topic"
	// RelationshipMemoryDBACLHasUser records a user name listed inside a
	// MemoryDB Access Control List.
	RelationshipMemoryDBACLHasUser = "memorydb_acl_has_user"
)
