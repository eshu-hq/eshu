// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionSearchPath labels eshu_dp_infra_search_path_total with the
// statement that served an infra resource search (#7245). The value set is
// closed: fulltext (the infra_search_key_ft read) or legacy (the per-label
// scan).
const MetricDimensionSearchPath = "search_path"

// AttrSearchPath returns a search_path attribute. v must be fulltext or
// legacy.
func AttrSearchPath(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSearchPath, v)
}

// registerInfraSearchKeyInstruments registers the #7245 infra search read
// cutover signals on inst: the per-request path counter and the readiness
// gauges the API and MCP server refresh every few minutes.
func registerInfraSearchKeyInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.InfraSearchPath, err = meter.Int64Counter(
		"eshu_dp_infra_search_path_total",
		metric.WithDescription("Infra resource searches by the statement that served them (search_path: fulltext "+
			"or legacy) and why (reason: ready, flag_off, not_ready, index_not_online, unkeyed, ft_error, "+
			"backend, no_query, query_too_long). legacy/ft_error means the infra_search_key_ft read failed and the legacy "+
			"statement answered that request (#7245)"),
	); err != nil {
		return fmt.Errorf("register InfraSearchPath counter: %w", err)
	}
	if inst.InfraSearchKeyReady, err = meter.Int64Gauge(
		"eshu_dp_infra_search_key_ready",
		metric.WithDescription("1 when the last infra search key readiness probe found infra_search_key_ft "+
			"ONLINE and no unkeyed infra node, else 0 (#7245). Only a ready process can serve the full-text search"),
	); err != nil {
		return fmt.Errorf("register InfraSearchKeyReady gauge: %w", err)
	}
	if inst.InfraSearchKeyIndexOnline, err = meter.Int64Gauge(
		"eshu_dp_infra_search_key_index_online",
		metric.WithDescription("1 when the last readiness probe read infra_search_key_ft as ONLINE, else 0 "+
			"(missing, POPULATING, FAILED, or the probe failed) (#7245)"),
	); err != nil {
		return fmt.Errorf("register InfraSearchKeyIndexOnline gauge: %w", err)
	}
	if inst.InfraSearchKeyUnkeyedNodes, err = meter.Int64Gauge(
		"eshu_dp_infra_search_key_unkeyed_nodes",
		metric.WithDescription("Infra search label nodes with no infra_search_ovf flag at the last successful "+
			"readiness probe: nodes no keying writer or backfill touched, which the full-text index cannot "+
			"find (#7245). Non-zero keeps the search on the legacy statement"),
	); err != nil {
		return fmt.Errorf("register InfraSearchKeyUnkeyedNodes gauge: %w", err)
	}
	return nil
}

// registerInfraSearchKey appends the search_path dimension (#7245). reason is
// already in the root registry.
func registerInfraSearchKey() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionSearchPath)
}
