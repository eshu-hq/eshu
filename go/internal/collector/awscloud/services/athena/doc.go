// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package athena maps Amazon Athena control-plane metadata into AWS cloud
// collector facts.
//
// The scanner emits reported-confidence workgroup, data catalog, prepared
// statement, and named-query resource facts plus workgroup-to-S3-result-bucket,
// workgroup-to-KMS-key, prepared-statement-to-workgroup, and
// named-query-to-workgroup relationships. Query result rows, query execution
// result location object contents, named-query SQL bodies, prepared-statement
// query strings, query history strings, StartQueryExecution, StopQueryExecution,
// CreateNamedQuery, DeleteNamedQuery, UpdateNamedQuery, CreatePreparedStatement,
// and every other Athena mutation API stay outside this package contract.
package athena
