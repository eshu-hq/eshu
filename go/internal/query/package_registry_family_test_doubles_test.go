// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/packagereg"
)

// This file carries this package's own copies of three PackageRegistryHandler
// test doubles that also live in internal/query/packagereg's _test.go files
// (recordingPackageRegistryGraphReader, recordingPackageRegistryCorrelationStore,
// stubPackageRegistryAggregateStore). Duplication here is not a seam gap: Go
// never compiles a package's _test.go files into anything another package can
// import, so no export level fixes this, on either side of the #6060 move.
// These roots test files predate the move and are not package-registry-only
// (graphReadSweepCases()/assertGraphReadSweepResponse() are shared across ~20
// root graph-read-availability suites, and collector_list_readiness_handler_test.go
// exercises 4 unrelated handler families in one table), so they stay in root
// rather than moving into packagereg with the family.

// recordingPackageRegistryGraphReader is a minimal querycontract.GraphQuery
// double for the root package-registry graph-read-availability sweep and the
// collector-readiness handler table. It mirrors packagereg's test double of
// the same name; see that type's doc comment for the full runRowsQueue/
// errByCall call-sequencing contract this copy also implements.
type recordingPackageRegistryGraphReader struct {
	runRows      []map[string]any
	runRowsQueue [][]map[string]any
	errByCall    map[int]error
	callCount    int
}

func (r *recordingPackageRegistryGraphReader) Run(
	_ context.Context,
	_ string,
	_ map[string]any,
) ([]map[string]any, error) {
	r.callCount++
	if err := r.errByCall[r.callCount]; err != nil {
		return nil, err
	}
	if len(r.runRowsQueue) > 0 {
		next := r.runRowsQueue[0]
		r.runRowsQueue = r.runRowsQueue[1:]
		return next, nil
	}
	return r.runRows, nil
}

func (*recordingPackageRegistryGraphReader) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

// recordingPackageRegistryCorrelationStore is a minimal
// packagereg.PackageRegistryCorrelationStore double for root's
// repository-selector and collector-readiness handler tests. It mirrors
// packagereg's test double of the same name, trimmed to the plumbing those
// root tests assert on (rows, lastFilter); it does not model the raw-fact
// window/truncation contract packagereg's pagination-focused fakes cover.
type recordingPackageRegistryCorrelationStore struct {
	rows       []packagereg.PackageRegistryCorrelationRow
	lastFilter packagereg.PackageRegistryCorrelationFilter
}

func (s *recordingPackageRegistryCorrelationStore) ListPackageRegistryCorrelations(
	_ context.Context,
	filter packagereg.PackageRegistryCorrelationFilter,
) (packagereg.PackageRegistryCorrelationPage, error) {
	s.lastFilter = filter
	rows := append([]packagereg.PackageRegistryCorrelationRow(nil), s.rows...)
	truncated := filter.Limit > 0 && len(rows) > filter.Limit
	if truncated {
		rows = rows[:filter.Limit]
	}
	return packagereg.PackageRegistryCorrelationPage{Rows: rows, Truncated: truncated, WindowFactCount: len(rows)}, nil
}

// stubPackageRegistryAggregateStore is a minimal
// packagereg.PackageRegistryAggregateStore double for root's aggregate
// graph-read-availability sweep, trimmed to the error-injection fields those
// tests need.
type stubPackageRegistryAggregateStore struct {
	countErr     error
	inventoryErr error
}

func (s *stubPackageRegistryAggregateStore) CountPackageRegistryPackages(
	context.Context,
	packagereg.PackageRegistryAggregateFilter,
) (packagereg.PackageRegistryAggregateCount, error) {
	if s.countErr != nil {
		return packagereg.PackageRegistryAggregateCount{}, s.countErr
	}
	return packagereg.PackageRegistryAggregateCount{}, nil
}

func (s *stubPackageRegistryAggregateStore) PackageRegistryPackageInventory(
	context.Context,
	packagereg.PackageRegistryAggregateFilter,
	packagereg.PackageRegistryInventoryDimension,
	int,
	int,
) ([]packagereg.PackageRegistryInventoryRow, error) {
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return nil, nil
}
