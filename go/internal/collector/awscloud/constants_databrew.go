// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
