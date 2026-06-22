package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"

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
	if size <= budget {
		return result
	}
	if logger != nil {
		logger.Warn("mcp tool response over budget",
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
		"then drill in via the returned handles instead of fetching the whole result at once"
}
