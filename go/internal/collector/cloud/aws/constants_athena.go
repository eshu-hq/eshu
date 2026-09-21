// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAthena identifies the regional Amazon Athena metadata-only scan
	// slice covering workgroups, data catalogs, prepared-statement names, and
	// named-query identities. Query result rows, query execution result location
	// contents, named-query SQL bodies, prepared-statement query strings, and
	// query history strings stay outside the scan slice.
	ServiceAthena = "athena"
)

const (
	// ResourceTypeAthenaWorkGroup identifies an Athena workgroup metadata
	// resource. The fact never carries query result rows, result-location
	// object contents, or query history strings.
	ResourceTypeAthenaWorkGroup = "aws_athena_workgroup"
	// ResourceTypeAthenaDataCatalog identifies an Athena data catalog metadata
	// resource. The fact never carries data plane records from the catalog.
	ResourceTypeAthenaDataCatalog = "aws_athena_data_catalog"
	// ResourceTypeAthenaPreparedStatement identifies an Athena prepared
	// statement metadata resource. The fact carries the statement name and
	// owning workgroup; the SQL `QueryStatement` body is never persisted.
	ResourceTypeAthenaPreparedStatement = "aws_athena_prepared_statement"
	// ResourceTypeAthenaNamedQuery identifies an Athena named-query metadata
	// resource. The fact carries the named-query identity (id, name, database,
	// workgroup) and never persists the SQL `QueryString` body or query
	// execution history.
	ResourceTypeAthenaNamedQuery = "aws_athena_named_query"
)

const (
	// RelationshipAthenaWorkGroupUsesResultBucket records the S3 result-location
	// bucket reported by an Athena workgroup's result configuration. The edge
	// is reported metadata only; result object contents stay outside facts.
	RelationshipAthenaWorkGroupUsesResultBucket = "athena_workgroup_uses_result_bucket"
	// RelationshipAthenaWorkGroupUsesKMSKey records the KMS key reported by an
	// Athena workgroup's result encryption configuration.
	RelationshipAthenaWorkGroupUsesKMSKey = "athena_workgroup_uses_kms_key"
	// RelationshipAthenaPreparedStatementInWorkGroup records prepared-statement
	// membership in an Athena workgroup.
	RelationshipAthenaPreparedStatementInWorkGroup = "athena_prepared_statement_in_workgroup"
	// RelationshipAthenaNamedQueryInWorkGroup records named-query membership in
	// an Athena workgroup.
	RelationshipAthenaNamedQueryInWorkGroup = "athena_named_query_in_workgroup"
)
