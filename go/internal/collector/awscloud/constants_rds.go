// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
