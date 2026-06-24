// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Athena client into the
// metadata-only Athena scanner interface.
//
// The adapter calls ListWorkGroups, GetWorkGroup, ListDataCatalogs,
// GetDataCatalog, ListPreparedStatements, ListNamedQueries,
// BatchGetNamedQuery, and ListTagsForResource. It intentionally excludes
// StartQueryExecution, StopQueryExecution, GetQueryExecution, GetQueryResults,
// ListQueryExecutions, GetNamedQuery, CreateNamedQuery, DeleteNamedQuery,
// UpdateNamedQuery, CreatePreparedStatement, UpdatePreparedStatement,
// DeletePreparedStatement, and GetPreparedStatement so query result rows,
// named-query SQL bodies, prepared-statement query strings, and query history
// strings can never enter the scanner contract.
package awssdk
