// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 CodeBuild calls into scanner-owned
// metadata.
//
// The adapter only calls metadata read operations: ListProjects,
// BatchGetProjects, ListReportGroups, BatchGetReportGroups, ListBuilds, and
// BatchGetBuilds. It must never call any mutation API (CreateProject,
// UpdateProject, DeleteProject, StartBuild, StopBuild, RetryBuild,
// BatchDeleteBuilds, report-group/webhook mutation), any source-credential API
// (ImportSourceCredentials, DeleteSourceCredentials, ListSourceCredentials), or
// any log-content/coverage/test-case reader. Buildspec bodies are dropped during
// mapping, build logs are never read, and PLAINTEXT environment-variable values
// pass through the redaction library before they reach scanner types so
// secret-shaped values are never persisted raw.
package awssdk
