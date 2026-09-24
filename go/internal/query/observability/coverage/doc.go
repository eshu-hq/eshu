// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package coverage serves GET /api/v0/observability/coverage/correlations,
// the bounded read of reducer-owned observability coverage correlations.
//
// Handler answers whether a monitored cloud resource or service has alarm,
// dashboard, log, or trace coverage, and which gaps remain. It reads active
// reducer_observability_coverage_correlation facts from Postgres through an
// ObservabilityCorrelationStore; PostgresCorrelationStore is the production
// implementation. A request must name at least one anchor (scope_id,
// provider, coverage_signal, observability_object_ref, target_uid or
// target_service_ref) and a limit of 1-200, or it is a 400. A scoped caller's
// grant binds fact.scope_id, and an empty grant returns an empty page without
// a store read. A profile without the capability answers 501, and a handler
// with no store answers 503; neither returns an empty page.
//
// Capability and Support declare the route's capability row once:
// internal/query/contract registers Support() for production and main_test.go
// registers it for this package's own tests.
//
// The package moved out of the root query package for #6642. Root keeps
// ObservabilityCoverageHandler and NewPostgresObservabilityCoverageCorrelationStore
// in observability_coverage_alias.go for cmd/api and cmd/mcp-server until the #6642
// alias sweep.
package coverage
