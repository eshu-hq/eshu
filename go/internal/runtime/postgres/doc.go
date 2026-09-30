// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres provides API and MCP runtimes with separate PostgreSQL
// writer and fenced reader pools. It supports one static physical primary and
// one streaming standby, or one primary DSN assigned to both roles. Each
// business query checks a writer WAL insertion checkpoint on its own reader
// connection before it executes. Dynamic failover, Aurora, reader selection,
// transactions, and QueryRow are outside this package's current contract.
package postgres
