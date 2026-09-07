// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudinventory

import (
	"database/sql"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// convergentFactResult is the [sql.Result] convergentFactStore.ExecContext
// returns. It is a verbatim local copy of the reducer root's
// fakeWorkloadIdentityResult: Go test files cannot share unexported symbols
// across a package boundary, so this package keeps its own copy rather than
// reaching into the reducer root's test files (issue #6061). Both methods
// report success, matching the always-successful upsert the convergence test
// simulates.
type convergentFactResult struct{}

func (convergentFactResult) LastInsertId() (int64, error) { return 0, nil }

func (convergentFactResult) RowsAffected() (int64, error) { return 1, nil }

var _ sql.Result = convergentFactResult{}

// counterTotal sums every int64 counter data point named name across the
// collected resource metrics. It is a verbatim local copy of the reducer
// root's metrics_counter_total_test_helper_test.go helper: Go test files
// cannot share unexported symbols across a package boundary, so this package
// keeps its own copy rather than reaching into the reducer root's test files
// (issue #6061), matching how tfconfigstate keeps its own identical copy.
func counterTotal(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}
