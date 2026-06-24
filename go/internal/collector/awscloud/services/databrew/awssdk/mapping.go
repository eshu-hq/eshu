// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatabrewtypes "github.com/aws/aws-sdk-go-v2/service/databrew/types"

	databrewservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/databrew"
)

// mapDataset maps an SDK DataBrew dataset into the scanner-owned model. It reads
// only the input source references (S3 bucket, Glue catalog table, or database
// connection name) and lifecycle metadata. It never reads dataset-parameter
// values, path-option expressions, or any custom SQL query string.
func mapDataset(dataset awsdatabrewtypes.Dataset) databrewservice.Dataset {
	mapped := databrewservice.Dataset{
		Name:             strings.TrimSpace(aws.ToString(dataset.Name)),
		ARN:              strings.TrimSpace(aws.ToString(dataset.ResourceArn)),
		SourceKind:       strings.TrimSpace(string(dataset.Source)),
		Format:           strings.TrimSpace(string(dataset.Format)),
		CreateDate:       aws.ToTime(dataset.CreateDate),
		LastModifiedDate: aws.ToTime(dataset.LastModifiedDate),
		Tags:             trimTags(dataset.Tags),
	}
	applyDatasetInput(&mapped, dataset.Input)
	return mapped
}

// applyDatasetInput copies only the input location references from the dataset
// input. The custom SQL query string a database input can carry is intentionally
// never read.
func applyDatasetInput(dataset *databrewservice.Dataset, input *awsdatabrewtypes.Input) {
	if input == nil {
		return
	}
	if s3 := input.S3InputDefinition; s3 != nil {
		dataset.S3Bucket = strings.TrimSpace(aws.ToString(s3.Bucket))
		dataset.S3Key = strings.TrimSpace(aws.ToString(s3.Key))
		if dataset.SourceKind == "" {
			dataset.SourceKind = string(awsdatabrewtypes.SourceS3)
		}
	}
	if catalog := input.DataCatalogInputDefinition; catalog != nil {
		dataset.GlueDatabaseName = strings.TrimSpace(aws.ToString(catalog.DatabaseName))
		dataset.GlueTableName = strings.TrimSpace(aws.ToString(catalog.TableName))
		dataset.GlueCatalogID = strings.TrimSpace(aws.ToString(catalog.CatalogId))
		if dataset.SourceKind == "" {
			dataset.SourceKind = string(awsdatabrewtypes.SourceDatacatalog)
		}
	}
	if database := input.DatabaseInputDefinition; database != nil {
		dataset.DatabaseConnectionName = strings.TrimSpace(aws.ToString(database.GlueConnectionName))
		if dataset.SourceKind == "" {
			dataset.SourceKind = string(awsdatabrewtypes.SourceDatabase)
		}
	}
}

// mapRecipe maps an SDK DataBrew recipe into the scanner-owned model. It records
// only identity, version, project association, and the step count; the recipe
// step expressions and their transformation parameters are never persisted.
func mapRecipe(recipe awsdatabrewtypes.Recipe) databrewservice.Recipe {
	return databrewservice.Recipe{
		Name:             strings.TrimSpace(aws.ToString(recipe.Name)),
		ARN:              strings.TrimSpace(aws.ToString(recipe.ResourceArn)),
		Version:          strings.TrimSpace(aws.ToString(recipe.RecipeVersion)),
		ProjectName:      strings.TrimSpace(aws.ToString(recipe.ProjectName)),
		StepCount:        len(recipe.Steps),
		CreateDate:       aws.ToTime(recipe.CreateDate),
		LastModifiedDate: aws.ToTime(recipe.LastModifiedDate),
		PublishedDate:    aws.ToTime(recipe.PublishedDate),
		Tags:             trimTags(recipe.Tags),
	}
}

// mapJob maps an SDK DataBrew job into the scanner-owned model. It records
// identity, the processed dataset and recipe references, the assumed IAM role
// ARN, the encryption mode, and the distinct S3 output bucket names. Output
// object data and profile sample rows are never read.
func mapJob(job awsdatabrewtypes.Job) databrewservice.Job {
	mapped := databrewservice.Job{
		Name:             strings.TrimSpace(aws.ToString(job.Name)),
		ARN:              strings.TrimSpace(aws.ToString(job.ResourceArn)),
		Type:             strings.TrimSpace(string(job.Type)),
		DatasetName:      strings.TrimSpace(aws.ToString(job.DatasetName)),
		ProjectName:      strings.TrimSpace(aws.ToString(job.ProjectName)),
		RoleARN:          strings.TrimSpace(aws.ToString(job.RoleArn)),
		EncryptionMode:   strings.TrimSpace(string(job.EncryptionMode)),
		OutputS3Buckets:  jobOutputBuckets(job.Outputs),
		CreateDate:       aws.ToTime(job.CreateDate),
		LastModifiedDate: aws.ToTime(job.LastModifiedDate),
		Tags:             trimTags(job.Tags),
	}
	if ref := job.RecipeReference; ref != nil {
		mapped.RecipeName = strings.TrimSpace(aws.ToString(ref.Name))
	}
	return mapped
}

// jobOutputBuckets returns the distinct, de-duplicated, order-stable set of S3
// output bucket names a job writes to. It reads only the bucket name of each
// output location, never the output object data.
func jobOutputBuckets(outputs []awsdatabrewtypes.Output) []string {
	if len(outputs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(outputs))
	var buckets []string
	for _, output := range outputs {
		if output.Location == nil {
			continue
		}
		bucket := strings.TrimSpace(aws.ToString(output.Location.Bucket))
		if bucket == "" {
			continue
		}
		if _, ok := seen[bucket]; ok {
			continue
		}
		seen[bucket] = struct{}{}
		buckets = append(buckets, bucket)
	}
	return buckets
}

// mapProject maps an SDK DataBrew project into the scanner-owned model. It
// records identity, the dataset and recipe it binds, the assumed IAM role ARN,
// and lifecycle metadata. Interactive session sample data is never read.
func mapProject(project awsdatabrewtypes.Project) databrewservice.Project {
	return databrewservice.Project{
		Name:             strings.TrimSpace(aws.ToString(project.Name)),
		ARN:              strings.TrimSpace(aws.ToString(project.ResourceArn)),
		DatasetName:      strings.TrimSpace(aws.ToString(project.DatasetName)),
		RecipeName:       strings.TrimSpace(aws.ToString(project.RecipeName)),
		RoleARN:          strings.TrimSpace(aws.ToString(project.RoleArn)),
		CreateDate:       aws.ToTime(project.CreateDate),
		LastModifiedDate: aws.ToTime(project.LastModifiedDate),
		Tags:             trimTags(project.Tags),
	}
}

// trimTags returns a trimmed-key copy of the resource tag map, dropping
// empty-keyed entries, or nil when nothing survives.
func trimTags(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
