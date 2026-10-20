// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 MemoryDB client into the
// metadata-only MemoryDB scanner interface.
//
// The adapter uses DescribeClusters, DescribeSubnetGroups,
// DescribeParameterGroups, DescribeUsers, DescribeACLs, DescribeSnapshots, and
// ListTags. It intentionally excludes CreateCluster, DeleteCluster,
// UpdateCluster, CreateUser, DeleteUser, UpdateUser, CreateACL, DeleteACL,
// UpdateACL, CopySnapshot, DeleteSnapshot, and every other mutation,
// snapshot-payload, or cache-data API. The adapter drops the AWS
// User.AccessString grant string before scanner code sees it and records only a
// non-secret presence signal so AUTH tokens, user passwords, and ACL grant
// strings can never reach facts or logs.
package awssdk
