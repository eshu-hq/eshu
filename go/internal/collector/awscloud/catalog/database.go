// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceRDS identifies the regional Amazon Relational Database Service
	// metadata scan slice.
	ServiceRDS = "rds"
)

const (
	// ResourceTypeRDSDBInstance identifies an RDS DB instance metadata
	// resource.
	ResourceTypeRDSDBInstance = "aws_rds_db_instance"
	// ResourceTypeRDSDBCluster identifies an RDS DB cluster metadata resource.
	ResourceTypeRDSDBCluster = "aws_rds_db_cluster"
	// ResourceTypeRDSDBSubnetGroup identifies an RDS DB subnet group metadata
	// resource.
	ResourceTypeRDSDBSubnetGroup = "aws_rds_db_subnet_group"
)

const (
	// RelationshipRDSDBInstanceMemberOfCluster records an RDS instance's
	// reported DB cluster membership.
	RelationshipRDSDBInstanceMemberOfCluster = "rds_db_instance_member_of_cluster"
	// RelationshipRDSDBInstanceInSubnetGroup records an RDS instance's reported
	// DB subnet group placement.
	RelationshipRDSDBInstanceInSubnetGroup = "rds_db_instance_in_subnet_group"
	// RelationshipRDSDBClusterInSubnetGroup records an RDS cluster's reported
	// DB subnet group placement.
	RelationshipRDSDBClusterInSubnetGroup = "rds_db_cluster_in_subnet_group"
	// RelationshipRDSDBInstanceUsesSecurityGroup records an RDS instance's
	// reported VPC security group attachment.
	RelationshipRDSDBInstanceUsesSecurityGroup = "rds_db_instance_uses_security_group"
	// RelationshipRDSDBClusterUsesSecurityGroup records an RDS cluster's
	// reported VPC security group attachment.
	RelationshipRDSDBClusterUsesSecurityGroup = "rds_db_cluster_uses_security_group"
	// RelationshipRDSDBInstanceUsesKMSKey records an RDS instance's reported KMS
	// key dependency.
	RelationshipRDSDBInstanceUsesKMSKey = "rds_db_instance_uses_kms_key"
	// RelationshipRDSDBClusterUsesKMSKey records an RDS cluster's reported KMS
	// key dependency.
	RelationshipRDSDBClusterUsesKMSKey = "rds_db_cluster_uses_kms_key"
	// RelationshipRDSDBInstanceUsesMonitoringRole records an RDS instance's
	// enhanced-monitoring IAM role dependency.
	RelationshipRDSDBInstanceUsesMonitoringRole = "rds_db_instance_uses_monitoring_role"
	// RelationshipRDSDBClusterUsesIAMRole records an RDS cluster's reported
	// associated IAM role dependency.
	RelationshipRDSDBClusterUsesIAMRole = "rds_db_cluster_uses_iam_role"
	// RelationshipRDSDBInstanceUsesParameterGroup records an RDS instance's
	// reported DB parameter group dependency.
	RelationshipRDSDBInstanceUsesParameterGroup = "rds_db_instance_uses_parameter_group"
	// RelationshipRDSDBClusterUsesParameterGroup records an RDS cluster's
	// reported DB cluster parameter group dependency.
	RelationshipRDSDBClusterUsesParameterGroup = "rds_db_cluster_uses_parameter_group"
	// RelationshipRDSDBInstanceUsesOptionGroup records an RDS instance's
	// reported option group dependency.
	RelationshipRDSDBInstanceUsesOptionGroup = "rds_db_instance_uses_option_group"
)

const (
	// ServiceDynamoDB identifies the regional Amazon DynamoDB metadata scan
	// slice.
	ServiceDynamoDB = "dynamodb"
)

const (
	// ResourceTypeDynamoDBTable identifies a DynamoDB table metadata resource.
	ResourceTypeDynamoDBTable = "aws_dynamodb_table"
)

const (
	// RelationshipDynamoDBTableUsesKMSKey records a DynamoDB table's reported
	// server-side encryption KMS key dependency.
	RelationshipDynamoDBTableUsesKMSKey = "dynamodb_table_uses_kms_key"
)

const (
	// ServiceElastiCache identifies the regional Amazon ElastiCache metadata
	// scan slice for cache clusters, replication groups, parameter and subnet
	// groups, users, user groups, and snapshot metadata.
	ServiceElastiCache = "elasticache"
)

const (
	// ResourceTypeElastiCacheCacheCluster identifies an ElastiCache cache
	// cluster metadata resource.
	ResourceTypeElastiCacheCacheCluster = "aws_elasticache_cache_cluster"
	// ResourceTypeElastiCacheReplicationGroup identifies an ElastiCache
	// replication group metadata resource.
	ResourceTypeElastiCacheReplicationGroup = "aws_elasticache_replication_group"
	// ResourceTypeElastiCacheSubnetGroup identifies an ElastiCache cache
	// subnet group metadata resource.
	ResourceTypeElastiCacheSubnetGroup = "aws_elasticache_subnet_group"
	// ResourceTypeElastiCacheParameterGroup identifies an ElastiCache cache
	// parameter group metadata resource.
	ResourceTypeElastiCacheParameterGroup = "aws_elasticache_parameter_group"
	// ResourceTypeElastiCacheUser identifies an ElastiCache user metadata
	// resource. The scanner never persists Passwords or AccessString fields.
	ResourceTypeElastiCacheUser = "aws_elasticache_user"
	// ResourceTypeElastiCacheUserGroup identifies an ElastiCache user group
	// metadata resource.
	ResourceTypeElastiCacheUserGroup = "aws_elasticache_user_group"
	// ResourceTypeElastiCacheSnapshot identifies an ElastiCache snapshot
	// metadata-only resource (name, source, status only).
	ResourceTypeElastiCacheSnapshot = "aws_elasticache_snapshot"
)

const (
	// RelationshipElastiCacheClusterInVPC records an ElastiCache cache
	// cluster's reported VPC placement, derived from the cache subnet group.
	RelationshipElastiCacheClusterInVPC = "elasticache_cluster_in_vpc"
	// RelationshipElastiCacheClusterInSubnet records an ElastiCache cache
	// cluster's reported subnet placement, derived from the cache subnet group.
	RelationshipElastiCacheClusterInSubnet = "elasticache_cluster_in_subnet"
	// RelationshipElastiCacheClusterUsesKMSKey records an ElastiCache cache
	// cluster's reported at-rest encryption KMS key dependency.
	RelationshipElastiCacheClusterUsesKMSKey = "elasticache_cluster_uses_kms_key"
	// RelationshipElastiCacheReplicationGroupHasCluster records a member cache
	// cluster reported by an ElastiCache replication group.
	RelationshipElastiCacheReplicationGroupHasCluster = "elasticache_replication_group_has_cluster"
	// RelationshipElastiCacheUserGroupHasUser records a user identity listed
	// inside an ElastiCache user group.
	RelationshipElastiCacheUserGroupHasUser = "elasticache_user_group_has_user"
)

const (
	// ServiceRedshift identifies the regional Amazon Redshift metadata-only scan
	// slice. It covers both provisioned Redshift and Redshift Serverless control
	// planes; provisioned and Serverless resource types are distinguished by the
	// emitted resource_type, not the service kind.
	ServiceRedshift = "redshift"
)

const (
	// ResourceTypeRedshiftCluster identifies a provisioned Amazon Redshift
	// cluster metadata resource.
	ResourceTypeRedshiftCluster = "aws_redshift_cluster"
	// ResourceTypeRedshiftClusterParameterGroup identifies a Redshift cluster
	// parameter group metadata resource.
	ResourceTypeRedshiftClusterParameterGroup = "aws_redshift_cluster_parameter_group"
	// ResourceTypeRedshiftClusterSubnetGroup identifies a Redshift cluster subnet
	// group metadata resource.
	ResourceTypeRedshiftClusterSubnetGroup = "aws_redshift_cluster_subnet_group"
	// ResourceTypeRedshiftClusterSnapshot identifies a Redshift cluster snapshot
	// metadata resource. The scanner emits identity/timing metadata only and never
	// persists snapshot contents.
	ResourceTypeRedshiftClusterSnapshot = "aws_redshift_cluster_snapshot"
	// ResourceTypeRedshiftScheduledAction identifies a Redshift scheduled action
	// metadata resource.
	ResourceTypeRedshiftScheduledAction = "aws_redshift_scheduled_action"
	// ResourceTypeRedshiftServerlessNamespace identifies a Redshift Serverless
	// namespace metadata resource.
	ResourceTypeRedshiftServerlessNamespace = "aws_redshift_serverless_namespace"
	// ResourceTypeRedshiftServerlessWorkgroup identifies a Redshift Serverless
	// workgroup metadata resource.
	ResourceTypeRedshiftServerlessWorkgroup = "aws_redshift_serverless_workgroup"
)

const (
	// RelationshipRedshiftClusterInVPC records a Redshift cluster's reported VPC
	// placement.
	RelationshipRedshiftClusterInVPC = "redshift_cluster_in_vpc"
	// RelationshipRedshiftClusterInSubnetGroup records a Redshift cluster's
	// reported cluster subnet group placement.
	RelationshipRedshiftClusterInSubnetGroup = "redshift_cluster_in_subnet_group"
	// RelationshipRedshiftClusterUsesSecurityGroup records a Redshift cluster's
	// reported VPC security group attachment.
	RelationshipRedshiftClusterUsesSecurityGroup = "redshift_cluster_uses_security_group"
	// RelationshipRedshiftClusterUsesKMSKey records a Redshift cluster's reported
	// KMS key dependency.
	RelationshipRedshiftClusterUsesKMSKey = "redshift_cluster_uses_kms_key"
	// RelationshipRedshiftClusterUsesIAMRole records a Redshift cluster's
	// reported IAM role attachment.
	RelationshipRedshiftClusterUsesIAMRole = "redshift_cluster_uses_iam_role"
	// RelationshipRedshiftClusterUsesParameterGroup records a Redshift cluster's
	// reported cluster parameter group dependency.
	RelationshipRedshiftClusterUsesParameterGroup = "redshift_cluster_uses_parameter_group"
	// RelationshipRedshiftClusterSubnetGroupInVPC records a Redshift cluster
	// subnet group's reported VPC placement.
	RelationshipRedshiftClusterSubnetGroupInVPC = "redshift_cluster_subnet_group_in_vpc"
	// RelationshipRedshiftClusterSnapshotOfCluster records a Redshift cluster
	// snapshot's reported source cluster identity.
	RelationshipRedshiftClusterSnapshotOfCluster = "redshift_cluster_snapshot_of_cluster"
	// RelationshipRedshiftClusterSnapshotUsesKMSKey records a Redshift cluster
	// snapshot's reported KMS key dependency.
	RelationshipRedshiftClusterSnapshotUsesKMSKey = "redshift_cluster_snapshot_uses_kms_key"
	// RelationshipRedshiftScheduledActionTargetsCluster records a Redshift
	// scheduled action's reported target cluster identity when the target action
	// reports a cluster identifier.
	RelationshipRedshiftScheduledActionTargetsCluster = "redshift_scheduled_action_targets_cluster"
	// RelationshipRedshiftScheduledActionUsesIAMRole records a Redshift
	// scheduled action's reported IAM role dependency.
	RelationshipRedshiftScheduledActionUsesIAMRole = "redshift_scheduled_action_uses_iam_role"
	// RelationshipRedshiftServerlessWorkgroupInNamespace records a Redshift
	// Serverless workgroup's reported namespace membership.
	RelationshipRedshiftServerlessWorkgroupInNamespace = "redshift_serverless_workgroup_in_namespace"
	// RelationshipRedshiftServerlessWorkgroupUsesSubnet records a Redshift
	// Serverless workgroup's reported subnet placement.
	RelationshipRedshiftServerlessWorkgroupUsesSubnet = "redshift_serverless_workgroup_uses_subnet"
	// RelationshipRedshiftServerlessWorkgroupUsesSecurityGroup records a Redshift
	// Serverless workgroup's reported VPC security group attachment.
	RelationshipRedshiftServerlessWorkgroupUsesSecurityGroup = "redshift_serverless_workgroup_uses_security_group"
	// RelationshipRedshiftServerlessNamespaceUsesKMSKey records a Redshift
	// Serverless namespace's reported KMS key dependency.
	RelationshipRedshiftServerlessNamespaceUsesKMSKey = "redshift_serverless_namespace_uses_kms_key"
	// RelationshipRedshiftServerlessNamespaceUsesIAMRole records a Redshift
	// Serverless namespace's reported IAM role attachment.
	RelationshipRedshiftServerlessNamespaceUsesIAMRole = "redshift_serverless_namespace_uses_iam_role"
)

const (
	// ServiceMemoryDB identifies the regional Amazon MemoryDB metadata scan
	// slice for clusters, subnet groups, parameter groups, users, ACLs, and
	// snapshot metadata.
	ServiceMemoryDB = "memorydb"
)

const (
	// ResourceTypeMemoryDBCluster identifies a MemoryDB cluster metadata
	// resource.
	ResourceTypeMemoryDBCluster = "aws_memorydb_cluster"
	// ResourceTypeMemoryDBSubnetGroup identifies a MemoryDB subnet group
	// metadata resource.
	ResourceTypeMemoryDBSubnetGroup = "aws_memorydb_subnet_group"
	// ResourceTypeMemoryDBParameterGroup identifies a MemoryDB parameter group
	// metadata resource. Only the parameter group name and family are
	// persisted; individual parameter values stay outside the scan.
	ResourceTypeMemoryDBParameterGroup = "aws_memorydb_parameter_group"
	// ResourceTypeMemoryDBUser identifies a MemoryDB user metadata resource.
	// The scanner never persists user passwords or AUTH tokens.
	ResourceTypeMemoryDBUser = "aws_memorydb_user"
	// ResourceTypeMemoryDBACL identifies a MemoryDB Access Control List
	// metadata resource.
	ResourceTypeMemoryDBACL = "aws_memorydb_acl"
	// ResourceTypeMemoryDBSnapshot identifies a MemoryDB snapshot metadata-only
	// resource (name, source cluster, source identity, and status only).
	ResourceTypeMemoryDBSnapshot = "aws_memorydb_snapshot"
)

const (
	// RelationshipMemoryDBClusterInSubnetGroup records a MemoryDB cluster's
	// reported subnet group placement.
	RelationshipMemoryDBClusterInSubnetGroup = "memorydb_cluster_in_subnet_group"
	// RelationshipMemoryDBClusterUsesKMSKey records a MemoryDB cluster's
	// reported at-rest encryption KMS key dependency.
	RelationshipMemoryDBClusterUsesKMSKey = "memorydb_cluster_uses_kms_key"
	// RelationshipMemoryDBClusterNotifiesSNSTopic records the SNS notification
	// topic a MemoryDB cluster reports for event delivery.
	RelationshipMemoryDBClusterNotifiesSNSTopic = "memorydb_cluster_notifies_sns_topic"
	// RelationshipMemoryDBACLHasUser records a user name listed inside a
	// MemoryDB Access Control List.
	RelationshipMemoryDBACLHasUser = "memorydb_acl_has_user"
)

const (
	// ServiceDocDB identifies the regional Amazon DocumentDB (with MongoDB
	// compatibility) metadata scan slice.
	ServiceDocDB = "docdb"
)

const (
	// ResourceTypeDocDBCluster identifies a DocumentDB DB cluster metadata
	// resource.
	ResourceTypeDocDBCluster = "aws_docdb_db_cluster"
	// ResourceTypeDocDBClusterInstance identifies a DocumentDB cluster instance
	// metadata resource.
	ResourceTypeDocDBClusterInstance = "aws_docdb_db_instance"
	// ResourceTypeDocDBClusterParameterGroup identifies a DocumentDB cluster
	// parameter group metadata resource. Only the name, family, and parameter
	// count are persisted; parameter values are never read or stored.
	ResourceTypeDocDBClusterParameterGroup = "aws_docdb_db_cluster_parameter_group"
	// ResourceTypeDocDBClusterSnapshot identifies a DocumentDB cluster snapshot
	// metadata resource. Snapshot contents are never read.
	ResourceTypeDocDBClusterSnapshot = "aws_docdb_db_cluster_snapshot"
	// ResourceTypeDocDBSubnetGroup identifies a DocumentDB DB subnet group
	// metadata resource.
	ResourceTypeDocDBSubnetGroup = "aws_docdb_db_subnet_group"
	// ResourceTypeDocDBGlobalCluster identifies a DocumentDB global cluster
	// metadata resource.
	ResourceTypeDocDBGlobalCluster = "aws_docdb_global_cluster"
	// ResourceTypeDocDBEventSubscription identifies a DocumentDB event
	// subscription metadata resource.
	ResourceTypeDocDBEventSubscription = "aws_docdb_event_subscription"
)

const (
	// RelationshipDocDBClusterInVPC records a DocumentDB cluster's reported VPC
	// placement, derived from its DB subnet group's VPC.
	RelationshipDocDBClusterInVPC = "docdb_db_cluster_in_vpc"
	// RelationshipDocDBClusterInSubnetGroup records a DocumentDB cluster's
	// reported DB subnet group placement.
	RelationshipDocDBClusterInSubnetGroup = "docdb_db_cluster_in_subnet_group"
	// RelationshipDocDBClusterUsesKMSKey records a DocumentDB cluster's reported
	// KMS key dependency.
	RelationshipDocDBClusterUsesKMSKey = "docdb_db_cluster_uses_kms_key"
	// RelationshipDocDBInstanceMemberOfCluster records a DocumentDB cluster
	// instance's reported DB cluster membership.
	RelationshipDocDBInstanceMemberOfCluster = "docdb_db_instance_member_of_cluster"
	// RelationshipDocDBGlobalClusterHasCluster records a DocumentDB global
	// cluster's reported regional DB cluster membership.
	RelationshipDocDBGlobalClusterHasCluster = "docdb_global_cluster_has_cluster"
)

const (
	// ServiceDocDBElastic identifies the regional Amazon DocumentDB Elastic
	// Clusters metadata-only scan slice. The scanner reads elastic cluster
	// control-plane metadata through the docdb-elastic management APIs
	// (ListClusters, GetCluster, ListTagsForResource) and never reads document
	// contents, collections, indexes, query results, or the admin password, and
	// never mutates Elastic Cluster state. It is deliberately a separate service
	// kind from the classic instance-based DocumentDB scanner (ServiceDocDB):
	// DocumentDB Elastic is a distinct sharded service with its own API surface,
	// resource types, and ARNs.
	ServiceDocDBElastic = "docdbelastic"
)

const (
	// ResourceTypeDocDBElasticCluster identifies an Amazon DocumentDB Elastic
	// Clusters cluster metadata resource. The scanner emits identity, status,
	// auth type, shard/instance topology, retention windows, maintenance window,
	// and the KMS and admin-secret references only. It never emits the cluster
	// endpoint connection string, the admin user name in plaintext, or the admin
	// password.
	ResourceTypeDocDBElasticCluster = "aws_docdbelastic_cluster"
)

const (
	// RelationshipDocDBElasticClusterInSubnet records a DocumentDB Elastic
	// cluster's placement in one VPC subnet. The target is keyed by the bare
	// EC2 subnet id (subnet-...) so the edge joins the subnet node the EC2
	// scanner publishes.
	RelationshipDocDBElasticClusterInSubnet = "docdbelastic_cluster_in_subnet"
	// RelationshipDocDBElasticClusterUsesSecurityGroup records a DocumentDB
	// Elastic cluster's attachment to one VPC security group. The target is
	// keyed by the bare EC2 security-group id (sg-...) so the edge joins the
	// security-group node the EC2 scanner publishes.
	RelationshipDocDBElasticClusterUsesSecurityGroup = "docdbelastic_cluster_uses_security_group"
	// RelationshipDocDBElasticClusterUsesKMSKey records a DocumentDB Elastic
	// cluster's reported KMS encryption key dependency. The target is keyed by
	// the reported key id or ARN so the edge joins the KMS key node the KMS
	// scanner publishes.
	RelationshipDocDBElasticClusterUsesKMSKey = "docdbelastic_cluster_uses_kms_key"
	// RelationshipDocDBElasticClusterUsesAdminSecret records a DocumentDB
	// Elastic cluster's reference to the Secrets Manager secret that holds its
	// admin credentials, emitted only for SECRET_ARN auth. The target is keyed
	// by the secret ARN so the edge joins the secret node the Secrets Manager
	// scanner publishes. The secret value is never read.
	RelationshipDocDBElasticClusterUsesAdminSecret = "docdbelastic_cluster_uses_admin_secret" // #nosec G101 -- relationship-type identifier recording a secret ARN reference, not a credential value
)

const (
	// ServiceNeptune identifies the regional Amazon Neptune metadata scan slice.
	// The slice covers both Neptune (provisioned graph database, RDS-shaped) and
	// Neptune Analytics (graph) resources under one service_kind.
	ServiceNeptune = "neptune"
)

const (
	// ResourceTypeNeptuneCluster identifies a Neptune (provisioned) DB cluster
	// metadata resource.
	ResourceTypeNeptuneCluster = "aws_neptune_db_cluster"
	// ResourceTypeNeptuneClusterInstance identifies a Neptune cluster instance
	// metadata resource.
	ResourceTypeNeptuneClusterInstance = "aws_neptune_db_instance"
	// ResourceTypeNeptuneClusterParameterGroup identifies a Neptune cluster
	// parameter group metadata resource. Only the name and family are persisted;
	// parameter values are never read or stored.
	ResourceTypeNeptuneClusterParameterGroup = "aws_neptune_db_cluster_parameter_group"
	// ResourceTypeNeptuneClusterSnapshot identifies a Neptune cluster snapshot
	// metadata resource. Snapshot contents are never read.
	ResourceTypeNeptuneClusterSnapshot = "aws_neptune_db_cluster_snapshot"
	// ResourceTypeNeptuneSubnetGroup identifies a Neptune DB subnet group
	// metadata resource.
	ResourceTypeNeptuneSubnetGroup = "aws_neptune_db_subnet_group"
	// ResourceTypeNeptuneGlobalCluster identifies a Neptune global cluster
	// metadata resource.
	ResourceTypeNeptuneGlobalCluster = "aws_neptune_global_cluster"
	// ResourceTypeNeptuneGraph identifies a Neptune Analytics graph metadata
	// resource. Graph vertex and edge contents are never read or persisted.
	ResourceTypeNeptuneGraph = "aws_neptune_graph"
	// ResourceTypeNeptuneGraphSnapshot identifies a Neptune Analytics graph
	// snapshot metadata resource. Snapshot contents are never read.
	ResourceTypeNeptuneGraphSnapshot = "aws_neptune_graph_snapshot"
)

const (
	// RelationshipNeptuneClusterInVPC records a Neptune cluster's reported VPC
	// placement, derived from its DB subnet group's VPC.
	RelationshipNeptuneClusterInVPC = "neptune_db_cluster_in_vpc"
	// RelationshipNeptuneClusterInSubnetGroup records a Neptune cluster's
	// reported DB subnet group placement.
	RelationshipNeptuneClusterInSubnetGroup = "neptune_db_cluster_in_subnet_group"
	// RelationshipNeptuneClusterUsesKMSKey records a Neptune cluster's reported
	// KMS key dependency.
	RelationshipNeptuneClusterUsesKMSKey = "neptune_db_cluster_uses_kms_key"
	// RelationshipNeptuneClusterUsesIAMRole records a Neptune cluster's reported
	// associated IAM role.
	RelationshipNeptuneClusterUsesIAMRole = "neptune_db_cluster_uses_iam_role"
	// RelationshipNeptuneInstanceMemberOfCluster records a Neptune cluster
	// instance's reported DB cluster membership.
	RelationshipNeptuneInstanceMemberOfCluster = "neptune_db_instance_member_of_cluster"
	// RelationshipNeptuneGlobalClusterHasCluster records a Neptune global
	// cluster's reported regional DB cluster membership.
	RelationshipNeptuneGlobalClusterHasCluster = "neptune_global_cluster_has_cluster"
	// RelationshipNeptuneGraphUsesKMSKey records a Neptune Analytics graph's
	// reported KMS key dependency.
	RelationshipNeptuneGraphUsesKMSKey = "neptune_graph_uses_kms_key"
)
