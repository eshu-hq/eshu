// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceTimestream identifies the regional Amazon Timestream for
	// LiveAnalytics metadata-only scan slice. The scanner reads database and
	// table control-plane metadata through the timestream-write management
	// APIs (ListDatabases, ListTables, ListTagsForResource) and never reads
	// time-series records, measures, or query results, and never writes
	// records or mutates Timestream state.
	ServiceTimestream = "timestream"
)

const (
	// ResourceTypeTimestreamDatabase identifies an Amazon Timestream database
	// metadata resource. The scanner emits identity, the KMS encryption key
	// identifier, table count, and lifecycle timestamps only.
	ResourceTypeTimestreamDatabase = "aws_timestream_database"
	// ResourceTypeTimestreamTable identifies an Amazon Timestream table
	// metadata resource. The scanner emits identity, memory and magnetic
	// retention, magnetic-store write configuration, and partition-key schema
	// names only. Time-series records and measure values stay outside the
	// contract.
	ResourceTypeTimestreamTable = "aws_timestream_table"
)

const (
	// RelationshipTimestreamTableInDatabase records a Timestream table's
	// membership in its parent database. The target is keyed by the database
	// ARN so the edge joins the database node the scanner publishes.
	RelationshipTimestreamTableInDatabase = "timestream_table_in_database"
	// RelationshipTimestreamDatabaseUsesKMSKey records a Timestream database's
	// reported KMS encryption key dependency.
	RelationshipTimestreamDatabaseUsesKMSKey = "timestream_database_uses_kms_key"
	// RelationshipTimestreamTableRejectsToS3 records a Timestream table's
	// magnetic-store rejected-data report S3 bucket location.
	RelationshipTimestreamTableRejectsToS3 = "timestream_table_rejects_to_s3"
)
