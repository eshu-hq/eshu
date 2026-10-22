// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// maxSuggestions caps the closest registered names an unknown target gets.
const maxSuggestions = 3

// MaxToolNameBytes is the longest call_tool name the router reads. A longer
// name is rejected with a fixed error that does not echo it, before any
// lookup or edit-distance work. Registered names are far shorter.
const MaxToolNameBytes = 128

// Call serves call_tool. It refuses find_tool, call_tool, and ask; answers an
// unknown name with the closest registered names; validates arguments
// against the target's input schema; refuses a target the credential can
// never call; and otherwise hands the caller's arguments, unchanged, to the
// Dispatcher. A dispatched outcome carries the Dispatcher's value and error
// untouched so the root renders it exactly like a direct tools/call.
func (e *Engine) Call(ctx context.Context, req Request, args map[string]any) CallOutcome {
	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, contract.SpanMCPRouterCall)
	defer span.End()

	out, label, outcome := e.call(ctx, req, args)
	span.SetAttributes(
		attribute.String("mcp.router.target", label),
		attribute.String("outcome", outcome),
	)
	if outcome != callOutcomeDispatched {
		span.SetStatus(codes.Error, "call_tool "+outcome)
	}
	recordCall(ctx, label, outcome)
	if _, registered := e.registry[out.Target]; registered {
		recordTargetRank(ctx, e.recent.rank(req.Session, out.Target))
	}
	return out
}

// call returns the outcome plus the bounded tool label and outcome label.
func (e *Engine) call(ctx context.Context, req Request, args map[string]any) (CallOutcome, string, string) {
	if errs, truncated := ValidateArgumentsDetail(Tools()[1].InputSchema, args); len(errs) != 0 {
		return CallOutcome{Result: invalidArgumentsResult(CallToolName, errs, truncated)}, unregisteredToolLabel, callOutcomeInvalidArgument
	}
	name, _ := args["name"].(string)
	if len(name) > MaxToolNameBytes {
		return CallOutcome{Result: invalidArgumentsResult(CallToolName, []FieldError{{
			Path: "name", Keyword: "maxLength", Message: fmt.Sprintf("must be at most %d bytes", MaxToolNameBytes),
		}}, false)}, unregisteredToolLabel, callOutcomeInvalidArgument
	}
	arguments, _ := asObject(args["arguments"])
	out := CallOutcome{Target: name}

	if refusedTargets[name] {
		out.Result = errorResult("mcp_router_refused", "call_tool cannot run "+name,
			map[string]any{"tool": name, "reason": "recursive_target"})
		return out, name, callOutcomeRefused
	}
	tool, registered := e.registry[name]
	if !registered {
		out.Result = errorResult("mcp_router_unknown_tool", "no registered tool is named "+name,
			map[string]any{"tool": name, "suggestions": e.closestNames(name)})
		return out, unregisteredToolLabel, callOutcomeUnknownTool
	}
	if errs, truncated := ValidateArgumentsDetail(tool.InputSchema, arguments); len(errs) != 0 {
		out.Result = errorResult("mcp_router_invalid_arguments", "arguments for "+name+" do not match its input schema",
			map[string]any{
				"tool":                   name,
				"field_errors":           errs,
				"field_errors_truncated": truncated,
				"input_schema":           tool.InputSchema,
				"example":                map[string]any{"name": name, "arguments": e.examples[name]},
			})
		return out, name, callOutcomeInvalidArgument
	}
	if acc := accessFromContext(ctx); !acc.admits(e.callRouteClass(name, arguments)) {
		out.Result = errorResult("mcp_router_refused", "this credential cannot call "+name,
			map[string]any{"tool": name, "reason": "not_permitted"})
		return out, name, callOutcomeRefused
	}

	// Pass the caller's own map: a direct tools/call hands dispatch the same
	// decoded object, so the routed call is byte-identical.
	if decoded, ok := args["arguments"].(map[string]any); ok {
		arguments = decoded
	}
	out.Dispatched = true
	out.Value, out.Err = req.Dispatch(ctx, name, arguments)
	return out, name, callOutcomeDispatched
}

// callRouteClass classifies the route these exact arguments dispatch to, and
// falls back to the registry-time class when they resolve to no route (the
// Dispatcher then reports that error itself).
func (e *Engine) callRouteClass(name string, arguments map[string]any) routeClass {
	if e.resolve != nil {
		if route, err := e.resolve(name, arguments); err == nil && route != nil && route.Path != "" {
			return classifyRoute(route.Method, route.Path)
		}
	}
	return e.routes[name]
}

// closestNames returns up to maxSuggestions registered names nearest to name
// by edit distance, ties broken by name. It skips the refused targets but does
// not apply the credential or profile filter: a suggestion is a spelling hint,
// and call_tool still checks access when it is called. The caller bounds name
// by MaxToolNameBytes, so the work is at most that times the registry size.
func (e *Engine) closestNames(name string) []string {
	type scored struct {
		name     string
		distance int
	}
	scoredNames := make([]scored, 0, len(e.names))
	for _, candidate := range e.names {
		if refusedTargets[candidate] {
			continue
		}
		scoredNames = append(scoredNames, scored{candidate, editDistance(name, candidate)})
	}
	sort.Slice(scoredNames, func(i, j int) bool {
		if scoredNames[i].distance != scoredNames[j].distance {
			return scoredNames[i].distance < scoredNames[j].distance
		}
		return scoredNames[i].name < scoredNames[j].name
	})
	out := make([]string, 0, maxSuggestions)
	for _, s := range scoredNames {
		if len(out) == maxSuggestions {
			break
		}
		out = append(out, s.name)
	}
	return out
}

// editDistance is the Levenshtein distance between a and b over bytes; tool
// names are ASCII.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
