// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sandbox provides a Tier 2 read-only query sandbox for executing
// Cypher and SQL queries with strict authorization controls and resource
// constraints.
//
// # Security Posture
//
// The sandbox is STRICTLY READ-ONLY and operates under a DEFAULT-DENY model:
// any uncertainty in authorization results in rejection. The sandbox is
// DEFAULT-OFF and refuses to execute queries unless explicitly enabled. This
// default-off design requires a dedicated security review per issues #1755,
// #1900, and #1902 before enabling in production.
//
// # Authorization
//
// Authorization decisions are made via Decision, which provides a bounded,
// low-cardinality reason when access is denied. Reasons NEVER echo the query
// string or reveal secrets, preventing information leakage through the
// authorization response. Empty Reason strings are used only when a query is
// allowed.
//
// # Resource Constraints
//
// Query execution is bounded by Caps: maximum row count, result byte size,
// execution time, query string length, maximum planner cost estimate
// (MaxPlanCost), and maximum planner row estimate (MaxEstimatedRows). These
// constraints prevent resource exhaustion and long-running queries from
// impacting the graph backend.
//
// # Cost Gate
//
// CostGateExecutor wraps an inner Executor and enforces a pre-execution cost
// check for SQL queries via EXPLAIN (FORMAT JSON). It is Layer 3.5 between
// Validate (Layer 2) and actual execution (Layer 3). An over-budget plan or a
// plan containing a forbidden operator is rejected with ErrPlanBudgetExceeded
// before the query reaches the backend. The cost gate is active only when
// Caps.MaxPlanCost or Caps.MaxEstimatedRows is non-zero, or
// CostGateConfig.ForbiddenPlanOperators is non-empty. Construct via
// NewCostGateExecutor; inject a SQLExplainer for EXPLAIN (FORMAT JSON) access.
//
// # Dialects
//
// The sandbox supports two query dialects: Cypher (for property graph queries
// against NornicDB) and SQL (for schema queries against the relational fact
// store). Each dialect has its own authorization and execution policy.
package sandbox
