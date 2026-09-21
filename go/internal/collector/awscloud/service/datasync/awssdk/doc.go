// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 DataSync client into the
// metadata-only DataSync scanner interface.
//
// The adapter uses ListTasks/DescribeTask, ListLocations with the flavor-specific
// DescribeLocationS3/Efs/FsxLustre/FsxOntap/FsxOpenZfs/FsxWindows reads, and
// ListAgents/DescribeAgent. It intentionally excludes CreateTask,
// StartTaskExecution, CancelTaskExecution, UpdateTask, DeleteTask,
// CreateLocation*, CreateAgent, UpdateAgent, DeleteAgent, and every other create,
// start, update, or delete API. It reads only the safe location fields needed to
// join the backing S3 bucket, EFS file system, FSx file system, and IAM role; it
// never reads object-storage access keys, server certificates, or SMB and
// object-storage passwords.
package awssdk
