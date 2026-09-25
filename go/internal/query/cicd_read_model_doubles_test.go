// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// cicd_read_model_doubles_test.go holds the root copies of the CI/CD
// read-model doubles. The family moved to internal/query/cicd (#6642) with
// its own copies for the handler tests that moved along; the tests that stay
// here — the repository-story and artifact readback, the selector read-model
// routes, the collector-readiness handler, and the scoped-auth proofs — still
// drive read models through the root aliases, so they need doubles in this
// package. The two copies MUST stay behavior-identical; the leaf's live in
// cicd/run_correlations_test.go and cicd/run_correlation_aggregates_test.go.

// recordingCICDRunCorrelationStore answers ListCICDRunCorrelations from a
// fixed row slice and captures the filter the handler built.
type recordingCICDRunCorrelationStore struct {
	rows       []CICDRunCorrelationRow
	lastFilter CICDRunCorrelationFilter
}

func (s *recordingCICDRunCorrelationStore) ListCICDRunCorrelations(
	_ context.Context,
	filter CICDRunCorrelationFilter,
) ([]CICDRunCorrelationRow, error) {
	s.lastFilter = filter
	// Filter by RepositoryID when set (simulating the real Postgres store's
	// WHERE payload->>'repository_id' = $3 predicate). When the filter does
	// not constrain RepositoryID, return all rows — matching the pre-existing
	// behavior every existing test caller relies on.
	if filter.RepositoryID != "" {
		out := make([]CICDRunCorrelationRow, 0, len(s.rows))
		for _, row := range s.rows {
			if row.RepositoryID == filter.RepositoryID {
				out = append(out, row)
			}
		}
		return out, nil
	}
	return append([]CICDRunCorrelationRow(nil), s.rows...), nil
}

// stubCICDRunCorrelationAggregateStore answers the aggregate reads from
// canned envelopes and captures the filter, dimension, and page the handler
// built.
type stubCICDRunCorrelationAggregateStore struct {
	count         CICDRunCorrelationAggregateCount
	countErr      error
	inventory     []CICDRunCorrelationInventoryRow
	inventoryErr  error
	lastFilter    CICDRunCorrelationAggregateFilter
	lastDimension CICDRunCorrelationInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubCICDRunCorrelationAggregateStore) CountRunCorrelations(
	_ context.Context,
	filter CICDRunCorrelationAggregateFilter,
) (CICDRunCorrelationAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return CICDRunCorrelationAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubCICDRunCorrelationAggregateStore) RunCorrelationInventory(
	_ context.Context,
	filter CICDRunCorrelationAggregateFilter,
	dim CICDRunCorrelationInventoryDimension,
	limit int,
	offset int,
) ([]CICDRunCorrelationInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]CICDRunCorrelationInventoryRow(nil), s.inventory...), nil
}
