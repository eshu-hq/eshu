// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceGlue identifies the regional AWS Glue metadata-only scan slice
	// covering Data Catalog, crawlers, jobs, triggers, workflows, and
	// connections.
	ServiceGlue = "glue"
)

const (
	// ResourceTypeGlueDatabase identifies an AWS Glue Data Catalog database
	// metadata resource.
	ResourceTypeGlueDatabase = "aws_glue_database"
	// ResourceTypeGlueTable identifies an AWS Glue Data Catalog table metadata
	// resource. Column statistics with sample values stay outside the contract.
	ResourceTypeGlueTable = "aws_glue_table"
	// ResourceTypeGlueCrawler identifies an AWS Glue crawler metadata resource.
	ResourceTypeGlueCrawler = "aws_glue_crawler"
	// ResourceTypeGlueJob identifies an AWS Glue job metadata resource. Job
	// script bodies and default-argument secret-shaped values stay outside the
	// contract.
	ResourceTypeGlueJob = "aws_glue_job"
	// ResourceTypeGlueTrigger identifies an AWS Glue trigger metadata resource.
	ResourceTypeGlueTrigger = "aws_glue_trigger"
	// ResourceTypeGlueWorkflow identifies an AWS Glue workflow metadata
	// resource.
	ResourceTypeGlueWorkflow = "aws_glue_workflow"
	// ResourceTypeGlueConnection identifies an AWS Glue connection metadata
	// resource. Connection properties such as passwords and credential-bearing
	// JDBC URLs stay outside the contract.
	ResourceTypeGlueConnection = "aws_glue_connection"
)

const (
	// RelationshipGlueTableInDatabase records a Glue Data Catalog table's
	// membership in a database.
	RelationshipGlueTableInDatabase = "glue_table_in_database"
	// RelationshipGlueTableStoredAtS3Location records a Glue table's reported
	// S3 storage location when the location parses as `s3://bucket/key`.
	RelationshipGlueTableStoredAtS3Location = "glue_table_stored_at_s3_location"
	// RelationshipGlueCrawlerTargetsDatabase records a Glue crawler's reported
	// target database when AWS reports the database name.
	RelationshipGlueCrawlerTargetsDatabase = "glue_crawler_targets_database"
	// RelationshipGlueCrawlerUsesIAMRole records a Glue crawler's reported IAM
	// role dependency.
	RelationshipGlueCrawlerUsesIAMRole = "glue_crawler_uses_iam_role"
	// RelationshipGlueJobUsesIAMRole records a Glue job's reported IAM role
	// dependency.
	RelationshipGlueJobUsesIAMRole = "glue_job_uses_iam_role"
	// RelationshipGlueTriggerInvokesJob records a Glue trigger's reported
	// action targeting a job by name.
	RelationshipGlueTriggerInvokesJob = "glue_trigger_invokes_job"
)
