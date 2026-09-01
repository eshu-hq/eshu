// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "go.opentelemetry.io/otel/sdk/metric/metricdata"

// counterTotal sums every int64 counter data point named name across the
// collected resource metrics. Several drift and admission test suites in
// this package assert on OTEL counter totals; this helper stayed in the
// reducer root when the terraform_config_state_drift family moved to
// internal/reducer/tfconfigstate (issue #6061), since aws_cloud_runtime_drift,
// multi_cloud_runtime_drift, and cloud_inventory_admission still use it here
// and tfconfigstate's own tests keep an identical copy scoped to that
// package.
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
