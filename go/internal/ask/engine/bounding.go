// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"encoding/json"
	"strconv"
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

// boundedListSearchTools maps a full-inventory list/search tool to the executable
// narrowing hint fed back when an unbounded call is refused before dispatch. Each
// listed tool must carry a positive limit; an unbounded call would otherwise
// return the entire inventory and blow the 256KB response budget.
//
// Only tools whose unbounded form is a genuine runaway are listed.
// list_indexed_repositories is the reproduced case (issue #5266): its unbounded
// form returned the 431,682-byte inventory. Tools that are already bounded at the
// dispatch layer are intentionally absent — find_code carries a dispatch default
// limit and list_relationship_edges is dispatch-bounded to 50 rows (its route
// forwards only verb/source_tool/limit) — so their runaway form is a slow or
// oversized result handled after dispatch by oversizedContinuationPacket, not a
// pre-dispatch refusal. Listing a scope argument the tool's route does not forward
// would let the model "bound" a call whose scope is silently dropped, so only the
// limit — which every listed tool honours — bounds a call here.
var boundedListSearchTools = map[string]string{
	"list_indexed_repositories": "list_indexed_repositories is a full-inventory list-all; add a bounded limit (for example limit=25) and page with offset instead of requesting every indexed repository at once",
}

// boundToolCall reports whether a tool call must be refused before dispatch for
// being an unbounded broad list-all. When refused it returns the executable
// narrowing hint the engine surfaces to the model and to operators. Tools not in
// the bounded set, and calls that already carry a positive limit, are never
// refused.
func boundToolCall(name string, args map[string]any) (hint string, refused bool) {
	hintText, ok := boundedListSearchTools[name]
	if !ok {
		return "", false
	}
	if hasPositiveLimit(args) {
		return "", false
	}
	return hintText, true
}

// hasPositiveLimit reports whether args carries a "limit" argument that parses to
// a value greater than zero. It accepts every numeric shape a decoded JSON tool
// argument can take — json.Number, the float and signed/unsigned integer types,
// and a string-encoded number — so a caller or model that encodes the limit
// unusually is not mistaken for an unbounded call.
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
	case uint:
		return n > 0
	case uint32:
		return n > 0
	case uint64:
		return n > 0
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return err == nil && i > 0
	default:
		return false
	}
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
