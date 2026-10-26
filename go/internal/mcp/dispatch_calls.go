// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"errors"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// errUnknownTool is the cause selectRoute reports for a tool name no route
// claims. Its text is the dispatch error a client has always seen.
var errUnknownTool = errors.New("unknown tool")

// Outcome labels for eshu_dp_mcp_tool_calls_total. Each names one way
// dispatchToolWithOptions can end; the set is closed.
const (
	// callOutcomeOK is a handler answer the client sees as a success,
	// including a budget page or resource-only fallback.
	callOutcomeOK = "ok"
	// callOutcomeError is a handler answer with a non-2xx status, or a
	// dispatch failure after routing (request build, non-envelope HTTP error).
	callOutcomeError = "error"
	// callOutcomeOverBudget is a response replaced by the over-budget envelope.
	callOutcomeOverBudget = "over_budget"
	// callOutcomeUnknownTool is a name no route claims.
	callOutcomeUnknownTool = "unknown_tool"
	// callOutcomeInvalidRequest is a registered tool whose arguments the
	// route builder refused before any request was built.
	callOutcomeInvalidRequest = "invalid_request"
	// callOutcomeTimeout is the dispatch deadline firing with no usable answer.
	callOutcomeTimeout = "timeout"
	// callOutcomePartialTimeout is the dispatch deadline firing after the
	// handler wrote a bounded deadline_truncated envelope that was returned.
	callOutcomePartialTimeout = "partial_timeout"
	// callOutcomeCanceled is the caller's context ending before the answer.
	callOutcomeCanceled = "canceled"
	// callOutcomePanic is a dispatch that unwound by panic.
	callOutcomePanic = "panic"
)

// callOutcomes lists every outcome label, so the per-series options are built
// for exactly the closed set.
var callOutcomes = []string{
	callOutcomeOK, callOutcomeError, callOutcomeOverBudget, callOutcomeUnknownTool,
	callOutcomeInvalidRequest, callOutcomeTimeout, callOutcomePartialTimeout,
	callOutcomeCanceled, callOutcomePanic,
}

// unknownToolLabel replaces a requested name that is not a registered tool, so
// a client cannot grow the tool label without bound.
const unknownToolLabel = "unknown"

// dispatchCallInstruments holds the dispatch-entry counter and one prebuilt
// attribute option per (tool, outcome), so the per-call write allocates no
// attribute set.
type dispatchCallInstruments struct {
	calls   metric.Int64Counter
	options map[string][]metric.AddOption
}

var (
	dispatchCallOnce sync.Once
	dispatchCallInst *dispatchCallInstruments
)

// dispatchCallMetrics returns the process-wide call counter, registered once
// against the global meter like the budget instruments beside it.
func dispatchCallMetrics() *dispatchCallInstruments {
	dispatchCallOnce.Do(func() {
		inst := &dispatchCallInstruments{options: map[string][]metric.AddOption{}}
		if counter, err := otel.Meter(dispatchBudgetMeterName).Int64Counter(
			"eshu_dp_mcp_tool_calls_total",
			metric.WithDescription("MCP tools/call dispatches counted when the dispatcher starts, labeled by registered tool (unknown for any other name) and how the dispatch ended"),
		); err == nil {
			inst.calls = counter
		}
		tools := ReadOnlyTools()
		names := make([]string, 0, len(tools)+1)
		for _, tool := range tools {
			names = append(names, tool.Name)
		}
		names = append(names, unknownToolLabel)
		for _, name := range names {
			opts := make([]metric.AddOption, len(callOutcomes))
			for i, outcome := range callOutcomes {
				opts[i] = metric.WithAttributeSet(attribute.NewSet(
					attribute.String("tool", name), attribute.String("outcome", outcome),
				))
			}
			inst.options[name] = opts
		}
		dispatchCallInst = inst
	})
	return dispatchCallInst
}

// resetDispatchCallMetricsForTest rebinds the counter to the current global
// meter provider.
func resetDispatchCallMetricsForTest() {
	dispatchCallOnce = sync.Once{}
	dispatchCallInst = nil
}

// dispatchCall tracks one tools/call from the moment the dispatcher knows the
// tool name. It is written when dispatch ends, in a defer, so a timeout, a
// cancellation, an early refusal, and a panic all count.
type dispatchCall struct {
	label   string
	outcome string
}

// boundedToolLabel returns a registered tool name or the one unknown bucket.
// It only reads the prebuilt catalog options; it does not count another call.
func boundedToolLabel(requested string) string {
	if _, ok := dispatchCallMetrics().options[requested]; ok && requested != unknownToolLabel {
		return requested
	}
	return unknownToolLabel
}

// startDispatchCall resolves the bounded tool label for requested.
func startDispatchCall(requested string) dispatchCall {
	return dispatchCall{label: boundedToolLabel(requested)}
}

// routeRejected records that route resolution refused the call.
func (c *dispatchCall) routeRejected(err error) {
	if errors.Is(err, errUnknownTool) {
		c.outcome = callOutcomeUnknownTool
		return
	}
	c.outcome = callOutcomeInvalidRequest
}

// partialAnswer records that the deadline fired but a bounded partial answer
// was returned.
func (c *dispatchCall) partialAnswer() { c.outcome = callOutcomePartialTimeout }

// finish writes the counter. completed is false when dispatch unwound by panic.
func (c *dispatchCall) finish(ctx context.Context, result *dispatchResult, err error, completed bool) {
	outcome := c.outcome
	switch {
	case !completed:
		outcome = callOutcomePanic
	case outcome != "":
	case err != nil:
		outcome = errorCallOutcome(err)
	case result != nil && result.IsError && isOverBudget(result):
		outcome = callOutcomeOverBudget
	case result != nil && result.IsError:
		outcome = callOutcomeError
	default:
		outcome = callOutcomeOK
	}
	inst := dispatchCallMetrics()
	if inst.calls == nil {
		return
	}
	for i, name := range callOutcomes {
		if name == outcome {
			inst.calls.Add(ctx, 1, inst.options[c.label][i])
			return
		}
	}
}

// errorCallOutcome classifies a dispatch error that is not a route refusal.
func errorCallOutcome(err error) string {
	var ctxErr *dispatchContextErr
	if errors.As(err, &ctxErr) {
		if errors.Is(ctxErr.err, context.DeadlineExceeded) {
			return callOutcomeTimeout
		}
		return callOutcomeCanceled
	}
	return callOutcomeError
}

// isOverBudget reports whether result is the over-budget replacement envelope.
func isOverBudget(result *dispatchResult) bool {
	env := result.Envelope
	return env != nil && env.Error != nil && env.Error.Code == query.ErrorCode(errorCodeResponseOverBudget)
}
