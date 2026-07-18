// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// countingExecQueryer is a fake postgres.ExecQueryer that counts ExecContext
// round-trips. It is shared by every Tier-2 Postgres cost scenario (C-14
// issue #4367): each scenario wraps this fake in the SAME production
// postgres.InstrumentedDB the reducer wires in production
// (go/cmd/reducer/observed_service_wiring.go:35, StoreName "reducer"), so the
// PRIMARY assertion reads a real eshu_dp_postgres_query_duration_seconds
// histogram Count recorded by InstrumentedDB.ExecContext, not a hand-counted
// call tally. The raw execCalls counter remains available as the SECONDARY
// "statements_executed" diagnostic, mirroring groupCountingExecutor's
// totalStatements() in cost_scenario_helpers_test.go.
type countingExecQueryer struct {
	execCalls atomic.Int64
}

// ExecContext records one write round-trip and always succeeds; the reducer
// Postgres writers under test only branch on the returned error, never on
// sql.Result, so a nil result is a faithful stand-in for a real driver.
func (f *countingExecQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	f.execCalls.Add(1)
	return nil, nil
}

// QueryContext is present only to satisfy postgres.ExecQueryer; none of the
// Tier-2 Postgres cost scenarios read back rows, so it always succeeds with a
// nil cursor.
func (f *countingExecQueryer) QueryContext(context.Context, string, ...any) (postgres.Rows, error) {
	return nil, nil
}

// totalExecs returns the accumulated ExecContext call count.
func (f *countingExecQueryer) totalExecs() int64 { return f.execCalls.Load() }

// collectAttributedHistogramCount reads one named Float64 histogram's total
// Count off a Collect snapshot, restricted to data points carrying the given
// attribute key/value pair. This is distinct from aws_cloud_runtime_drift_
// cost_test.go's collectHistogramCount (2-arg, no attribute filter, already
// committed on main), which is safe there only because that scenario's
// InstrumentedDB never issues a QueryContext read — every data point on its
// eshu_dp_postgres_query_duration_seconds is already "operation=write". Every
// Tier-2 writer in this package shares that same instrument name, so an
// unfiltered sum would still be correct today, but the attribute filter
// documents and guards the "write" semantics explicitly and keeps this helper
// safe if a future scenario's fake ever answers QueryContext too. Unlike
// collectFloat64HistogramSum (cost_scenario_helpers_test.go), which sums
// every data point's Sum with no attribute filter for a Sum, this reads Count
// on a Histogram[float64].
func collectAttributedHistogramCount(rm metricdata.ResourceMetrics, name, attrKey, attrVal string) uint64 {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				return 0
			}
			var total uint64
			for _, dp := range hist.DataPoints {
				value, ok := dp.Attributes.Value(attribute.Key(attrKey))
				if !ok || value.AsString() != attrVal {
					continue
				}
				total += dp.Count
			}
			return total
		}
	}
	return 0
}

// newInstrumentedReducerDB wraps fake in the PRODUCTION postgres.InstrumentedDB
// shape the reducer wires (go/cmd/reducer/observed_service_wiring.go:35):
// StoreName "reducer", a real telemetry.Instruments registry backed by a
// ManualReader (so every scenario reads a genuine eshu_dp_* value instead of a
// re-implemented counter), and a nil Tracer — InstrumentedDB.ExecContext
// records the eshu_dp_postgres_query_duration_seconds sample on both the
// traced and untraced branch, so leaving Tracer nil changes no counted
// behavior, only skips span creation.
func newInstrumentedReducerDB(
	t *testing.T,
	fake *countingExecQueryer,
) (*postgres.InstrumentedDB, *sdkmetric.ManualReader) {
	t.Helper()

	inst, reader := newManualReaderInstruments(t)
	db := &postgres.InstrumentedDB{
		Inner:       fake,
		Tracer:      nil,
		Instruments: inst,
		StoreName:   "reducer",
	}
	return db, reader
}
