// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

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

const (
	// ServiceKeyspaces identifies the regional Amazon Keyspaces (for Apache
	// Cassandra) metadata-only scan slice. The scanner reads keyspace and table
	// control-plane metadata through ListKeyspaces, GetKeyspace, ListTables, and
	// GetTable only. It never executes CQL, never runs ExecuteStatement,
	// BatchStatement, Select, or any data-plane read against table rows or cells,
	// and never invokes keyspace/table mutation APIs (CreateKeyspace,
	// DeleteKeyspace, UpdateKeyspace, CreateTable, DeleteTable, UpdateTable,
	// RestoreTable, CreateType, DeleteType, TagResource, UntagResource).
	ServiceKeyspaces = "keyspaces"
)

const (
	// ResourceTypeKeyspacesKeyspace identifies an Amazon Keyspaces keyspace
	// metadata resource. The scanner emits keyspace identity, ARN, and
	// replication strategy metadata only; it carries no table row or cell data.
	ResourceTypeKeyspacesKeyspace = "aws_keyspaces_keyspace"
	// ResourceTypeKeyspacesTable identifies an Amazon Keyspaces table metadata
	// resource. The scanner emits table identity, ARN, status, encryption
	// metadata, point-in-time-recovery status, capacity mode, and structural
	// schema column names and types only. Table row data, cell values, and CQL
	// query results are never read or persisted.
	ResourceTypeKeyspacesTable = "aws_keyspaces_table"
)

const (
	// RelationshipKeyspacesTableInKeyspace records that an Amazon Keyspaces table
	// is stored in its parent keyspace. The target is the keyspace resource keyed
	// by its ARN.
	RelationshipKeyspacesTableInKeyspace = "keyspaces_table_in_keyspace"
	// RelationshipKeyspacesTableUsesKMSKey records an Amazon Keyspaces table's
	// reported customer-managed server-side encryption KMS key dependency. The
	// target is the KMS key keyed by its ARN.
	RelationshipKeyspacesTableUsesKMSKey = "keyspaces_table_uses_kms_key"
)

const (
	// ServiceDAX identifies the regional Amazon DynamoDB Accelerator (DAX)
	// metadata-only scan slice. The scanner reads cluster, subnet-group, and
	// parameter-group control-plane metadata through the DAX management APIs
	// (DescribeClusters, DescribeSubnetGroups, DescribeParameterGroups, and
	// ListTags) and never reads cached DynamoDB item data, query results, or node
	// endpoint payloads, and never mutates DAX state.
	ServiceDAX = "dax"
)

const (
	// ResourceTypeDAXCluster identifies an Amazon DAX cluster metadata resource.
	// The scanner emits identity, node type, node counts, status, network type,
	// endpoint encryption type, the assumed IAM role ARN, parameter-group and
	// subnet-group names, security-group memberships, and the server-side
	// encryption status only. Per-node endpoint addresses are recorded as plain
	// connection metadata and never as secrets; cached DynamoDB items and query
	// results stay outside the contract.
	ResourceTypeDAXCluster = "aws_dax_cluster"
	// ResourceTypeDAXSubnetGroup identifies an Amazon DAX subnet group metadata
	// resource. DAX subnet groups have no ARN, so the scanner keys the resource by
	// name and emits the VPC id, member subnet ids, and description only.
	ResourceTypeDAXSubnetGroup = "aws_dax_subnet_group"
	// ResourceTypeDAXParameterGroup identifies an Amazon DAX parameter group
	// metadata resource. DAX parameter groups expose only a name and description
	// through DescribeParameterGroups; the scanner persists those and never reads
	// individual parameter values, which can reveal operational posture.
	ResourceTypeDAXParameterGroup = "aws_dax_parameter_group"
)

const (
	// RelationshipDAXClusterInSubnetGroup records a DAX cluster's placement in its
	// subnet group. DAX subnet groups have no ARN, so the target is keyed by the
	// subnet-group name the subnet-group resource publishes as its resource_id.
	RelationshipDAXClusterInSubnetGroup = "dax_cluster_in_subnet_group"
	// RelationshipDAXClusterUsesSecurityGroup records a DAX cluster's reported
	// VPC security-group membership. The target is keyed by the bare security
	// group id (sg-...), matching how the EC2 scanner publishes a security group.
	RelationshipDAXClusterUsesSecurityGroup = "dax_cluster_uses_security_group"
	// RelationshipDAXClusterAssumesIAMRole records the IAM role a DAX cluster
	// assumes at runtime to access DynamoDB. The target is keyed by the role ARN,
	// matching how the IAM scanner publishes a role.
	RelationshipDAXClusterAssumesIAMRole = "dax_cluster_assumes_iam_role"
	// RelationshipDAXSubnetGroupInVPC records a DAX subnet group's VPC. The target
	// is keyed by the bare VPC id (vpc-...), matching how the EC2 scanner
	// publishes a VPC.
	RelationshipDAXSubnetGroupInVPC = "dax_subnet_group_in_vpc"
	// RelationshipDAXSubnetGroupHasSubnet records a member subnet of a DAX subnet
	// group. The target is keyed by the bare subnet id (subnet-...), matching how
	// the EC2 scanner publishes a subnet.
	RelationshipDAXSubnetGroupHasSubnet = "dax_subnet_group_has_subnet"
)

const (
	// ServiceLakeFormation identifies the regional AWS Lake Formation
	// metadata-only scan slice covering data-lake settings, registered data
	// locations, and the principal/resource permission grants that govern the
	// Glue Data Catalog. Permission policy bodies, LF-Tag values, and principal
	// credentials stay outside the scan slice; only grant identities, principal
	// identifiers, and resource ARNs are emitted.
	ServiceLakeFormation = "lakeformation"
)

const (
	// ResourceTypeLakeFormationResource identifies an AWS Lake Formation
	// registered data location (a `ListResources` entry). Its resource_id is the
	// registered location ARN AWS reports, which is the join key the registered
	// resource's S3-bucket and IAM-role edges resolve against.
	ResourceTypeLakeFormationResource = "aws_lakeformation_resource"
	// ResourceTypeLakeFormationSettings identifies the AWS Lake Formation
	// data-lake settings resource for one account and region. It carries the
	// data-lake administrator and read-only administrator principal identifiers
	// only; no permission body, LF-Tag value, or credential is persisted.
	ResourceTypeLakeFormationSettings = "aws_lakeformation_settings"
	// ResourceTypeLakeFormationPermission identifies one AWS Lake Formation
	// principal/resource permission grant (a `ListPermissions` entry). It carries
	// the grant identity: the principal identifier, the governed resource
	// reference, and the bounded AWS privilege enum names (SELECT, ALTER, ...).
	// No condition expression, LF-Tag value, or policy body is persisted.
	ResourceTypeLakeFormationPermission = "aws_lakeformation_permission"
)

const (
	// RelationshipLakeFormationResourceAtS3Bucket records a Lake Formation
	// registered data location's S3 bucket, derived from the registered location
	// ARN. The target ARN inherits the registered ARN's partition so GovCloud and
	// China joins resolve to the bucket node the S3 scanner publishes.
	RelationshipLakeFormationResourceAtS3Bucket = "lakeformation_resource_at_s3_bucket"
	// RelationshipLakeFormationResourceUsesIAMRole records the IAM role that
	// registered a Lake Formation data location.
	RelationshipLakeFormationResourceUsesIAMRole = "lakeformation_resource_uses_iam_role"
	// RelationshipLakeFormationPermissionOnGlueDatabase records a Lake Formation
	// grant governing a Glue Data Catalog database. The target_resource_id is the
	// bare database name the Glue scanner publishes as its database resource_id.
	RelationshipLakeFormationPermissionOnGlueDatabase = "lakeformation_permission_on_glue_database"
	// RelationshipLakeFormationPermissionOnGlueTable records a Lake Formation
	// grant governing a Glue Data Catalog table. The target_resource_id is the
	// `database/table` identifier the Glue scanner publishes as its table
	// resource_id.
	RelationshipLakeFormationPermissionOnGlueTable = "lakeformation_permission_on_glue_table"
	// RelationshipLakeFormationPermissionGrantedToPrincipal records the IAM
	// principal a Lake Formation grant is granted to, emitted only when the
	// principal identifier is an IAM role ARN.
	RelationshipLakeFormationPermissionGrantedToPrincipal = "lakeformation_permission_granted_to_principal"
)
