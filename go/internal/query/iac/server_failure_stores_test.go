// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// failingManagementStore fails the management count, list, or selector read
// with the configured error and answers the rest from rows.
type failingManagementStore struct {
	countErr    error
	listErr     error
	selectorErr error
	rows        []ManagementFindingRow
}

func (s failingManagementStore) CountUnmanagedCloudResources(context.Context, ManagementFilter) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return len(s.rows), nil
}

func (s failingManagementStore) ListUnmanagedCloudResources(context.Context, ManagementFilter) ([]ManagementFindingRow, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.rows, nil
}

func (s failingManagementStore) ListReplatformingSelectors(context.Context, int, []string) (ReplatformingSelectorPage, error) {
	return ReplatformingSelectorPage{}, s.selectorErr
}

// failingInventoryStore fails the inventory search or facet summary with the
// configured error and answers the search from candidates otherwise.
type failingInventoryStore struct {
	searchErr  error
	summaryErr error
	candidates []InventoryCandidate
}

func (s failingInventoryStore) SearchActive(context.Context, InventorySearch, querycontract.RepositoryAccessFilter) ([]InventoryCandidate, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.candidates, nil
}

func (s failingInventoryStore) Summary(context.Context, querycontract.RepositoryAccessFilter, int) (InventorySummary, error) {
	return InventorySummary{}, s.summaryErr
}
