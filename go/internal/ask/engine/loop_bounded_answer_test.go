// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// overBudgetEnvelope mirrors the mcp dispatch response-budget rejection shape:
// an error envelope carrying the mcp_response_over_budget code and the narrowing
// guidance the dispatch layer attaches. It is the retained-shape reproduction of
// the 467,116-byte get_service_story and 431,682-byte list_indexed_repositories
// rejections in issue #5266.
func overBudgetEnvelope(tool string) *query.ResponseEnvelope {
	return &query.ResponseEnvelope{
		Error: &query.ErrorEnvelope{
			Code:       query.ErrorCode("mcp_response_over_budget"),
			Message:    "MCP tool " + tool + " response exceeds the 262144 byte response budget",
			Capability: "mcp.dispatch",
			Details: map[string]any{
				"tool":         tool,
				"budget_bytes": 262144,
				"guidance":     "response exceeded the MCP response budget; lower limit, add repo_id/scope filters, then drill in via the returned handles",
			},
		},
	}
}

// dispatchTimeoutEnvelope mirrors the mcp dispatch deadline rejection: an error
// envelope carrying the mcp_dispatch_timeout code, the retained-shape
// reproduction of the 30s find_code context-deadline in issue #5266.
func dispatchTimeoutEnvelope(tool string) *query.ResponseEnvelope {
	return &query.ResponseEnvelope{
		Error: &query.ErrorEnvelope{
			Code:       query.ErrorCode("mcp_dispatch_timeout"),
			Message:    "MCP tool " + tool + " exceeded the 30s dispatch deadline",
			Capability: "mcp.dispatch",
			Details: map[string]any{
				"tool":     tool,
				"guidance": "scope the search to a repo_id or path and set a smaller limit, then drill in",
			},
		},
	}
}

// serviceStoryEnvelope is the bounded, scope-first service story: a supported,
// summary-bearing answer packet with the entity's operational facts. It stands in
// for the bounded get_service_story call the reproduced run should have preferred.
func serviceStoryEnvelope() *query.ResponseEnvelope {
	pkt := query.AnswerPacket{
		PrimaryTool: "get_service_story",
		TruthClass:  query.AnswerTruthDeterministic,
		Summary:     "payments service — repository payments-api; 3 deployments across prod and staging; exposes 5 REST endpoints; depends on ledger and auth.",
		Supported:   true,
	}
	return &query.ResponseEnvelope{
		Truth: &query.TruthEnvelope{Level: query.TruthLevelExact, Basis: query.TruthBasisAuthoritativeGraph},
		Data:  map[string]any{"answer_packet": pkt},
	}
}

// indexStatusEnvelope is a generic, supported-but-off-topic summary that a
// first-supported-packet selector would wrongly publish for an entity-scoped
// question. Its summary names no entity facet from the question.
func indexStatusEnvelope() *query.ResponseEnvelope {
	return &query.ResponseEnvelope{
		Truth: &query.TruthEnvelope{Level: query.TruthLevelExact, Basis: query.TruthBasisAuthoritativeGraph},
		Data:  map[string]any{"indexed_repositories": 42, "state": "ready"},
	}
}

// TestAsk_RetainedEntityOverview_BoundedUsefulAnswer is the failing-first
// retained-shape regression for issue #5266. The scripted model reproduces the
// pathological run: it resolves the payments entity, then keeps issuing an
// unbounded list-all, an oversized service story, a broad unbounded find_code,
// and redundant status calls, never emitting a final text turn. The bounded
// answer engine must:
//
//   - refuse the unbounded list/search calls before dispatch (no 30s timeout, no
//     256KB rejection burned) with an executable narrowing hint,
//   - convert the oversized service-story result into a bounded continuation
//     packet rather than an opaque unsupported outcome,
//   - stop on evidence sufficiency well before the iteration bound, and
//   - publish the relevant entity summary, not the first-supported generic one.
func TestAsk_RetainedEntityOverview_BoundedUsefulAnswer(t *testing.T) {
	t.Parallel()

	const question = "give me an operational overview of the payments service"

	tc := func(id, name string, args map[string]any) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: name, Arguments: args}
	}
	turns := []provider.Completion{
		{ToolCalls: []provider.ToolCall{
			tc("c0a", "list_indexed_repositories", map[string]any{}),
			tc("c0b", "get_service_story", map[string]any{"service": "payments"}),
		}},
		{ToolCalls: []provider.ToolCall{
			tc("c1", "find_code", map[string]any{"query": "payments"}),
		}},
		{ToolCalls: []provider.ToolCall{
			tc("c2a", "get_index_status", map[string]any{}),
			tc("c2b", "get_service_story", map[string]any{"service": "payments", "limit": 1}),
		}},
		{ToolCalls: []provider.ToolCall{
			tc("c3", "get_index_status", map[string]any{}),
		}},
	}
	// Pad beyond MaxIterations so, absent the sufficiency stop, the loop would run
	// to the bound and fall back to first-supported prose.
	for len(turns) < DefaultOptions().MaxIterations+3 {
		turns = append(turns, turns[len(turns)-1])
	}
	adapter := &scriptedAdapter{turns: turns, errOnIdx: -1}

	var dispatched []string
	runner := RunnerFunc(func(_ context.Context, name string, args map[string]any) (RunResult, error) {
		dispatched = append(dispatched, name)
		_, hasLimit := args["limit"]
		switch {
		case name == "get_service_story" && !hasLimit:
			return RunResult{Envelope: overBudgetEnvelope(name)}, nil
		case name == "get_service_story":
			return RunResult{Envelope: serviceStoryEnvelope()}, nil
		case name == "get_index_status":
			return RunResult{Envelope: indexStatusEnvelope()}, nil
		case name == "find_code":
			// A broad, slow search returns a dispatch-timeout envelope; the engine
			// must convert it to a bounded continuation, not an opaque unsupported.
			return RunResult{Envelope: dispatchTimeoutEnvelope(name)}, nil
		default:
			// list_indexed_repositories is a full-inventory list-all and must be
			// refused before dispatch; reaching here is a bounding-guard failure.
			return RunResult{Envelope: overBudgetEnvelope(name)}, nil
		}
	})

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ans, err := eng.Ask(context.Background(), question)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	t.Logf("iterations=%d dispatched=%v termination=%q prose=%q",
		adapter.calls, dispatched, ans.TerminationReason, ans.Prose)

	// Sufficiency stop: fewer iterations than the bound.
	if adapter.calls >= DefaultOptions().MaxIterations {
		t.Errorf("iterations = %d, want < MaxIterations %d (evidence-sufficiency stop)",
			adapter.calls, DefaultOptions().MaxIterations)
	}
	if ans.TerminationReason != terminationEvidenceSufficient {
		t.Errorf("TerminationReason = %q, want %q", ans.TerminationReason, terminationEvidenceSufficient)
	}

	// The unbounded full-inventory list-all is refused before dispatch.
	if containsString(dispatched, "list_indexed_repositories") {
		t.Errorf("unbounded list_indexed_repositories was dispatched; want refused before dispatch")
	}
	// The scoped-but-slow/oversized tools are still dispatched; their runaway
	// results are converted to bounded continuations rather than pre-refused.
	for _, want := range []string{"get_service_story", "find_code"} {
		if !containsString(dispatched, want) {
			t.Errorf("%q was not dispatched; want dispatched then bounded on runaway", want)
		}
	}

	// The refusal must be surfaced with an executable narrowing hint.
	if !anyContains(ans.Limitations, "limit") {
		t.Errorf("Limitations %v missing an executable narrowing hint (limit)", ans.Limitations)
	}

	// The oversized service story and timed-out find_code became bounded
	// continuation packets, not opaque unsupported outcomes.
	for _, tool := range []string{"get_service_story", "find_code"} {
		if !hasOversizedContinuationPacket(ans.Packets, tool) {
			t.Errorf("no bounded continuation packet for runaway %s; packets=%+v", tool, ans.Packets)
		}
	}

	// The published answer is the relevant entity summary, not the generic one.
	if ans.PrimaryPacketIndex == nil {
		t.Fatalf("PrimaryPacketIndex = nil, want the relevant service-story packet")
	}
	primary := ans.Packets[*ans.PrimaryPacketIndex]
	if !strings.Contains(primary.Summary, "payments service") {
		t.Errorf("primary packet Summary = %q, want the payments service overview", primary.Summary)
	}
	if !strings.Contains(ans.Prose, "payments service") || !strings.Contains(ans.Prose, "deployments") {
		t.Errorf("Prose = %q, want the relevant payments overview, not the first-supported generic packet", ans.Prose)
	}
}

// TestAsk_ProgressingMultiFacetFlowNotTruncated proves the evidence-sufficiency
// stop does not truncate a legitimate multi-turn flow: a model that makes
// distinct-tool progress on every turn runs to its own final text turn rather
// than being cut off after the first turn that holds evidence.
func TestAsk_ProgressingMultiFacetFlowNotTruncated(t *testing.T) {
	t.Parallel()

	tc := func(id, name string, args map[string]any) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: name, Arguments: args}
	}
	turns := []provider.Completion{
		{ToolCalls: []provider.ToolCall{tc("t0", "get_service_story", map[string]any{"service": "payments", "limit": 1})}},
		{ToolCalls: []provider.ToolCall{tc("t1", "get_repository_summary", map[string]any{"repo_id": "payments-api"})}},
		{ToolCalls: []provider.ToolCall{tc("t2", "list_deployments", map[string]any{"service": "payments", "limit": 5})}},
		{Text: "here is the full payments overview"},
	}
	adapter := &scriptedAdapter{turns: turns, errOnIdx: -1}

	// Every tool returns a distinct supported, summary-bearing packet.
	runner := RunnerFunc(func(_ context.Context, name string, _ map[string]any) (RunResult, error) {
		return RunResult{Envelope: &query.ResponseEnvelope{
			Truth: &query.TruthEnvelope{Level: query.TruthLevelExact, Basis: query.TruthBasisAuthoritativeGraph},
			Data:  map[string]any{"facet": name, "detail": "payments " + name},
		}}, nil
	})

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ans, err := eng.Ask(context.Background(), "give me a full operational overview of the payments service")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if ans.TerminationReason != terminationFinalTurn {
		t.Errorf("TerminationReason = %q, want %q (progressing flow must reach its final turn)",
			ans.TerminationReason, terminationFinalTurn)
	}
	if adapter.calls != len(turns) {
		t.Errorf("iterations = %d, want %d (no premature sufficiency stop)", adapter.calls, len(turns))
	}
	if len(ans.Packets) != 3 {
		t.Errorf("len(Packets) = %d, want 3 (all facets gathered)", len(ans.Packets))
	}
}

// TestAsk_SingleNoProgressTurnDoesNotTruncate is the failing-first guard for the
// two-consecutive-no-progress-turns rule. The scripted flow makes progress
// (tool A), then spends one no-progress turn (a redundant A call), then makes
// progress again (tool B), then finishes. With the required streak at 1 this
// truncates after the single no-progress turn and never reaches B; at 2 it
// survives, gathers B, and reaches the model's final turn. Reverting
// sufficiencyNoProgressTurns from 2 to 1 must turn this test red.
func TestAsk_SingleNoProgressTurnDoesNotTruncate(t *testing.T) {
	t.Parallel()

	tc := func(id, name string, args map[string]any) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: name, Arguments: args}
	}
	turns := []provider.Completion{
		{ToolCalls: []provider.ToolCall{tc("a", "get_service_story", map[string]any{"service": "payments", "limit": 1})}},
		// One no-progress turn: a redundant call to the already-seen tool.
		{ToolCalls: []provider.ToolCall{tc("a2", "get_service_story", map[string]any{"service": "payments", "limit": 1})}},
		// Progress resumes with a distinct tool that must still be reached.
		{ToolCalls: []provider.ToolCall{tc("b", "list_deployments", map[string]any{"service": "payments", "limit": 5})}},
		{Text: "here is the full payments overview"},
	}
	adapter := &scriptedAdapter{turns: turns, errOnIdx: -1}

	runner := RunnerFunc(func(_ context.Context, name string, _ map[string]any) (RunResult, error) {
		return RunResult{Envelope: &query.ResponseEnvelope{
			Truth: &query.TruthEnvelope{Level: query.TruthLevelExact, Basis: query.TruthBasisAuthoritativeGraph},
			Data:  map[string]any{"facet": name, "detail": "payments " + name},
		}}, nil
	})

	eng, err := New(adapter, runner, nil, DefaultOptions())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ans, err := eng.Ask(context.Background(), "give me a full operational overview of the payments service")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if ans.TerminationReason != terminationFinalTurn {
		t.Errorf("TerminationReason = %q, want %q (a single no-progress turn must not stop the loop)",
			ans.TerminationReason, terminationFinalTurn)
	}
	if adapter.calls != len(turns) {
		t.Errorf("iterations = %d, want %d (loop must survive one no-progress turn)", adapter.calls, len(turns))
	}
	if !hasPacketForTool(ans.Packets, "list_deployments") {
		t.Errorf("the distinct tool after the no-progress turn was never reached; packets=%+v", ans.Packets)
	}
}

func hasPacketForTool(packets []query.AnswerPacket, tool string) bool {
	for _, p := range packets {
		if p.PrimaryTool == tool {
			return true
		}
	}
	return false
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func anyContains(xs []string, sub string) bool {
	for _, x := range xs {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

func hasOversizedContinuationPacket(packets []query.AnswerPacket, tool string) bool {
	for _, p := range packets {
		if p.PrimaryTool != tool || p.Supported {
			continue
		}
		if p.Partial && (len(p.RecommendedNextCalls) > 0 || len(p.UnsupportedReasons) > 0) {
			return true
		}
	}
	return false
}
