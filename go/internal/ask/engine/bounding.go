// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// MCP dispatch error codes the engine recognises as runaway-read signals. They
// mirror the codes emitted by the mcp dispatch response-budget and deadline
// guards (go/internal/mcp/dispatch_budget.go, dispatch_timeout.go). The engine
// converts a tool result carrying one of these into a bounded continuation
// packet instead of an opaque unsupported outcome.
const (
	errCodeResponseOverBudget = "mcp_response_over_budget"
	errCodeDispatchTimeout    = "mcp_dispatch_timeout"
)

// boundingRule describes how a broad list/search tool must be bounded before the
// Ask engine will dispatch it. A call is bounded when it carries a positive
// limit, or when any one of scopeArgs is present with a non-empty value. A call
// that satisfies neither is refused before dispatch, so the 30s dispatch
// deadline and the 256KB response budget are never spent on a runaway read.
type boundingRule struct {
	// scopeArgs are alternative bounding arguments; any one present with a
	// non-empty value bounds the call even without a limit. Empty means only a
	// positive limit bounds the call.
	scopeArgs []string
	// hint is the executable narrowing instruction fed back to the model and
	// surfaced to operators when the call is refused.
	hint string
}

// boundedListSearchTools is the curated set of full-inventory list/search tools
// an Ask session must bound before dispatch. These are the tools whose unbounded
// form returned the 431,682-byte inventory and blew the response budget in issue
// #5266. Scoped searches with dispatch-layer default limits (for example
// find_code) are intentionally absent: their runaway form is a slow or oversized
// result, handled after dispatch by oversizedContinuationPacket, not a
// pre-dispatch refusal.
var boundedListSearchTools = map[string]boundingRule{
	"list_indexed_repositories": {
		hint: "list_indexed_repositories is a full-inventory list-all; add a bounded limit (for example limit=25) and page with offset instead of requesting every indexed repository at once",
	},
	"list_relationship_edges": {
		scopeArgs: []string{"repo_id", "repository", "source_tool", "relationship_type"},
		hint:      "list_relationship_edges can return the entire edge set; add a limit (for example limit=25) or scope it with repo_id, source_tool, or a single relationship_type",
	},
}

// boundToolCall reports whether a tool call must be refused before dispatch for
// being an unbounded broad list/search. When refused it returns the executable
// narrowing hint the engine surfaces to the model and to operators. Tools not in
// the bounded set, and calls that already carry a positive limit or a recognised
// scope argument, are never refused.
func boundToolCall(name string, args map[string]any) (hint string, refused bool) {
	rule, ok := boundedListSearchTools[name]
	if !ok {
		return "", false
	}
	if callIsBounded(rule, args) {
		return "", false
	}
	return rule.hint, true
}

// callIsBounded reports whether args satisfy rule: a positive limit, or any one
// of the rule's scope arguments present with a non-empty value.
func callIsBounded(rule boundingRule, args map[string]any) bool {
	if hasPositiveLimit(args) {
		return true
	}
	for _, key := range rule.scopeArgs {
		if hasNonEmptyArg(args, key) {
			return true
		}
	}
	return false
}

// hasPositiveLimit reports whether args carries a "limit" argument that parses to
// a value greater than zero. It accepts the numeric shapes a decoded JSON tool
// argument can take (json.Number, float64, and the Go integer types tests use).
func hasPositiveLimit(args map[string]any) bool {
	v, ok := args["limit"]
	if !ok {
		return false
	}
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		return err == nil && i > 0
	case float64:
		return n > 0
	case float32:
		return n > 0
	case int:
		return n > 0
	case int32:
		return n > 0
	case int64:
		return n > 0
	default:
		return false
	}
}

// hasNonEmptyArg reports whether args carries key with a non-empty, non-blank
// string value or a non-nil non-string value.
func hasNonEmptyArg(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	if s, isStr := v.(string); isStr {
		return strings.TrimSpace(s) != ""
	}
	return true
}

// refusalToolResult is the bounded JSON tool-result fed back to the model when a
// call is refused for being unbounded. It carries the executable narrowing hint
// so the model can retry a bounded call on the next turn.
func refusalToolResult(hint string) string {
	b, err := json.Marshal(map[string]any{
		"refused":   true,
		"supported": false,
		"reason":    "unbounded list/search call refused before dispatch",
		"retry":     hint,
	})
	if err != nil {
		return `{"refused":true,"supported":false}`
	}
	return string(b)
}

// oversizedContinuationPacket converts a runaway tool result (over-budget or
// dispatch-timeout error envelope) into a bounded continuation AnswerPacket. The
// packet is partial and unsupported for the failed call, but it preserves a
// useful next action: the narrowing guidance the dispatch layer attached, plus a
// recommended bounded retry of the same tool. It returns (zero, false) when the
// envelope is not a recognised runaway signal, so ordinary error envelopes keep
// their existing unsupported-packet path.
func oversizedContinuationPacket(question, toolName string, env *query.ResponseEnvelope) (query.AnswerPacket, bool) {
	code := envelopeErrorCode(env)
	if code != errCodeResponseOverBudget && code != errCodeDispatchTimeout {
		return query.AnswerPacket{}, false
	}
	reason := runawayReason(code, toolName)
	guidance := runawayGuidance(env)
	return query.AnswerPacket{
		Question:           strings.TrimSpace(question),
		PrimaryTool:        toolName,
		TruthClass:         query.AnswerTruthUnsupported,
		Supported:          false,
		Partial:            true,
		UnsupportedReasons: []string{reason},
		Limitations:        []string{guidance},
		RecommendedNextCalls: []map[string]any{{
			"tool":   toolName,
			"reason": guidance,
		}},
	}, true
}

// continuationToolResult is the bounded JSON tool-result fed to the model for a
// runaway tool result. It surfaces the narrowing guidance so the model retries a
// bounded call rather than treating the outcome as a dead end.
func continuationToolResult(pkt query.AnswerPacket) string {
	guidance := ""
	if len(pkt.Limitations) > 0 {
		guidance = pkt.Limitations[0]
	}
	b, err := json.Marshal(map[string]any{
		"supported":   false,
		"partial":     true,
		"truth_class": string(query.AnswerTruthUnsupported),
		"retry_tool":  pkt.PrimaryTool,
		"retry":       guidance,
	})
	if err != nil {
		return `{"supported":false,"partial":true}`
	}
	return string(b)
}

// runawayReason returns the bounded unsupported reason recorded on a continuation
// packet for the given runaway code.
func runawayReason(code, toolName string) string {
	switch code {
	case errCodeResponseOverBudget:
		return "tool " + toolName + " result exceeded the response budget; bounded continuation offered"
	case errCodeDispatchTimeout:
		return "tool " + toolName + " exceeded the dispatch deadline; bounded continuation offered"
	default:
		return "tool " + toolName + " returned a runaway result; bounded continuation offered"
	}
}

// runawayGuidance returns the narrowing guidance to surface for a runaway tool
// result: the dispatch layer's own guidance detail when present, else a bounded
// default that instructs adding a limit and scope.
func runawayGuidance(env *query.ResponseEnvelope) string {
	if env != nil && env.Error != nil {
		if raw, ok := env.Error.Details["guidance"]; ok {
			if s, isStr := raw.(string); isStr && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return "narrow the call: add a limit and a repo_id/scope filter, then drill in via the returned handles"
}

// envelopeErrorCode returns the string error code carried by env, or "" when env
// has no error envelope.
func envelopeErrorCode(env *query.ResponseEnvelope) string {
	if env == nil || env.Error == nil {
		return ""
	}
	return string(env.Error.Code)
}
