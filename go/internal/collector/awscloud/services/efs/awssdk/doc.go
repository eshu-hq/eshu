// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 EFS calls into scanner-owned
// metadata.
//
// The adapter only calls describe-class reads: DescribeFileSystems,
// DescribeAccessPoints, DescribeMountTargets, DescribeMountTargetSecurityGroups,
// DescribeLifecycleConfiguration, and DescribeReplicationConfigurations. It must
// not call any mutation API (Create/Delete/Put/Update/Modify) and must not call
// DescribeFileSystemPolicy or DescribeBackupPolicy, so NFS file system policy
// bodies never enter the scan slice. The apiClient interface plus the reflection
// guard in client_test.go enforce that contract.
package awssdk
