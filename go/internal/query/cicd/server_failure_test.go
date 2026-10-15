// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestCICDRoutesAnswerFixedServerFailures is the #7674 regression for every
// CI/CD read. Each answered 500 with the store's error text, skipped the
// reader-fence 503, and answered a client cancel as a server fault. Not
// parallel: it swaps cicdHandlerTracer.
func TestCICDRoutesAnswerFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{HandlerTracer: &cicdHandlerTracer, Routes: []failure.Route{
		{
			Name: "run correlations list", Path: "/api/v0/ci-cd/run-correlations?scope_id=scope-1&limit=10",
			Message: runCorrelationsListFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &Handler{Correlations: failingRunCorrelationStore{err: err}}
			},
		},
		{
			Name: "run correlations count", Path: "/api/v0/ci-cd/run-correlations/count?scope_id=scope-1",
			Message: runCorrelationsCountFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Aggregates: failingAggregateStore{err: err}} },
		},
		{
			Name: "run correlations inventory", Path: "/api/v0/ci-cd/run-correlations/inventory?scope_id=scope-1&limit=10",
			Message: runCorrelationsInventoryFailedMessage,
			Handler: func(err error) failure.Mounter { return &Handler{Aggregates: failingAggregateStore{err: err}} },
		},
	}})
}

// failingRunCorrelationStore fails the run-correlation list read with err.
type failingRunCorrelationStore struct{ err error }

func (s failingRunCorrelationStore) ListCICDRunCorrelations(
	context.Context, querycontract.CICDRunCorrelationFilter,
) ([]querycontract.CICDRunCorrelationRow, error) {
	return nil, s.err
}

// failingAggregateStore fails both aggregate reads with err.
type failingAggregateStore struct{ err error }

func (s failingAggregateStore) CountRunCorrelations(
	context.Context, RunCorrelationAggregateFilter,
) (RunCorrelationAggregateCount, error) {
	return RunCorrelationAggregateCount{}, s.err
}

func (s failingAggregateStore) RunCorrelationInventory(
	context.Context, RunCorrelationAggregateFilter, RunCorrelationInventoryDimension, int, int,
) ([]RunCorrelationInventoryRow, error) {
	return nil, s.err
}
