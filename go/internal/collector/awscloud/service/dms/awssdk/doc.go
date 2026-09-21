// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Database Migration Service client
// into the metadata-only DMS scanner interface.
//
// The adapter uses DescribeReplicationInstances, DescribeReplicationSubnetGroups,
// DescribeEndpoints, DescribeReplicationTasks, and ListTagsForResource to read
// DMS control-plane metadata and resource tags. It intentionally excludes
// TestConnection, RefreshSchemas, ReloadTables, every Start/Stop task API, and
// all Create/Update/Delete mutation APIs, so the adapter cannot read migrated
// rows, test live endpoint connections, or mutate DMS state. It records only
// resolvable data-store references (S3 bucket name, Kinesis stream ARN, Secrets
// Manager secret id) from endpoint settings and never persists server names
// used as credentials, usernames, passwords, connection attributes, external
// table definitions, or SSL key material.
package awssdk
