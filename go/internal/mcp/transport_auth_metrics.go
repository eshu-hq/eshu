// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// mcpAuthMeterName scopes the lazily registered transport-auth-denied
// counter to this package. This package (unlike go/internal/query, which
// receives a *telemetry.Instruments) does not build an Instruments value, so
// it records through the global meter provider that cmd/mcp-server installs
// via telemetry.NewProviders -- the same self-contained-package pattern
// go/internal/query/request_metrics.go uses for eshu_dp_api_request_*. The
// instrument name is also registered in
// go/internal/telemetry/instruments.go's InitInstruments so the
// telemetry-coverage (X2) contract check finds it there.
const mcpAuthMeterName = "eshu/go/internal/mcp"

var (
	mcpAuthDeniedOnce    sync.Once
	mcpAuthDeniedCounter metric.Int64Counter
)

// mcpTransportAuthDeniedCounter returns the process-wide transport-auth-denied
// counter, registering it once against the global meter. Registration failure
// (for example before a meter provider is installed in a unit test) leaves
// the counter nil; callers must nil-check before recording.
func mcpTransportAuthDeniedCounter() metric.Int64Counter {
	mcpAuthDeniedOnce.Do(func() {
		meter := otel.Meter(mcpAuthMeterName)
		if counter, err := meter.Int64Counter(
			"eshu_dp_mcp_transport_auth_denied_total",
			metric.WithDescription("MCP transport-level authentication denials by mcp_method and reason, so an operator can see catalog-enumeration or session-hijack attempts against initialize/tools/list/tools/call/ping/SSE"),
		); err == nil {
			mcpAuthDeniedCounter = counter
		}
	})
	return mcpAuthDeniedCounter
}

// recordMCPTransportAuthDenied records one transport-auth denial labeled by
// the bounded JSON-RPC method (or "sse") and the bounded reason
// (mcpAuthDenyReason* constants).
func recordMCPTransportAuthDenied(ctx context.Context, method, reason string) {
	counter := mcpTransportAuthDeniedCounter()
	if counter == nil {
		return
	}
	counter.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrMCPMethod(method),
		telemetry.AttrReason(reason),
	))
}
