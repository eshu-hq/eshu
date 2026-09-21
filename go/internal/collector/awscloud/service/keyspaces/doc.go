// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package keyspaces converts Amazon Keyspaces (for Apache Cassandra)
// control-plane metadata into AWS cloud collector facts.
//
// The package owns scanner-level keyspace and table fact selection, the
// table-in-keyspace edge, and direct customer-managed KMS relationship evidence.
// Schema column names and types are structural metadata and are the only schema
// information emitted; table row data, cell values, and CQL query results are
// never read or persisted. The package does not own AWS SDK pagination,
// credential loading, workflow claims, fact persistence, graph writes, reducer
// admission, workload ownership, or query behavior.
package keyspaces
