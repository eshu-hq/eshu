// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dms maps AWS Database Migration Service control-plane metadata into
// AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for DMS replication
// instances, replication subnet groups, endpoints, and replication tasks plus
// relationships for instance placement (subnet group, EC2 subnets, EC2 security
// groups), instance and endpoint KMS encryption keys, subnet-group VPC and
// subnet membership, endpoint data-store targets (S3 bucket, Kinesis stream,
// Secrets Manager secret), and task source/target endpoint and replication
// instance references. Migrated table rows, endpoint connection credentials,
// passwords, server names used as credentials, connection attributes, SSL key
// material, external table definitions, task settings, and table-mapping bodies
// stay outside this package contract: the scanner is metadata-only and never
// mutates DMS state.
package dms
