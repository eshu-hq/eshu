// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceElastiCache identifies the regional Amazon ElastiCache metadata
	// scan slice for cache clusters, replication groups, parameter and subnet
	// groups, users, user groups, and snapshot metadata.
	ServiceElastiCache = "elasticache"
)

const (
	// ResourceTypeElastiCacheCacheCluster identifies an ElastiCache cache
	// cluster metadata resource.
	ResourceTypeElastiCacheCacheCluster = "aws_elasticache_cache_cluster"
	// ResourceTypeElastiCacheReplicationGroup identifies an ElastiCache
	// replication group metadata resource.
	ResourceTypeElastiCacheReplicationGroup = "aws_elasticache_replication_group"
	// ResourceTypeElastiCacheSubnetGroup identifies an ElastiCache cache
	// subnet group metadata resource.
	ResourceTypeElastiCacheSubnetGroup = "aws_elasticache_subnet_group"
	// ResourceTypeElastiCacheParameterGroup identifies an ElastiCache cache
	// parameter group metadata resource.
	ResourceTypeElastiCacheParameterGroup = "aws_elasticache_parameter_group"
	// ResourceTypeElastiCacheUser identifies an ElastiCache user metadata
	// resource. The scanner never persists Passwords or AccessString fields.
	ResourceTypeElastiCacheUser = "aws_elasticache_user"
	// ResourceTypeElastiCacheUserGroup identifies an ElastiCache user group
	// metadata resource.
	ResourceTypeElastiCacheUserGroup = "aws_elasticache_user_group"
	// ResourceTypeElastiCacheSnapshot identifies an ElastiCache snapshot
	// metadata-only resource (name, source, status only).
	ResourceTypeElastiCacheSnapshot = "aws_elasticache_snapshot"
)

const (
	// RelationshipElastiCacheClusterInVPC records an ElastiCache cache
	// cluster's reported VPC placement, derived from the cache subnet group.
	RelationshipElastiCacheClusterInVPC = "elasticache_cluster_in_vpc"
	// RelationshipElastiCacheClusterInSubnet records an ElastiCache cache
	// cluster's reported subnet placement, derived from the cache subnet group.
	RelationshipElastiCacheClusterInSubnet = "elasticache_cluster_in_subnet"
	// RelationshipElastiCacheClusterUsesKMSKey records an ElastiCache cache
	// cluster's reported at-rest encryption KMS key dependency.
	RelationshipElastiCacheClusterUsesKMSKey = "elasticache_cluster_uses_kms_key"
	// RelationshipElastiCacheReplicationGroupHasCluster records a member cache
	// cluster reported by an ElastiCache replication group.
	RelationshipElastiCacheReplicationGroupHasCluster = "elasticache_replication_group_has_cluster"
	// RelationshipElastiCacheUserGroupHasUser records a user identity listed
	// inside an ElastiCache user group.
	RelationshipElastiCacheUserGroupHasUser = "elasticache_user_group_has_user"
)
