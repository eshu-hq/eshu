// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 FSx calls into scanner-owned metadata
// across every FSx flavor (Windows File Server, Lustre, NetApp ONTAP, OpenZFS).
//
// The adapter only calls describe-class reads: DescribeFileSystems,
// DescribeBackups, DescribeStorageVirtualMachines, DescribeVolumes, and
// DescribeSnapshots. It must not call any mutation API
// (Create/Delete/Update/Restore/Copy/Release) and must not read file contents,
// so file data never enters the scan slice. The apiClient interface plus the
// reflection guard in client_test.go enforce that contract.
//
// The mapping layer reads only safe metadata from the flavor-specific
// configuration blocks. It never maps the Windows/SVM self-managed Active
// Directory credentials (Password, UserName, FileSystemAdministratorsGroup,
// DnsIps, DomainJoinServiceAccountSecret), the ONTAP fsxadmin password, or the
// SVM admin password. Only the AWS Managed Microsoft AD directory ID is read,
// for relationship join keys.
package awssdk
