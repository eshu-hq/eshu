// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// TestDispatchCallCounterThroughRouterPath drives Server.callTool, the path a
// real tools/call takes, and pins which router calls reach the dispatcher. A
// direct call and a call_tool target both dispatch and count under the target
// name. find_tool, a call_tool the router refuses (unknown target, bad
// arguments), never reach the dispatcher; they are counted by
// eshu_dp_mcp_router_call_total, not by this counter. A direct unregistered
// name does reach the dispatcher and counts as unknown/unknown_tool.
func TestDispatchCallCounterThroughRouterPath(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	reader := callMetricsForTest(t)
	s := newRouterTestServer(t, echoEnvelopeHandler())
	ctx := context.Background()
	symbol := map[string]any{"symbol": "Handle", "limit": 5}

	toolCall(t, s, ctx, "find_symbol", symbol)
	toolCall(t, s, ctx, router.CallToolName, map[string]any{"name": "find_symbol", "arguments": symbol})
	requireToolCalls(t, reader, map[string]int64{"find_symbol/ok": 2})

	toolCall(t, s, ctx, router.FindToolName, map[string]any{"intent": "find a symbol"})
	toolCall(t, s, ctx, router.CallToolName, map[string]any{"name": "no_such_tool_a1b2c3", "arguments": map[string]any{}})
	toolCall(t, s, ctx, router.CallToolName, map[string]any{"name": "find_symbol", "arguments": "not-an-object"})
	toolCall(t, s, ctx, router.CallToolName, map[string]any{"arguments": symbol})
	requireToolCalls(t, reader, map[string]int64{"find_symbol/ok": 2})

	toolCall(t, s, ctx, "no_such_tool_a1b2c3", nil)
	requireToolCalls(t, reader, map[string]int64{"find_symbol/ok": 2, "unknown/unknown_tool": 1})
}

// TestDispatchCallCounterConcurrentCallsKeepExactTotals runs many goroutines
// through the shared attribute-set cache with registered, unknown, empty,
// wrongly cased and bad-argument names. Under -race it proves the cache is
// read-only after first use and that no increment is lost or mislabeled.
func TestDispatchCallCounterConcurrentCallsKeepExactTotals(t *testing.T) {
	// Not parallel: installs a process-global meter provider.
	reader := callMetricsForTest(t)
	handler := echoEnvelopeHandler()
	const goroutines, rounds = 16, 25
	symbol := map[string]any{"symbol": "Handle", "limit": 5}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ctx := context.Background()
			for r := 0; r < rounds; r++ {
				_, _ = dispatchForCall(ctx, handler, "find_symbol", symbol, dispatchOptions{})
				_, _ = dispatchForCall(ctx, handler, fmt.Sprintf("unregistered_%d_%d", g, r), nil, dispatchOptions{})
				_, _ = dispatchForCall(ctx, handler, "", nil, dispatchOptions{})
				_, _ = dispatchForCall(ctx, handler, "FIND_SYMBOL", symbol, dispatchOptions{})
				_, _ = dispatchForCall(ctx, handler, "get_workload_context", map[string]any{}, dispatchOptions{})
			}
		}(g)
	}
	wg.Wait()

	total := int64(goroutines * rounds)
	requireToolCalls(t, reader, map[string]int64{
		"find_symbol/ok":                       total,
		"unknown/unknown_tool":                 3 * total,
		"get_workload_context/invalid_request": total,
	})
}
