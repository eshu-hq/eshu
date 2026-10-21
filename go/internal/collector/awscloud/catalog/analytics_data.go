// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Analytics-category constants consolidated from constants_quicksight.go (#6696).

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

// Analytics-category constants consolidated from constants_cleanrooms.go (#6696).

const (
	// ServiceCleanRooms identifies the regional AWS Clean Rooms metadata-only
	// scan slice. The scanner reads collaboration, configured-table, and
	// membership control-plane metadata through the Clean Rooms management APIs
	// (ListCollaborations, ListConfiguredTables, GetConfiguredTable,
	// ListMemberships, ListTagsForResource) and never reads or persists analysis
	// rule SQL, query bodies, allowed-column names, or member secrets, and never
	// mutates Clean Rooms state.
	ServiceCleanRooms = "cleanrooms"
)

const (
	// ResourceTypeCleanRoomsCollaboration identifies an AWS Clean Rooms
	// collaboration metadata resource. The scanner emits identity, the creator
	// account id and display name, the member status, the analytics engine, and
	// lifecycle timestamps only.
	ResourceTypeCleanRoomsCollaboration = "aws_cleanrooms_collaboration"
	// ResourceTypeCleanRoomsConfiguredTable identifies an AWS Clean Rooms
	// configured-table metadata resource. The scanner emits identity, the
	// analysis method, the configured analysis-rule type names, the count of
	// allowed columns (never the column names), and the backing-table reference
	// kind only. Analysis-rule SQL and allowed-column names stay outside the
	// contract.
	ResourceTypeCleanRoomsConfiguredTable = "aws_cleanrooms_configured_table"
	// ResourceTypeCleanRoomsMembership identifies an AWS Clean Rooms membership
	// metadata resource. The scanner emits identity, the associated
	// collaboration identity, the member abilities, and lifecycle timestamps
	// only.
	ResourceTypeCleanRoomsMembership = "aws_cleanrooms_membership"
)

const (
	// RelationshipCleanRoomsConfiguredTableUsesGlueTable records a Clean Rooms
	// configured table's backing AWS Glue Data Catalog table. Clean Rooms reports
	// the Glue database and table names, so the target is keyed by the
	// "<database>/<table>" resource_id the Glue scanner publishes for a table
	// node, joining the configured table to its Glue source.
	RelationshipCleanRoomsConfiguredTableUsesGlueTable = "cleanrooms_configured_table_uses_glue_table"
	// RelationshipCleanRoomsMembershipInCollaboration records a Clean Rooms
	// membership's association with its collaboration. The target is keyed by the
	// collaboration ARN the collaboration node publishes, so the edge joins the
	// collaboration node within the same service.
	RelationshipCleanRoomsMembershipInCollaboration = "cleanrooms_membership_in_collaboration"
)

// Analytics-category constants consolidated from constants_datazone.go (#6696).

const (
	// ServiceDatazone identifies the regional Amazon DataZone metadata-only scan
	// slice. The scanner reads governance control-plane metadata through the
	// DataZone management APIs (ListDomains, GetDomain, ListProjects,
	// ListEnvironments, ListDataSources, GetDataSource, ListTagsForResource) and
	// never reads or persists business glossaries, glossary terms, catalog asset
	// content, subscription data, or any data-plane payload, and never mutates
	// DataZone state.
	ServiceDatazone = "datazone"
)

const (
	// ResourceTypeDatazoneDomain identifies an Amazon DataZone domain metadata
	// resource. The scanner emits identity, status, the encryption KMS key
	// reference, the domain execution / service IAM role references, and
	// lifecycle timestamps only.
	ResourceTypeDatazoneDomain = "aws_datazone_domain"
	// ResourceTypeDatazoneProject identifies an Amazon DataZone project metadata
	// resource. The scanner emits identity, parent domain, status, and category
	// only.
	ResourceTypeDatazoneProject = "aws_datazone_project"
	// ResourceTypeDatazoneEnvironment identifies an Amazon DataZone environment
	// metadata resource. The scanner emits identity, parent domain and project,
	// provider, blueprint/profile identifiers, and the target AWS account/region
	// only.
	ResourceTypeDatazoneEnvironment = "aws_datazone_environment"
	// ResourceTypeDatazoneDataSource identifies an Amazon DataZone data source
	// metadata resource. The scanner emits identity, parent domain/project/
	// environment, source type, and enablement only. Ingested asset content,
	// filter expressions, and access credentials stay outside the contract.
	ResourceTypeDatazoneDataSource = "aws_datazone_data_source"
)

const (
	// RelationshipDatazoneDomainUsesKMSKey records a DataZone domain's reported
	// KMS encryption key dependency. The target is keyed by the KMS key
	// identifier DataZone reports (key id, key ARN, or alias), matching how the
	// KMS scanner publishes its key resource_id.
	RelationshipDatazoneDomainUsesKMSKey = "datazone_domain_uses_kms_key"
	// RelationshipDatazoneDomainUsesIAMRole records a DataZone domain's reported
	// IAM execution or service role dependency. The target is keyed by the role
	// ARN, matching how the IAM scanner publishes its role resource_id.
	RelationshipDatazoneDomainUsesIAMRole = "datazone_domain_uses_iam_role"
	// RelationshipDatazoneProjectInDomain records a DataZone project's membership
	// in its parent domain. The target is keyed by the domain id the domain node
	// publishes.
	RelationshipDatazoneProjectInDomain = "datazone_project_in_domain"
	// RelationshipDatazoneEnvironmentInDomain records a DataZone environment's
	// membership in its parent domain. The target is keyed by the domain id the
	// domain node publishes.
	RelationshipDatazoneEnvironmentInDomain = "datazone_environment_in_domain"
	// RelationshipDatazoneDataSourceInDomain records a DataZone data source's
	// membership in its parent domain. The target is keyed by the domain id the
	// domain node publishes.
	RelationshipDatazoneDataSourceInDomain = "datazone_data_source_in_domain"
	// RelationshipDatazoneDataSourceBacksGlueDatabase records a DataZone Glue data
	// source's backing AWS Glue Data Catalog database. The target is keyed by the
	// Glue database name, matching how the Glue scanner publishes its database
	// resource_id.
	RelationshipDatazoneDataSourceBacksGlueDatabase = "datazone_data_source_backs_glue_database"
	// RelationshipDatazoneDataSourceBacksRedshiftCluster records a DataZone
	// Redshift data source's backing provisioned Amazon Redshift cluster. The
	// target is keyed by the partition-aware cluster ARN the Redshift scanner
	// synthesizes and publishes for a cluster node.
	RelationshipDatazoneDataSourceBacksRedshiftCluster = "datazone_data_source_backs_redshift_cluster"
)

// Analytics-category constants consolidated from constants_databrew.go (#6696).

const (
	// ServiceDatabrew identifies the regional AWS Glue DataBrew metadata-only
	// scan slice. The scanner reads dataset, recipe, job, and project
	// control-plane metadata through the DataBrew list APIs (ListDatasets,
	// ListRecipes, ListJobs, ListProjects) and reads resource tags inline from
	// those list responses, never via a separate ListTagsForResource call. It
	// never reads or persists recipe step expressions, transformation
	// parameters, custom SQL query strings, sample data, or any data-plane
	// payload, and never mutates DataBrew state.
	ServiceDatabrew = "databrew"
)

const (
	// ResourceTypeDatabrewDataset identifies an AWS Glue DataBrew dataset
	// metadata resource. The scanner emits identity, the input source kind
	// (Amazon S3, Glue Data Catalog, or database), input location references,
	// file format, and lifecycle timestamps only. Sample data and path-option
	// parameter values stay outside the contract.
	ResourceTypeDatabrewDataset = "aws_databrew_dataset"
	// ResourceTypeDatabrewRecipe identifies an AWS Glue DataBrew recipe metadata
	// resource. The scanner emits identity, version, associated project name,
	// and step count only. Recipe step expressions, transformation operations,
	// and their parameters are never read or persisted.
	ResourceTypeDatabrewRecipe = "aws_databrew_recipe"
	// ResourceTypeDatabrewJob identifies an AWS Glue DataBrew job metadata
	// resource. The scanner emits identity, job type, the processed dataset
	// name, the recipe reference, the assumed IAM role ARN, output S3 location
	// references, encryption mode, and lifecycle timestamps only. Output object
	// data and profile sample rows stay outside the contract.
	ResourceTypeDatabrewJob = "aws_databrew_job"
	// ResourceTypeDatabrewProject identifies an AWS Glue DataBrew project
	// metadata resource. The scanner emits identity, the dataset and recipe it
	// binds, the assumed IAM role ARN, and lifecycle timestamps only. Interactive
	// session sample data stays outside the contract.
	ResourceTypeDatabrewProject = "aws_databrew_project"
)

const (
	// RelationshipDatabrewDatasetReadsS3 records a DataBrew dataset's Amazon S3
	// input location. DataBrew reports a bucket NAME, so the scanner synthesizes
	// the partition-aware bucket ARN (arn:<partition>:s3:::<bucket>) to match the
	// S3 scanner's published bucket resource_id.
	RelationshipDatabrewDatasetReadsS3 = "databrew_dataset_reads_s3"
	// RelationshipDatabrewDatasetReadsGlueTable records a DataBrew dataset's Glue
	// Data Catalog input table. The target is keyed by the "<database>/<table>"
	// identity the Glue table scanner publishes as its resource_id.
	RelationshipDatabrewDatasetReadsGlueTable = "databrew_dataset_reads_glue_table"
	// RelationshipDatabrewJobWritesS3 records a DataBrew job's Amazon S3 output
	// location. DataBrew reports a bucket NAME, so the scanner synthesizes the
	// partition-aware bucket ARN to match the S3 scanner's published bucket
	// resource_id.
	RelationshipDatabrewJobWritesS3 = "databrew_job_writes_s3"
	// RelationshipDatabrewJobAssumesRole records the IAM role a DataBrew job
	// assumes. AWS reports a role ARN, which matches how the IAM scanner
	// publishes its role resource_id.
	RelationshipDatabrewJobAssumesRole = "databrew_job_assumes_role"
	// RelationshipDatabrewJobProcessesDataset records the DataBrew dataset a job
	// processes. The target is keyed by the dataset name the dataset node
	// publishes as its resource_id.
	RelationshipDatabrewJobProcessesDataset = "databrew_job_processes_dataset"
	// RelationshipDatabrewProjectUsesDataset records the DataBrew dataset a
	// project binds. The target is keyed by the dataset name the dataset node
	// publishes as its resource_id.
	RelationshipDatabrewProjectUsesDataset = "databrew_project_uses_dataset"
	// RelationshipDatabrewProjectUsesRecipe records the DataBrew recipe a project
	// develops. The target is keyed by the recipe name the recipe node publishes
	// as its resource_id.
	RelationshipDatabrewProjectUsesRecipe = "databrew_project_uses_recipe"
	// RelationshipDatabrewProjectAssumesRole records the IAM role a DataBrew
	// project assumes. AWS reports a role ARN, which matches how the IAM scanner
	// publishes its role resource_id.
	RelationshipDatabrewProjectAssumesRole = "databrew_project_assumes_role"
)

// Analytics-category constants consolidated from constants_datasync.go (#6696).

const (
	// ServiceDataSync identifies the regional AWS DataSync metadata-only scan
	// slice covering transfer tasks, transfer locations, and on-premises
	// agents.
	ServiceDataSync = "datasync"
)

const (
	// ResourceTypeDataSyncTask identifies an AWS DataSync transfer task metadata
	// resource. The task carries its source and destination location ARNs, an
	// optional CloudWatch log group ARN, a schedule expression, and status. Data
	// transferred between locations is never read.
	ResourceTypeDataSyncTask = "aws_datasync_task"
	// ResourceTypeDataSyncLocation identifies an AWS DataSync location metadata
	// resource. The location carries its type (S3, EFS, FSx, NFS, SMB, object
	// storage, HDFS, Azure Blob) and a host/path-only URI. Object contents,
	// access keys, and storage credentials are never read.
	ResourceTypeDataSyncLocation = "aws_datasync_location"
	// ResourceTypeDataSyncAgent identifies an AWS DataSync agent metadata
	// resource (the on-premises or in-cloud appliance that moves data). The
	// agent carries its status, endpoint type, and platform version.
	ResourceTypeDataSyncAgent = "aws_datasync_agent"
)

const (
	// RelationshipDataSyncTaskSourceLocation records the source location a
	// DataSync task reads from, keyed by the location ARN.
	RelationshipDataSyncTaskSourceLocation = "datasync_task_source_location"
	// RelationshipDataSyncTaskDestinationLocation records the destination
	// location a DataSync task writes to, keyed by the location ARN.
	RelationshipDataSyncTaskDestinationLocation = "datasync_task_destination_location"
	// RelationshipDataSyncTaskLogsToCloudWatch records the CloudWatch log group
	// a DataSync task writes transfer logs to, keyed by the log group ARN.
	RelationshipDataSyncTaskLogsToCloudWatch = "datasync_task_logs_to_cloudwatch"
	// RelationshipDataSyncLocationTargetsS3Bucket records the S3 bucket a
	// DataSync S3 location is backed by, keyed by the synthesized bucket ARN.
	RelationshipDataSyncLocationTargetsS3Bucket = "datasync_location_targets_s3_bucket"
	// RelationshipDataSyncLocationTargetsEFSFileSystem records the EFS file
	// system a DataSync EFS location is backed by, keyed by the synthesized file
	// system ARN.
	RelationshipDataSyncLocationTargetsEFSFileSystem = "datasync_location_targets_efs_file_system"
	// RelationshipDataSyncLocationTargetsFSxFileSystem records the FSx file
	// system a DataSync FSx location is backed by, keyed by the FSx file system
	// ARN.
	RelationshipDataSyncLocationTargetsFSxFileSystem = "datasync_location_targets_fsx_file_system"
	// RelationshipDataSyncLocationUsesIAMRole records the IAM role a DataSync
	// location uses to access its backing AWS storage, keyed by the role ARN.
	RelationshipDataSyncLocationUsesIAMRole = "datasync_location_uses_iam_role"
)
