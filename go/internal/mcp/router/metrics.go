// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// routerMeterName scopes the router instruments. Like the rest of the mcp
// tree, the router records through the global meter provider cmd/mcp-server
// installs, because the MCP server does not build a telemetry.Instruments.
const routerMeterName = "eshu/go/internal/mcp/router"

// Bounded outcome labels for eshu_dp_mcp_router_find_total.
const (
	findOutcomeCandidates = "candidates"
	findOutcomeEmpty      = "empty"
	findOutcomeError      = "error"
)

// Bounded outcome labels for eshu_dp_mcp_router_call_total.
const (
	callOutcomeDispatched      = "dispatched"
	callOutcomeInvalidArgument = "invalid_arguments"
	callOutcomeUnknownTool     = "unknown_tool"
	callOutcomeRefused         = "refused"
)

// unregisteredToolLabel replaces a call_tool target that is not a registered
// tool, so a caller cannot grow the tool label without bound.
const unregisteredToolLabel = "unregistered"

// targetRankBuckets put each find_tool position (1..MaxLimit) in its own
// bucket, with 0 meaning the target was not offered.
var targetRankBuckets = []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

type routerInstruments struct {
	find metric.Int64Counter
	call metric.Int64Counter
	rank metric.Int64Histogram
}

var (
	routerInstOnce sync.Once
	routerInst     *routerInstruments
)

// routerMetrics returns the process-wide router instruments, registered once
// against the global meter. A failed registration leaves a field nil and the
// recorders skip it.
func routerMetrics() *routerInstruments {
	routerInstOnce.Do(func() {
		meter := otel.Meter(routerMeterName)
		inst := &routerInstruments{}
		if counter, err := meter.Int64Counter(
			"eshu_dp_mcp_router_find_total",
			metric.WithDescription("MCP find_tool requests by outcome: candidates, empty, or error"),
		); err == nil {
			inst.find = counter
		}
		if counter, err := meter.Int64Counter(
			"eshu_dp_mcp_router_call_total",
			metric.WithDescription("MCP call_tool requests by target tool and outcome: dispatched, invalid_arguments, unknown_tool, or refused"),
		); err == nil {
			inst.call = counter
		}
		if hist, err := meter.Int64Histogram(
			"eshu_dp_mcp_router_target_rank",
			metric.WithDescription("Position of each dispatched call_tool target in the same session's last find_tool result, 1-based; 0 when it was not offered"),
			metric.WithExplicitBucketBoundaries(targetRankBuckets...),
		); err == nil {
			inst.rank = hist
		}
		routerInst = inst
	})
	return routerInst
}

// resetRouterMetricsForTest rebinds the instruments to the current global
// meter provider.
func resetRouterMetricsForTest() {
	routerInstOnce = sync.Once{}
	routerInst = nil
}

func recordFind(ctx context.Context, outcome string) {
	if inst := routerMetrics(); inst.find != nil {
		inst.find.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}

func recordCall(ctx context.Context, tool, outcome string) {
	if inst := routerMetrics(); inst.call != nil {
		inst.call.Add(ctx, 1, metric.WithAttributes(
			attribute.String("tool", tool),
			attribute.String("outcome", outcome),
		))
	}
}

func recordTargetRank(ctx context.Context, rank int) {
	if inst := routerMetrics(); inst.rank != nil {
		inst.rank.Record(ctx, int64(rank))
	}
}
