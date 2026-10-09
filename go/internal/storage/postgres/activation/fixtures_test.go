// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// openIsolatedBootstrapSchema gives one test its own schema with the full
// production bootstrap (postgres.ApplyBootstrap) applied. Every activation
// live proof drives real queue claims, so none may share a schema (#7479).
func openIsolatedBootstrapSchema(t *testing.T, dsn, prefix string) *sql.DB {
	t.Helper()
	return postgresproof.OpenIsolatedSchema(t, dsn, prefix, func(ctx context.Context, database *sql.DB) error {
		return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
	})
}

// counterValue sums an int64 counter's data points, restricted to the points
// whose attribute key equals value when key is set.
func counterValue(rm metricdata.ResourceMetrics, name, key, value string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if key != "" {
					if v, ok := dp.Attributes.Value(attribute.Key(key)); !ok || v.AsString() != value {
						continue
					}
				}
				total += dp.Value
			}
		}
	}
	return total
}

// histogramCount counts a float64 histogram's samples, restricted to the
// points whose outcome attribute equals outcome when outcome is set.
func histogramCount(rm metricdata.ResourceMetrics, name, outcome string) uint64 {
	var total uint64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, dp := range histogram.DataPoints {
				if outcome != "" {
					if value, ok := dp.Attributes.Value("outcome"); !ok || value.AsString() != outcome {
						continue
					}
				}
				total += dp.Count
			}
		}
	}
	return total
}
