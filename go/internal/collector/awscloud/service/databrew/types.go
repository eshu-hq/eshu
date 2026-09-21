// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package databrew

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Glue DataBrew dataset, recipe, job, and
// project observations for one AWS claim. Implementations read control-plane
// metadata through the DataBrew list APIs and never read or persist recipe step
// expressions, transformation parameters, custom SQL query strings, sample
// data, or any data-plane payload.
type Client interface {
	// Snapshot returns every DataBrew dataset, recipe, job, and project visible
	// to the configured AWS credentials in one region.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures DataBrew control-plane metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// Datasets is the metadata-only set of DataBrew datasets.
	Datasets []Dataset
	// Recipes is the metadata-only set of DataBrew recipes. Recipe steps are
	// intentionally reduced to a count; their expressions are never carried.
	Recipes []Recipe
	// Jobs is the metadata-only set of DataBrew jobs.
	Jobs []Job
	// Projects is the metadata-only set of DataBrew projects.
	Projects []Project
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Dataset is the scanner-owned DataBrew dataset model. It carries control-plane
// metadata only and intentionally excludes sample data and dataset-parameter
// values.
type Dataset struct {
	// Name is the unique DataBrew dataset name.
	Name string
	// ARN is the Amazon Resource Name that uniquely identifies the dataset.
	ARN string
	// SourceKind is the input source category AWS reports for the dataset (for
	// example S3, DATA-CATALOG, or DATABASE).
	SourceKind string
	// Format is the file format of an S3-sourced dataset, when reported.
	Format string
	// S3Bucket is the Amazon S3 bucket NAME the dataset reads from, when the
	// input is an S3 location. It is a bucket name, not an ARN.
	S3Bucket string
	// S3Key is the optional S3 object key or prefix the dataset reads from.
	S3Key string
	// GlueDatabaseName is the Glue Data Catalog database name, when the input is
	// a Data Catalog table.
	GlueDatabaseName string
	// GlueTableName is the Glue Data Catalog table name, when the input is a
	// Data Catalog table.
	GlueTableName string
	// GlueCatalogID is the Data Catalog account identifier, when reported.
	GlueCatalogID string
	// DatabaseConnectionName is the Glue connection name backing a database
	// (JDBC/Redshift) input, when the input is a database. The custom SQL query
	// string for such inputs is never read or persisted.
	DatabaseConnectionName string
	// CreateDate is when the dataset was created.
	CreateDate time.Time
	// LastModifiedDate is when the dataset was last modified.
	LastModifiedDate time.Time
	// Tags carries the dataset resource tags.
	Tags map[string]string
}

// Recipe is the scanner-owned DataBrew recipe model. It carries control-plane
// metadata only. Recipe step expressions, transformation operations, and their
// parameters are never read or persisted; only the step count is recorded.
type Recipe struct {
	// Name is the unique DataBrew recipe name.
	Name string
	// ARN is the Amazon Resource Name that uniquely identifies the recipe.
	ARN string
	// Version is the recipe version identifier (for example 1.0 or
	// LATEST_PUBLISHED).
	Version string
	// ProjectName is the project the recipe is associated with, when reported.
	ProjectName string
	// StepCount is the number of steps the recipe defines. The step expressions
	// themselves are intentionally excluded from the scanner contract.
	StepCount int
	// CreateDate is when the recipe was created.
	CreateDate time.Time
	// LastModifiedDate is when the recipe was last modified.
	LastModifiedDate time.Time
	// PublishedDate is when the recipe version was published, when reported.
	PublishedDate time.Time
	// Tags carries the recipe resource tags.
	Tags map[string]string
}

// Job is the scanner-owned DataBrew job model. It carries control-plane
// metadata only and intentionally excludes job output object data and profile
// sample rows.
type Job struct {
	// Name is the unique DataBrew job name.
	Name string
	// ARN is the Amazon Resource Name that uniquely identifies the job.
	ARN string
	// Type is the job type AWS reports (for example PROFILE or RECIPE).
	Type string
	// DatasetName is the DataBrew dataset the job processes, when reported.
	DatasetName string
	// ProjectName is the project the job is associated with, when reported.
	ProjectName string
	// RecipeName is the recipe the job runs, when reported.
	RecipeName string
	// RoleARN is the IAM role ARN the job assumes.
	RoleARN string
	// EncryptionMode is the job output encryption mode (for example SSE-KMS or
	// SSE-S3), when reported.
	EncryptionMode string
	// OutputS3Buckets is the set of distinct Amazon S3 output bucket NAMES the
	// job writes to. They are bucket names, not ARNs.
	OutputS3Buckets []string
	// CreateDate is when the job was created.
	CreateDate time.Time
	// LastModifiedDate is when the job was last modified.
	LastModifiedDate time.Time
	// Tags carries the job resource tags.
	Tags map[string]string
}

// Project is the scanner-owned DataBrew project model. It carries control-plane
// metadata only and intentionally excludes interactive session sample data.
type Project struct {
	// Name is the unique DataBrew project name.
	Name string
	// ARN is the Amazon Resource Name that uniquely identifies the project.
	ARN string
	// DatasetName is the DataBrew dataset the project acts upon, when reported.
	DatasetName string
	// RecipeName is the DataBrew recipe the project develops.
	RecipeName string
	// RoleARN is the IAM role ARN the project assumes.
	RoleARN string
	// CreateDate is when the project was created.
	CreateDate time.Time
	// LastModifiedDate is when the project was last modified.
	LastModifiedDate time.Time
	// Tags carries the project resource tags.
	Tags map[string]string
}
