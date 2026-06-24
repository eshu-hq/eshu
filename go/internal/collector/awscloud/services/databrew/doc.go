// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package databrew maps AWS Glue DataBrew dataset, recipe, job, and project
// metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for DataBrew datasets,
// recipes, jobs, and projects plus relationships for dataset-to-S3,
// dataset-to-Glue-table, job-to-S3, job-to-IAM-role, job-to-dataset,
// project-to-dataset, project-to-recipe, and project-to-IAM-role evidence.
//
// The package is metadata-only. It never reads or persists recipe step
// expressions, transformation operations or their parameters, custom SQL query
// strings, sample data, or any data-plane payload, and never calls a mutation
// API. A dataset whose input is a Redshift/JDBC database carries no edge to a
// Redshift cluster: DataBrew reports only a Glue connection name and table name
// for such inputs, never a Redshift cluster ARN or identifier, so an edge would
// dangle and is intentionally skipped.
package databrew
