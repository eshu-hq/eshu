// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// defaultToolResponseByteBudget caps the serialized size of any single MCP tool
// response before it is handed back to the LLM client. A heavy graph-returning
// tool (a large subgraph, a wide story, a deep visualization packet) can
// otherwise serialize an arbitrarily large payload straight into the model
// context window and blow the repo-scale performance contract. The dispatch
// boundary is the one hub every tool response passes through, so the budget is
// enforced here once rather than per route. Per-route token budgets (for
// example the relationship-story token_budget) still apply first; this is the
// outer, tool-agnostic guard. 256 KiB is roughly 64k tokens at the repo's
// conservative ~4-bytes-per-token heuristic, large enough for any honestly
// bounded read yet small enough to refuse a runaway payload.
const defaultToolResponseByteBudget = 256 * 1024

// errorCodeResponseOverBudget is the canonical error code returned when a tool
// response exceeds the dispatch response-size budget. It is an MCP-dispatch
// concern, not a query-layer capability error, so it is defined here rather than
// in the query package's ErrorCode enum, mirroring the mcp_dispatch_timeout code
// emitted by the dispatch deadline guard.
const errorCodeResponseOverBudget query.ErrorCode = "mcp_response_over_budget"

// applyResponseBudget enforces a serialized response-size budget on a dispatch
// result. When budget <= 0 the guard is disabled and the result is returned
// unchanged. When the serialized response exceeds the budget, the oversized
// payload is replaced with a small, bounded canonical envelope that names the
// budget, the actual size, the estimated token cost, and how to narrow the
// query, and the original result is dropped so it never reaches the client.
//
// The replacement is itself an error envelope (IsError=true) so MCP clients and
// summarizers treat it as a structured failure, not partial data. A structured
// log event records every budget hit for 3 AM operability.
func applyResponseBudget(result *dispatchResult, toolName string, budget int, logger *slog.Logger) *dispatchResult {
	if result == nil || budget <= 0 {
		return result
	}
	size := estimateResponseBytes(result)
	recordResponseBytes(toolName, size)
	if size <= budget {
		return result
	}
	recordResponseOverBudget(toolName)
	if logger != nil {
		logger.Warn(
			"mcp tool response over budget",
			"tool", toolName,
			"response_bytes", size,
			"budget_bytes", budget,
		)
	}
	return overBudgetResult(toolName, size, budget)
}

// estimateResponseBytes returns the serialized byte size the dispatch result
// would occupy in the MCP tools/call wire response. handleMessage emits the same
// payload twice in a single mcpToolResult: once as the raw structuredContent
// object and again, JSON-string-escaped, inside the resource.Text block. Sizing
// only one copy lets a ~130-256 KiB payload clear a 256 KiB guard while shipping
// ~2x that on the wire, defeating the dispatch budget. So both copies are
// counted: the canonical envelope when present, otherwise the plain value.
//
// The structuredContent copy is the marshaled payload itself. The resource.Text
// copy is that payload re-encoded as a JSON string, so its on-wire size is the
// quoted length, which json.Marshal of the string reports exactly (including
// surrounding quotes and any escaping). A marshal failure yields 0, which fails
// open (no false-positive truncation) rather than refusing a payload that could
// not be sized.
func estimateResponseBytes(result *dispatchResult) int {
	if result == nil {
		return 0
	}
	var payload any
	if result.Envelope != nil {
		payload = result.Envelope
	} else {
		payload = result.Value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0
	}
	quoted, err := json.Marshal(string(encoded))
	if err != nil {
		return 0
	}
	// structuredContent copy + resource.Text (JSON-string-escaped) copy.
	return len(encoded) + len(quoted)
}

// estimateResponseTokens converts a serialized byte size into a conservative
// token estimate using the repo's shared ~4-bytes-per-token heuristic. It is a
// bound, not a billing-grade tokenizer.
func estimateResponseTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}

// overBudgetResult builds the bounded canonical envelope returned in place of an
// over-budget tool response.
func overBudgetResult(toolName string, size, budget int) *dispatchResult {
	envelope := &query.ResponseEnvelope{
		Data: nil,
		Error: &query.ErrorEnvelope{
			Code:       errorCodeResponseOverBudget,
			Message:    fmt.Sprintf("MCP tool %q response of %d bytes exceeds the %d byte response budget", toolName, size, budget),
			Capability: "mcp.dispatch",
			Details: map[string]any{
				"tool":             toolName,
				"response_bytes":   size,
				"budget_bytes":     budget,
				"estimated_tokens": estimateResponseTokens(size),
				"guidance":         responseBudgetGuidance(),
			},
		},
	}
	return &dispatchResult{
		Value:    envelope,
		Envelope: envelope,
		IsError:  true,
	}
}

// responseBudgetGuidance returns a deterministic instruction teaching the agent
// how to shrink a tool response that exceeded the dispatch budget.
func responseBudgetGuidance() string {
	return "response exceeded the MCP response budget; lower limit, add repo_id/scope filters, " +
		"request a single relationship_type or direction, set a smaller token_budget where supported, " +
		"then drill in via the returned handles instead of fetching the whole result at once; " +
		"a row with source_cache_clipped=true has a clipped body, so call get_entity_content for its full text"
}

// dispatchBudgetMeterName scopes the lazily registered budget instruments to
// this package. Like the transport-auth counter, they record through the global
// meter provider cmd/mcp-server installs, because this package does not build a
// telemetry.Instruments value.
const dispatchBudgetMeterName = "eshu/go/internal/mcp"

// responseBytesBuckets span the 256 KiB dispatch budget in bytes. A tool whose
// p95 climbs toward the top bucket is heading for mcp_response_over_budget
// before any caller sees the error.
var responseBytesBuckets = []float64{
	1024, 4096, 16384, 32768, 65536, 98304, 131072, 196608, 262144, 524288, 1048576,
}

// dispatchBudgetInstruments holds the per-tool response-size histogram and the
// over-budget counter. Fields stay nil when registration fails (for example
// before a meter provider is installed); callers nil-check before recording.
type dispatchBudgetInstruments struct {
	bytes      metric.Int64Histogram
	overBudget metric.Int64Counter
}

var (
	dispatchBudgetOnce sync.Once
	dispatchBudgetInst *dispatchBudgetInstruments
)

// dispatchBudgetMetrics returns the process-wide budget instruments,
// registering them once against the global meter.
func dispatchBudgetMetrics() *dispatchBudgetInstruments {
	dispatchBudgetOnce.Do(func() {
		meter := otel.Meter(dispatchBudgetMeterName)
		inst := &dispatchBudgetInstruments{}
		if hist, err := meter.Int64Histogram(
			"eshu_dp_mcp_response_bytes",
			metric.WithDescription("Serialized MCP tools/call response size in bytes (both wire copies, as the dispatch budget counts them), labeled by tool, recorded for every response the budget guard sizes"),
			metric.WithUnit("By"),
			metric.WithExplicitBucketBoundaries(responseBytesBuckets...),
		); err == nil {
			inst.bytes = hist
		}
		if counter, err := meter.Int64Counter(
			"eshu_dp_mcp_response_over_budget_total",
			metric.WithDescription("MCP tool responses replaced by the mcp_response_over_budget error envelope, labeled by tool, so an operator sees which tool defaults exceed the response budget"),
		); err == nil {
			inst.overBudget = counter
		}
		dispatchBudgetInst = inst
	})
	return dispatchBudgetInst
}

// resetDispatchBudgetMetricsForTest rebinds the lazily registered instruments so
// a test can register them against its own meter provider.
func resetDispatchBudgetMetricsForTest() {
	dispatchBudgetOnce = sync.Once{}
	dispatchBudgetInst = nil
}

// toolAttr labels a budget sample with the tool name. The name is a registered
// tool (resolveRoute has already succeeded), so the label set is bounded by the
// tool catalog.
func toolAttr(toolName string) attribute.KeyValue {
	return attribute.String("tool", toolName)
}

// recordResponseBytes records one sized response for toolName.
func recordResponseBytes(toolName string, size int) {
	if inst := dispatchBudgetMetrics(); inst.bytes != nil {
		inst.bytes.Record(context.Background(), int64(size), metric.WithAttributes(toolAttr(toolName)))
	}
}

// recordResponseOverBudget counts one response replaced by the over-budget
// envelope for toolName.
func recordResponseOverBudget(toolName string) {
	if inst := dispatchBudgetMetrics(); inst.overBudget != nil {
		inst.overBudget.Add(context.Background(), 1, metric.WithAttributes(toolAttr(toolName)))
	}
}
