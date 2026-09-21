// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Amazon Keyspaces (for Apache
// Cassandra) control-plane calls into the metadata-only Snapshot the keyspaces
// scanner consumes. It owns pagination, throttle accounting, and API-call
// telemetry. It reads keyspace and table metadata only: it never executes CQL,
// never runs ExecuteStatement, BatchStatement, or Select, never reads table rows
// or cells, never restores tables, and never mutates keyspaces or tables.
package awssdk
