// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package efs maps Amazon EFS metadata into AWS cloud collector facts.
//
// The package owns scanner-level normalization only. It never calls the AWS
// SDK directly, never reads file contents, and never persists NFS file system
// policy bodies. SDK adapters provide FileSystem and ReplicationConfiguration
// values, and Scanner emits aws_resource facts for file systems, access points,
// mount targets, and replication configurations plus the reported subnet,
// security group, KMS-key, access-point-to-file-system, and
// replication-to-target-file-system relationships.
package efs
