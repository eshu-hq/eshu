// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cicd serves the CI/CD run correlation reads:
// GET /api/v0/ci-cd/run-correlations and its cheap-summary aggregates
// GET /api/v0/ci-cd/run-correlations/count and
// GET /api/v0/ci-cd/run-correlations/inventory.
//
// Handler requires a scope anchor (scope_id, repository_id, commit_sha,
// provider_run_id, artifact_digest, image_ref, or environment) and a limit
// (1-200); a missing one is a 400. Reads come from the Postgres reducer read
// model, never the graph: PostgresRunCorrelationStore pages active
// reducer_ci_cd_run_correlation facts with a deterministic cursor, and
// PostgresRunCorrelationAggregateStore answers the count and grouped
// inventory rollups. A nil store answers 503. A scoped caller with empty
// grants gets the empty page, never a store read; a selector outside the
// grant is a 404 that leaks nothing. A profile without the capability
// answers 501.
//
// A failed store read never echoes the backend error: a stale or timed-out
// reader answers the retryable 503, a client cancel 499, and anything else a
// fixed per-route 500 with the error recorded on the handler span (#7674).
//
// Capability, AggregateCapability and Support declare the family's capability
// rows once: internal/query/contract registers Support() for production and
// main_test.go registers it for this package's own tests.
//
// The package moved out of the root query package for #6642. Root keeps
// CICDHandler and the store aliases in cicd_alias.go for cmd/api,
// cmd/mcp-server, and the staying read-model route tests until the #6642
// alias sweep.
package cicd
