// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package datasync maps AWS DataSync transfer task, transfer location, and
// agent metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for tasks, locations, and
// agents plus relationships for task-to-source-location,
// task-to-destination-location, task-to-CloudWatch-log-group,
// location-to-S3-bucket, location-to-EFS-file-system,
// location-to-FSx-file-system, and location-to-IAM-role evidence. The object
// and record contents a task transfers, object-storage access keys, server
// certificates, SMB/object-storage passwords, include/exclude filter patterns,
// and every create, start, update, and delete API stay outside this package
// contract.
//
// Task, location, and agent ARNs come from the DataSync API and are used
// directly. The S3 bucket, EFS file system, and FSx file system ARNs that back
// a location are synthesized partition-aware from the bare identifiers in the
// location configuration so the storage edges join the resource each target
// scanner publishes in every AWS partition, never a hardcoded `arn:aws:`.
package datasync
