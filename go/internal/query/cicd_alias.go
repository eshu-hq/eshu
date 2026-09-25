// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the CI/CD run-correlation aliases must live in package query so the APIRouter field, the cmd/api and cmd/mcp-server wiring, and the staying read-model route tests compile unchanged.

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/cicd"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// cicd_alias.go is the root alias shim for the CI/CD run correlation family,
// which moved to internal/query/cicd (#6642). cmd/api and cmd/mcp-server
// build the handler and its Postgres stores through package query, and the
// APIRouter field names the handler, so this stays until the #6642 alias
// sweep.

// CICDHandler serves GET /api/v0/ci-cd/run-correlations and its count and
// inventory aggregates. See cicd.Handler.
type CICDHandler = cicd.Handler

// CICDRunCorrelationResult is one reducer-owned CI/CD run correlation row.
// See querycontract.CICDRunCorrelationResult.
type CICDRunCorrelationResult = querycontract.CICDRunCorrelationResult

// CICDRunCorrelationStore reads reducer-owned CI/CD run correlations.
// See querycontract.CICDRunCorrelationStore.
type CICDRunCorrelationStore = querycontract.CICDRunCorrelationStore

// CICDRunCorrelationFilter bounds run-correlation reads. See
// querycontract.CICDRunCorrelationFilter.
type CICDRunCorrelationFilter = querycontract.CICDRunCorrelationFilter

// CICDRunCorrelationRow is one durable CI/CD correlation fact. See
// querycontract.CICDRunCorrelationRow.
type CICDRunCorrelationRow = querycontract.CICDRunCorrelationRow

// PostgresCICDRunCorrelationStore reads active CI/CD run correlation facts
// from Postgres. See cicd.PostgresRunCorrelationStore.
type PostgresCICDRunCorrelationStore = cicd.PostgresRunCorrelationStore

// NewPostgresCICDRunCorrelationStore creates the Postgres-backed CI/CD run
// correlation read model. See cicd.NewPostgresRunCorrelationStore.
func NewPostgresCICDRunCorrelationStore(db *sql.DB) PostgresCICDRunCorrelationStore {
	return cicd.NewPostgresRunCorrelationStore(db)
}

// CICDRunCorrelationAggregateStore reads cheap-summary aggregates over
// reducer-owned CI/CD run correlations. See
// cicd.RunCorrelationAggregateStore.
type CICDRunCorrelationAggregateStore = cicd.RunCorrelationAggregateStore

// CICDRunCorrelationAggregateFilter narrows aggregate reads. See
// cicd.RunCorrelationAggregateFilter.
type CICDRunCorrelationAggregateFilter = cicd.RunCorrelationAggregateFilter

// CICDRunCorrelationAggregateCount is the cheap-summary totals envelope.
// See cicd.RunCorrelationAggregateCount.
type CICDRunCorrelationAggregateCount = cicd.RunCorrelationAggregateCount

// CICDRunCorrelationInventoryRow is one grouped aggregate bucket. See
// cicd.RunCorrelationInventoryRow.
type CICDRunCorrelationInventoryRow = cicd.RunCorrelationInventoryRow

// CICDRunCorrelationInventoryDimension names the inventory grouping
// dimension. See cicd.RunCorrelationInventoryDimension.
type CICDRunCorrelationInventoryDimension = cicd.RunCorrelationInventoryDimension

// PostgresCICDRunCorrelationAggregateStore reads aggregate counts directly
// from reducer-owned CI/CD run correlation facts. See
// cicd.PostgresRunCorrelationAggregateStore.
type PostgresCICDRunCorrelationAggregateStore = cicd.PostgresRunCorrelationAggregateStore

// NewPostgresCICDRunCorrelationAggregateStore creates the Postgres-backed
// aggregate store. See cicd.NewPostgresRunCorrelationAggregateStore.
func NewPostgresCICDRunCorrelationAggregateStore(db *sql.DB) PostgresCICDRunCorrelationAggregateStore {
	return cicd.NewPostgresRunCorrelationAggregateStore(db)
}
