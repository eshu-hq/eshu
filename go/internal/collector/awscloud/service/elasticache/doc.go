// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package elasticache maps Amazon ElastiCache metadata into AWS cloud collector
// facts.
//
// The scanner emits reported-confidence cache cluster, replication group,
// parameter group, subnet group, user, user group, and snapshot resources plus
// relationships for VPC and subnet placement, KMS dependencies,
// replication-group cluster membership, and user-group membership. Cache keys
// and values, AUTH tokens, user passwords, user access strings, snapshot data,
// and mutation APIs (CreateCacheCluster, DeleteCacheCluster,
// ModifyCacheCluster, CreateReplicationGroup, DeleteReplicationGroup,
// ModifyReplicationGroup, CreateUser, DeleteUser, ModifyUser, and related
// mutation calls) stay outside this package contract.
package elasticache
