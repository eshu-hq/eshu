// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the observability-coverage aliases must live in package query so the APIRouter field and the cmd/api and cmd/mcp-server wiring compile unchanged.

import (
	"github.com/eshu-hq/eshu/go/internal/query/observability/coverage"
)

// observability_coverage_alias.go is the root alias shim for the
// observability-coverage family, which moved to
// internal/query/observability/coverage (#6642). cmd/api and cmd/mcp-server
// build the handler and its store through package query, and the APIRouter
// field names the handler, so these stay until the #6642 alias sweep.

// ObservabilityCoverageHandler serves GET
// /api/v0/observability/coverage/correlations. See coverage.Handler.
type ObservabilityCoverageHandler = coverage.Handler

// NewPostgresObservabilityCoverageCorrelationStore constructs the
// Postgres-backed correlation store. It forwards unchanged to
// coverage.NewPostgresCorrelationStore.
func NewPostgresObservabilityCoverageCorrelationStore(db coverage.CorrelationQueryer) coverage.PostgresCorrelationStore {
	return coverage.NewPostgresCorrelationStore(db)
}
