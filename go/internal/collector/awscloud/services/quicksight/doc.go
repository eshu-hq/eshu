// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package quicksight maps Amazon QuickSight data source, dataset, dashboard, and
// analysis metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for QuickSight data sources,
// datasets, dashboards, and analyses plus relationships for data-source-to-
// backing-store (Redshift cluster, RDS instance, Athena workgroup, S3 bucket),
// data-source-to-VPC-connection (security group, subnet), dataset-to-data-source,
// and dashboard/analysis-to-dataset evidence. Data-source credentials, connection
// passwords, secret connection parameters, the Secrets Manager secret value, SQL
// query bodies, and visual definitions stay outside this package contract: the
// scanner is metadata-only. A QuickSight subscription is account-scoped, so a
// not-subscribed account yields an empty result rather than a failed scan.
package quicksight
