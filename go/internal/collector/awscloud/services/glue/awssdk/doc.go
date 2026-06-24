// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Glue client into the
// metadata-only Glue scanner interface.
//
// The adapter uses GetDatabases, GetTables, GetCrawlers, GetJobs, GetTriggers,
// ListWorkflows, GetWorkflow, and GetConnections with HidePassword set so the
// AWS API never returns connection passwords. It intentionally excludes
// StartCrawler, StartJobRun, BatchStopJobRun, CreateJob, UpdateJob, DeleteJob,
// CreateDatabase, DeleteDatabase, GetUserDefinedFunctions, classifier custom
// pattern reads, column statistics with sample values, and any other mutation
// or sensitive-payload API.
package awssdk
