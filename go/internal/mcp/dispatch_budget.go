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
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// defaultToolResponseByteBudget caps the serialized mcpToolResult, excluding
// the enclosing JSON-RPC response and transport newline. A heavy graph-returning
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
// unchanged. If both complete copies exceed the budget but the full embedded
// resource fits, the structured copy is omitted. If the resource also exceeds
// the budget and the tool pages (budgetPagedTools), the result becomes the
// largest page of whole rows that fits, marked truncated with a next_offset
// (trimToBudgetPage). Otherwise, or when the first row alone is over budget,
// a bounded error envelope replaces the result.
//
// The replacement is itself an error envelope (IsError=true) so MCP clients and
// summarizers treat it as a structured failure, not partial data. A structured
// log event records every budget hit for 3 AM operability.
func applyResponseBudget(result *dispatchResult, toolName string, budget int, logger *slog.Logger) *dispatchResult {
	if result == nil {
		return result
	}
	result.ToolName = toolName
	if budget <= 0 {
		return result
	}
	size := estimateResponseBytes(result)
	recordResponseBytes(toolName, size)
	if size <= budget {
		return result
	}
	result.ResourceOnly = true
	emittedSize := estimateResponseBytes(result)
	if emittedSize > 0 && emittedSize <= budget {
		recordResponseResourceFallback(toolName)
		if logger != nil {
			logger.Info("mcp tool response resource fallback",
				"tool", toolName,
				"response_bytes", size,
				"emitted_bytes", emittedSize,
				"budget_bytes", budget,
			)
		}
		return result
	}
	if page, ok := trimToBudgetPage(result, toolName, budget); ok {
		recordResponseBudgetPage(toolName)
		if logger != nil {
			logger.Info("mcp tool response budget page",
				"tool", toolName,
				"response_bytes", size,
				"emitted_bytes", page.bytes,
				"budget_bytes", budget,
				"rows_returned", page.rowsReturned,
				"rows_available", page.rowsAvailable,
			)
		}
		return page.result
	}
	recordResponseOverBudget(toolName)
	if logger != nil {
		logger.Warn(
			"mcp tool response over budget",
			"tool", toolName,
			"response_bytes", size,
			"resource_bytes", emittedSize,
			"budget_bytes", budget,
		)
	}
	return overBudgetResult(toolName, size, budget)
}

// estimateResponseBytes returns the exact serialized size of mcpToolResult,
// including the summary, embedded resource, and optional structured copy. It
// excludes the JSON-RPC wrapper and transport newline, as the old guard did.
// A marshal failure yields 0 so the guard does not refuse an unsized response.
func estimateResponseBytes(result *dispatchResult) int {
	if result == nil {
		return 0
	}
	encoded, err := json.Marshal(renderToolResult(result.ToolName, result))
	if err != nil {
		return 0
	}
	return len(encoded)
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
	bytes            metric.Int64Histogram
	overBudget       metric.Int64Counter
	resourceFallback metric.Int64Counter
	budgetPage       metric.Int64Counter
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
			metric.WithDescription("Attempted serialized MCP tools/call result size in bytes, including both wire copies, labeled by tool"),
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
		if counter, err := meter.Int64Counter(
			"eshu_dp_mcp_response_resource_fallback_total",
			metric.WithDescription("MCP tool responses whose full resource was preserved by omitting oversized structuredContent, labeled by tool"),
		); err == nil {
			inst.resourceFallback = counter
		}
		if counter, err := meter.Int64Counter(
			"eshu_dp_mcp_response_budget_page_total",
			metric.WithDescription("MCP tool responses trimmed to a page of whole rows with truncated=true and a next_offset because the full result exceeded the response budget, labeled by tool"),
		); err == nil {
			inst.budgetPage = counter
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

// recordResponseResourceFallback counts a complete response emitted as one
// embedded resource after its two-copy form exceeded the byte budget.
func recordResponseResourceFallback(toolName string) {
	if inst := dispatchBudgetMetrics(); inst.resourceFallback != nil {
		inst.resourceFallback.Add(context.Background(), 1, metric.WithAttributes(toolAttr(toolName)))
	}
}

// recordResponseBudgetPage counts one over-budget response answered as a page
// of whole rows instead of the over-budget error envelope.
func recordResponseBudgetPage(toolName string) {
	if inst := dispatchBudgetMetrics(); inst.budgetPage != nil {
		inst.budgetPage.Add(context.Background(), 1, metric.WithAttributes(toolAttr(toolName)))
	}
}

// budgetPageReason is the reason string a budget page reports in
// data.budget_page.reason and in the truth.omissions detail.
const budgetPageReason = "response_byte_budget"

// budgetPagedTools names the tools whose data.results rows are independent,
// ordered, and re-readable through the data.offset the same tool accepts.
// Only these tools can answer an over-budget request with a page: a client
// that gets next_offset can pass it back as offset and read the remainder, so
// no row is lost. Every other tool keeps the over-budget error envelope.
var budgetPagedTools = map[string]struct{}{
	"find_code":             {},
	"search_entity_content": {},
}

// budgetPage is the outcome of trimming an over-budget response to a page.
type budgetPage struct {
	result        *dispatchResult
	rowsReturned  int
	rowsAvailable int
	bytes         int
}

// trimToBudgetPage trims an over-budget success envelope to the largest page
// of whole rows that fits budget in its single-copy (resource-only) form. It
// reports false when the tool does not page, the response is not a row list,
// or even the first row alone is over budget; the caller then keeps the
// over-budget error envelope.
//
// The trim runs on the serialized-size measure the budget itself uses
// (estimateResponseBytes), not on an estimate, so a returned page is within
// budget by construction. The page keeps the first rows in handler order,
// marks data.truncated, and sets data.next_offset to the offset of the first
// dropped row, so offset-paging the same request reads every row exactly once.
// The cut is a binary search over whole rows; a row is never split.
func trimToBudgetPage(result *dispatchResult, toolName string, budget int) (budgetPage, bool) {
	if _, paged := budgetPagedTools[toolName]; !paged || result == nil || result.IsError {
		return budgetPage{}, false
	}
	envelope := result.Envelope
	if envelope == nil || envelope.Error != nil || envelope.Truth == nil {
		return budgetPage{}, false
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		return budgetPage{}, false
	}
	rows, ok := data["results"].([]any)
	if !ok || len(rows) < 2 {
		return budgetPage{}, false
	}
	offset := pageOffset(data["offset"])
	candidate := func(keep int) *dispatchResult {
		page := budgetPageEnvelope(envelope, data, rows, keep, offset, budget)
		return &dispatchResult{Value: page, Envelope: page, ToolName: toolName, ResourceOnly: true}
	}
	if size := estimateResponseBytes(candidate(1)); size <= 0 || size > budget {
		return budgetPage{}, false
	}
	// The full row set is known to be over budget in this form, so the answer
	// lies in [1, len(rows)-1]; keep is the largest count known to fit.
	keep, over := 1, len(rows)
	for over-keep > 1 {
		middle := keep + (over-keep)/2
		if size := estimateResponseBytes(candidate(middle)); size > 0 && size <= budget {
			keep = middle
		} else {
			over = middle
		}
	}
	page := candidate(keep)
	return budgetPage{
		result:        page,
		rowsReturned:  keep,
		rowsAvailable: len(rows),
		bytes:         estimateResponseBytes(page),
	}, true
}

// budgetPageEnvelope returns a copy of envelope that carries only the first
// keep rows. The original envelope and its data map are left untouched. The
// copy reports truncated, the next offset, a budget_page block, a
// truth.omissions entry, and clip counts recomputed over the kept rows.
func budgetPageEnvelope(
	envelope *query.ResponseEnvelope,
	data map[string]any,
	rows []any,
	keep, offset, budget int,
) *query.ResponseEnvelope {
	page := make(map[string]any, len(data)+2)
	for key, value := range data {
		page[key] = value
	}
	kept := rows[:keep]
	page["results"] = kept
	page["count"] = keep
	page["truncated"] = true
	page["next_offset"] = offset + keep
	page["budget_page"] = map[string]any{
		"reason":         budgetPageReason,
		"budget_bytes":   budget,
		"rows_returned":  keep,
		"rows_available": len(rows),
	}
	recountClipped(page, "source_cache_clipped_rows", "source_cache_clipped", kept)
	recountClipped(page, "docstring_clipped_rows", "docstring_clipped", kept)

	truth := *envelope.Truth
	truth.Omissions = append(append([]querycontract.TruthOmission(nil), truth.Omissions...), querycontract.TruthOmission{
		Section: "results",
		Detail:  budgetPageReason,
		Total:   len(rows),
	})
	return &query.ResponseEnvelope{Data: page, Truth: &truth, Error: envelope.Error}
}

// recountClipped rewrites a response-level clipped-row count over the kept
// rows. It does nothing when the response never carried the count.
func recountClipped(page map[string]any, countKey, rowKey string, rows []any) {
	if _, present := page[countKey]; !present {
		return
	}
	clipped := 0
	for _, row := range rows {
		if fields, ok := row.(map[string]any); ok && fields[rowKey] == true {
			clipped++
		}
	}
	page[countKey] = clipped
}

// pageOffset reads the page's start offset from the decoded JSON data block.
// A missing, non-numeric, or negative value is offset zero.
func pageOffset(value any) int {
	if number, ok := value.(float64); ok && number > 0 {
		return int(number)
	}
	return 0
}
