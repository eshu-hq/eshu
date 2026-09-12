// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package alerts implements the security-alert reconciliation Postgres reads
// behind the supply-chain hub (#6642, split off supplychain #6060 lane A):
// PostgresStore (the list read and the provider-repository-scope lookup) and
// PostgresAggregateStore (the cheap-summary count and the grouped inventory
// read).
//
// Both stores read the reducer-owned reducer_security_alert_reconciliation
// fact kind. Every list route rejects an anchorless read before the store
// runs: ListSecurityAlertReconciliations requires
// supplychain.SecurityAlertReconciliationFilter.HasScope, and the package's
// unexported SQL text applies the requested repository scope and the
// scoped-token grant set inside the ranking CTE, before pagination -- a
// scoped caller never observes or paginates a reconciliation row outside its
// granted repositories. In-package tests (queries_test.go, aggregates_test.go)
// pin the exact SQL text and grant-predicate ordering.
//
// This package imports supplychain (the port interfaces, filter/row/limit
// types the handlers and stores share) and querycontract (payload row-value
// decoders); it MUST NOT import the query root or supplychain would cycle
// back through root's compatibility aliases in supply_chain_hub_alias.go,
// which import this package for the PostgresStore /
// PostgresAggregateStore type aliases and constructors cmd/api and
// cmd/mcp-server still use. See README.md for the file layout and move
// evidence, and AGENTS.md for the per-symbol export rationale.
package alerts
