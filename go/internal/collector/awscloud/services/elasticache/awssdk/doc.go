// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 ElastiCache client into the
// metadata-only ElastiCache scanner interface.
//
// The adapter uses DescribeCacheClusters, DescribeReplicationGroups,
// DescribeCacheSubnetGroups, DescribeCacheParameterGroups, DescribeUsers,
// DescribeUserGroups, DescribeSnapshots, and ListTagsForResource. It
// intentionally excludes CreateCacheCluster, DeleteCacheCluster,
// ModifyCacheCluster, CreateReplicationGroup, DeleteReplicationGroup,
// ModifyReplicationGroup, CreateUser, DeleteUser, ModifyUser, and every other
// mutation, snapshot-payload, or cache-data API. The adapter drops the AWS
// User.Passwords and User.AccessString fields before scanner code sees them so
// AUTH tokens, user passwords, and ACL grant strings can never reach facts or
// logs.
package awssdk
