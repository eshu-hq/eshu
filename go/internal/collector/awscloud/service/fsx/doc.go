// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package fsx maps Amazon FSx metadata into AWS cloud collector facts across
// every FSx flavor (Windows File Server, Lustre, NetApp ONTAP, OpenZFS).
//
// The package owns scanner-level normalization only. It never calls the AWS SDK
// directly, never reads file contents, and never persists Active Directory
// self-managed credentials (the Windows/ONTAP self-managed AD Password and the
// service-account UserName are unreachable through the Client interface), the
// ONTAP fsxadmin password, or the SVM admin password. SDK adapters provide
// FileSystem, Backup, StorageVirtualMachine, Volume, and Snapshot values;
// Scanner emits aws_resource facts plus file-system-to-VPC, file-system-to-
// subnet, file-system-to-KMS-key, file-system-to-AD-directory,
// backup-to-file-system, SVM-to-file-system, SVM-to-AD-directory,
// volume-to-SVM, and volume-to-file-system relationship evidence.
//
// Every relationship sets a non-empty target_type and a target_resource_id that
// matches the target scanner's resource_id: VPC and subnet edges use the bare
// AWS ID, KMS edges use the key ARN or ID, AD edges use the bare directory ID
// (joining a future Directory Service scanner), and FSx-internal edges upgrade
// to the file system or SVM ARN when the parent is known.
package fsx
