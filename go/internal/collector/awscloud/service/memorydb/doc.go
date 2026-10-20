// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package memorydb maps Amazon MemoryDB metadata into AWS cloud collector
// facts.
//
// The scanner emits reported-confidence cluster, subnet group, parameter group,
// user, ACL, and snapshot resources plus relationships for cluster subnet-group
// placement, cluster KMS dependencies, cluster SNS notification topics, and
// ACL-to-user membership. Cache keys and values, AUTH tokens, user passwords,
// the raw user access string, snapshot data, and mutation APIs (CreateCluster,
// DeleteCluster, UpdateCluster, CreateUser, DeleteUser, UpdateUser, CreateACL,
// DeleteACL, UpdateACL, and related mutation calls) stay outside this package
// contract.
package memorydb
