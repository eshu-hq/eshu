// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package glue maps AWS Glue Data Catalog, crawler, job, trigger, workflow,
// and connection metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for databases, tables,
// crawlers, jobs, triggers, workflows, and connections plus relationships for
// table-in-database, table-to-S3-location, crawler-to-database,
// crawler-to-IAM-role, job-to-IAM-role, and trigger-to-job evidence. Job
// script bodies, default-argument values, connection properties (including
// passwords and JDBC credential URLs), table column statistics with sample
// values, classifier custom patterns, and mutation APIs stay outside this
// package contract.
package glue
