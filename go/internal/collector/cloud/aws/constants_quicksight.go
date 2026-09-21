// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceQuickSight identifies the regional Amazon QuickSight metadata-only
	// scan slice. The scanner reads QuickSight control-plane metadata through the
	// account-scoped list/describe APIs (ListDataSources, DescribeDataSource,
	// ListDataSets, DescribeDataSet, ListDashboards, DescribeDashboard,
	// ListAnalyses, DescribeAnalysis, ListVPCConnections, ListTagsForResource)
	// and never reads dashboard/analysis SQL, data-source credentials, connection
	// passwords, or any secret connection parameter, and never mutates QuickSight
	// state. Nearly every QuickSight API requires the caller's AWS account id, so
	// the scanner threads boundary.AccountID through each call.
	ServiceQuickSight = "quicksight"
)

const (
	// ResourceTypeQuickSightDataSource identifies an Amazon QuickSight data
	// source metadata resource. The scanner emits identity, the connector type,
	// the resolvable backing-store reference (cluster id, instance id, workgroup,
	// or S3 manifest bucket), the VPC connection reference, and lifecycle
	// timestamps only. Credentials, connection passwords, secret connection
	// parameters, and the Secrets Manager secret value are never read or emitted.
	ResourceTypeQuickSightDataSource = "aws_quicksight_data_source"
	// ResourceTypeQuickSightDataSet identifies an Amazon QuickSight dataset
	// metadata resource. The scanner emits identity, import mode, and the data
	// sources it physically reads, never the column-level data, row-level
	// security values, or any SQL query body.
	ResourceTypeQuickSightDataSet = "aws_quicksight_data_set"
	// ResourceTypeQuickSightDashboard identifies an Amazon QuickSight dashboard
	// metadata resource. The scanner emits identity, published version number,
	// and the datasets the published version reads, never the visual definition
	// or any embedded data values.
	ResourceTypeQuickSightDashboard = "aws_quicksight_dashboard"
	// ResourceTypeQuickSightAnalysis identifies an Amazon QuickSight analysis
	// metadata resource. The scanner emits identity, status, and the datasets the
	// analysis reads, never the visual definition or any embedded data values.
	ResourceTypeQuickSightAnalysis = "aws_quicksight_analysis"
)

const (
	// RelationshipQuickSightDataSourceUsesRedshiftCluster records a QuickSight
	// Redshift data source's backing provisioned Redshift cluster. The target is
	// keyed by the bare cluster id, matching the Redshift scanner's published
	// cluster resource_id (ARN, falling back to identifier) and the convention
	// the Firehose and Kinesis scanners use for the same cluster edge.
	RelationshipQuickSightDataSourceUsesRedshiftCluster = "quicksight_data_source_uses_redshift_cluster"
	// RelationshipQuickSightDataSourceUsesRDSInstance records a QuickSight RDS
	// data source's backing RDS DB instance. The target is keyed by the bare DB
	// instance identifier, matching the RDS scanner's published instance
	// resource_id (ARN, falling back to identifier).
	RelationshipQuickSightDataSourceUsesRDSInstance = "quicksight_data_source_uses_rds_instance"
	// RelationshipQuickSightDataSourceUsesAthenaWorkGroup records a QuickSight
	// Athena data source's backing Athena workgroup. The target is keyed by the
	// bare workgroup name, matching the Athena scanner's published workgroup
	// resource_id.
	RelationshipQuickSightDataSourceUsesAthenaWorkGroup = "quicksight_data_source_uses_athena_workgroup"
	// RelationshipQuickSightDataSourceUsesS3Bucket records a QuickSight S3 data
	// source's backing S3 manifest bucket. QuickSight reports a bucket NAME, so
	// the scanner synthesizes the partition-aware bucket ARN to match the S3
	// scanner's published bucket resource_id (arn:<partition>:s3:::<bucket>).
	RelationshipQuickSightDataSourceUsesS3Bucket = "quicksight_data_source_uses_s3_bucket"
	// RelationshipQuickSightDataSourceUsesSecurityGroup records a security group
	// attached to the VPC connection a QuickSight data source uses. The target is
	// keyed by the bare security group id, matching the EC2 scanner's published
	// security group resource_id.
	RelationshipQuickSightDataSourceUsesSecurityGroup = "quicksight_data_source_uses_security_group"
	// RelationshipQuickSightDataSourceUsesSubnet records a subnet a QuickSight
	// data source's VPC connection network interface resides in. The target is
	// keyed by the bare subnet id, matching the EC2 scanner's published subnet
	// resource_id.
	RelationshipQuickSightDataSourceUsesSubnet = "quicksight_data_source_uses_subnet"
	// RelationshipQuickSightDataSetReadsDataSource records that a QuickSight
	// dataset physically reads from a QuickSight data source. The target is keyed
	// by the data-source ARN this scanner publishes, so the edge joins the
	// data-source node this same scan emitted.
	RelationshipQuickSightDataSetReadsDataSource = "quicksight_data_set_reads_data_source"
	// RelationshipQuickSightDashboardReadsDataSet records that a QuickSight
	// dashboard's published version reads from a QuickSight dataset. The target is
	// keyed by the dataset ARN this scanner publishes.
	RelationshipQuickSightDashboardReadsDataSet = "quicksight_dashboard_reads_data_set"
	// RelationshipQuickSightAnalysisReadsDataSet records that a QuickSight
	// analysis reads from a QuickSight dataset. The target is keyed by the dataset
	// ARN this scanner publishes.
	RelationshipQuickSightAnalysisReadsDataSet = "quicksight_analysis_reads_data_set"
)
